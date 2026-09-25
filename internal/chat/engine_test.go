package chat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

type fixture struct {
	st      storage.Store
	engine  *chat.Engine
	provs   *provider.Service
	project storage.Repo
	dir     string
}

func setup(t *testing.T, providerIn func(provs *provider.Service) storage.Provider) fixture {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	u := usage.New(st, time.UTC)
	provs.SetUsage(u)
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	providerIn(provs)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0o644)
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: dir})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	return fixture{st: st, engine: chat.NewEngine(st, provs, u), provs: provs, project: project, dir: dir}
}

func collect(t *testing.T, turn *chat.Turn) []chat.Event {
	t.Helper()
	deadline := time.After(15 * time.Second)
	seq := 0
	var all []chat.Event
	for {
		evs, done, wake := turn.Since(seq)
		all = append(all, evs...)
		seq += len(evs)
		if done {
			return all
		}
		select {
		case <-wake:
		case <-deadline:
			t.Fatalf("turn did not finish; events: %+v", all)
		}
	}
}

func TestAnthropicToolLoopAndPatch(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Messages []json.RawMessage `json:"messages"`
			System   string            `json:"system"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.System, "KHÔNG tự sửa file") {
			t.Errorf("system prompt missing patch rule: %q", body.System)
		}
		if calls == 1 {
			w.Write([]byte(`{"model":"claude-sonnet-5","stop_reason":"tool_use","content":[{"type":"text","text":"Để tôi đọc file."},{"type":"tool_use","id":"tu1","name":"read_file","input":{"path":"hello.txt"}}],"usage":{"input_tokens":100,"output_tokens":20}}`))
			return
		}
		// the tool result must have been sent back
		last := string(body.Messages[len(body.Messages)-1])
		if !strings.Contains(last, "1\\thello") {
			t.Errorf("tool result not returned: %s", last)
		}
		answer := "File có 2 dòng. Sửa như sau:\n```diff\n--- a/hello.txt\n+++ b/hello.txt\n@@ -1,2 +1,2 @@\n-hello\n+xin chào\n world\n```"
		out, _ := json.Marshal(map[string]any{"model": "claude-sonnet-5", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": answer}}, "usage": map[string]int{"input_tokens": 150, "output_tokens": 60}})
		w.Write(out)
	}))
	defer srv.Close()
	f := setup(t, func(provs *provider.Service) storage.Provider {
		key := "sk-ant-test-key-0000"
		p, err := provs.Create(context.Background(), provider.Input{Name: "C", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: &key})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
	ctx := actor.With(context.Background(), "human:a@b.c")
	conv, err := f.engine.StartConversation(ctx, f.project.ID, "")
	if err != nil || conv.AgentName != "Trợ lý" {
		t.Fatalf("conversation = %+v, %v", conv, err)
	}
	turn, _, err := f.engine.Send(ctx, conv.ID, "Sửa lời chào trong hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.engine.Send(ctx, conv.ID, "again"); err != chat.ErrBusy {
		t.Fatalf("concurrent send err = %v", err)
	}
	events := collect(t, turn)
	types := []string{}
	for _, e := range events {
		types = append(types, e.Type)
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "tool") || !strings.Contains(joined, "patch") || !strings.HasSuffix(joined, "done") {
		t.Fatalf("event types = %s", joined)
	}
	done := events[len(events)-1].Message
	if len(done.Patches) != 1 || done.Patches[0].Status != "pending" || done.Patches[0].Files[0] != "hello.txt" || len(done.Tools) != 1 {
		t.Fatalf("done message = %+v", done)
	}
	if done.CostUSD == nil {
		t.Fatal("chat cost not recorded")
	}
	runs, _ := f.st.Runs().List(ctx, storage.RunFilter{Limit: 5})
	if len(runs) != 1 || runs[0].Kind != "chat" || runs[0].InputTokens != 250 {
		t.Fatalf("runs = %+v", runs)
	}

	// history keeps messages and patches
	hist, _ := f.engine.History(ctx, conv.ID)
	if len(hist) != 2 || hist[0].Role != "user" || len(hist[1].Patches) != 1 {
		t.Fatalf("history = %+v", hist)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	p, err := f.engine.DecidePatch(ctx, done.Patches[0].ID, true)
	if err != nil || p.Status != "applied" || p.DecidedBy != "human:a@b.c" {
		t.Fatalf("approve = %+v, %v", p, err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "hello.txt")); string(b) != "xin chào\nworld\n" {
		t.Fatalf("file after approve = %q", b)
	}
	if _, err := f.engine.DecidePatch(ctx, done.Patches[0].ID, false); err != chat.ErrDecided {
		t.Fatalf("second decision err = %v", err)
	}
}

func TestClaudeCLIRunnerStreams(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in *"--tools Read,Glob,Grep"*) ;; *) echo "missing read-only tools: $*" >&2; exit 2;; esac
case "$*" in *"--setting-sources project,local"*) ;; *) echo "not isolated" >&2; exit 2;; esac
cat >/dev/null
echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Xin "}}}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"hello.txt"}}]}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"chào"}}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Xin chào","session_id":"sess-1","total_cost_usd":0.02,"usage":{"input_tokens":5,"cache_read_input_tokens":100,"output_tokens":7}}'
`), 0o755)
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, err := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
	ctx := context.Background()
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	turn, _, err := f.engine.Send(ctx, conv.ID, "chào")
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, turn)
	var text strings.Builder
	for _, e := range events {
		if e.Type == "text" {
			text.WriteString(e.Text)
		}
		if e.Type == "error" {
			t.Fatalf("error event: %s", e.Text)
		}
	}
	done := events[len(events)-1].Message
	if text.String() != "Xin chào" || done.Content != "Xin chào" || len(done.Tools) != 1 || done.Tools[0].Summary != "Đọc hello.txt" {
		t.Fatalf("streamed=%q done=%+v", text.String(), done)
	}
	conv, _ = f.st.Chat().GetConversation(ctx, conv.ID)
	if conv.SessionID != "sess-1" || conv.Runtime != "claude_cli" {
		t.Fatalf("session not saved: %+v", conv)
	}
	runs, _ := f.st.Runs().List(ctx, storage.RunFilter{Limit: 5})
	if len(runs) != 1 || runs[0].CostSource != "provider" || runs[0].InputTokens != 105 {
		t.Fatalf("runs = %+v", runs)
	}
}
