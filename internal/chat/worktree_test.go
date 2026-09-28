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

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/worktree"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// fakeAnthropic answers each turn with one tool call, then a short text.
func fakeAnthropic(t *testing.T, wantSystem string, tools []map[string]any) *httptest.Server {
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			System string           `json:"system"`
			Tools  []map[string]any `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		turn, second := calls/2, calls%2 == 1
		calls++
		if !second {
			names := ""
			for _, x := range body.Tools {
				names += x["name"].(string) + " "
			}
			if !strings.Contains(names, "edit_file") || !strings.Contains(body.System, wantSystem) {
				t.Errorf("turn %d: tools %q / system without %q", turn, names, wantSystem)
			}
			out, _ := json.Marshal(map[string]any{"model": "m", "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 10, "output_tokens": 5},
				"content": []map[string]any{{"type": "tool_use", "id": "tu", "name": tools[turn]["name"], "input": tools[turn]["input"]}}})
			w.Write(out)
			return
		}
		w.Write([]byte(`{"model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"Xong."}],"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
}

func TestWorktreeChatEditsThenMerge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	srv := fakeAnthropic(t, "worktree riêng", []map[string]any{
		{"name": "edit_file", "input": map[string]any{"path": "hello.txt", "old": "hello", "new": "xin chào"}},
		{"name": "write_file", "input": map[string]any{"path": "extra.txt", "content": "sai\n"}},
	})
	defer srv.Close()
	f := setup(t, func(provs *provider.Service) storage.Provider {
		key := "sk-ant-test-key-0000"
		p, _ := provs.Create(context.Background(), provider.Input{Name: "C", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: &key})
		return p
	})
	gitRun(t, f.dir, "init", "-q")
	gitRun(t, f.dir, "add", "-A")
	gitRun(t, f.dir, "commit", "-q", "-m", "init")
	trees := worktree.New(filepath.Join(t.TempDir(), "worktrees"))
	f.engine.SetWorktrees(trees)
	ctx := actor.With(context.Background(), "human:a@b.c")
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")

	// 1: the agent edits its worktree; the project is untouched until merged
	turn, _, err := f.engine.Send(ctx, conv.ID, "đổi lời chào", nil)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, turn)
	done := evs[len(evs)-1].Message
	if done == nil || len(done.Patches) != 1 || done.Patches[0].Status != "pending" || strings.Join(done.Patches[0].Files, ",") != "hello.txt" {
		t.Fatalf("patches = %+v (events %+v)", done, evs)
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "hello.txt")); string(b) != "hello\nworld\n" {
		t.Fatalf("project changed before merge: %q", b)
	}
	tree := trees.Path(f.project.ID, chat.ChatTree(conv.ID))
	if b, _ := os.ReadFile(filepath.Join(tree, "hello.txt")); string(b) != "xin chào\nworld\n" {
		t.Fatalf("worktree file = %q", b)
	}
	p, err := f.engine.DecidePatch(ctx, done.Patches[0].ID, true)
	if err != nil || p.Status != "applied" {
		t.Fatalf("merge = %+v %v", p, err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "hello.txt")); string(b) != "xin chào\nworld\n" {
		t.Fatalf("project after merge = %q", b)
	}
	if left, _ := worktree.Changed(context.Background(), tree); len(left) != 0 {
		t.Fatalf("still pending in worktree after merge: %v", left)
	}

	// 2: a new change is only what came after the merge; rejecting drops it
	turn, _, _ = f.engine.Send(ctx, conv.ID, "thêm file", nil)
	evs = collect(t, turn)
	done = evs[len(evs)-1].Message
	if len(done.Patches) != 1 || strings.Join(done.Patches[0].Files, ",") != "extra.txt" {
		t.Fatalf("second patch = %+v", done.Patches)
	}
	if _, err := f.engine.DecidePatch(ctx, done.Patches[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tree, "extra.txt")); !os.IsNotExist(err) {
		t.Fatal("rejected file still in worktree")
	}

	// deleting the conversation removes its worktree
	if err := f.engine.DeleteConversation(ctx, conv.ID); err != nil || trees.Exists(f.project.ID, chat.ChatTree(conv.ID)) {
		t.Fatalf("delete: %v, exists %v", err, trees.Exists(f.project.ID, chat.ChatTree(conv.ID)))
	}
}

func TestDirectModeEditsProject(t *testing.T) {
	srv := fakeAnthropic(t, "trong thư mục project của người dùng", []map[string]any{{"name": "edit_file", "input": map[string]any{"path": "hello.txt", "old": "hello", "new": "chào"}}})
	defer srv.Close()
	f := setup(t, func(provs *provider.Service) storage.Provider {
		key := "sk-ant-test-key-0000"
		p, _ := provs.Create(context.Background(), provider.Input{Name: "C", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: &key})
		return p
	})
	f.engine.SetWorktrees(worktree.New(t.TempDir()))
	ctx := actor.With(context.Background(), "human:a@b.c")
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	if err := f.engine.SetEditMode(ctx, conv.ID, perm.EditDirect); err != nil {
		t.Fatal(err)
	}
	turn, _, err := f.engine.Send(ctx, conv.ID, "đổi", nil)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, turn)
	if done := evs[len(evs)-1].Message; done == nil || len(done.Patches) != 0 {
		t.Fatalf("direct mode made patches: %+v", evs[len(evs)-1])
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "hello.txt")); string(b) != "chào\nworld\n" {
		t.Fatalf("project = %q", b)
	}
}
