package channels_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/channels"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// fakeBot is an adapter the test drives.
type fakeBot struct {
	mu   sync.Mutex
	in   chan channels.Incoming
	sent map[string][]string
}

func (b *fakeBot) Run(ctx context.Context, onReady func(string), onMessage func(channels.Incoming)) error {
	onReady("shop_bot")
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-b.in:
			onMessage(m)
		}
	}
}
func (b *fakeBot) Send(_ context.Context, chatID, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent[chatID] = append(b.sent[chatID], text)
	return nil
}
func (b *fakeBot) Typing(context.Context, string) {}
func (b *fakeBot) wait(t *testing.T, chatID string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		got := append([]string(nil), b.sent[chatID]...)
		b.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("chat %s: sent %v", chatID, b.sent[chatID])
	return nil
}

// ADR-048: an outside chat gets the agent's answer; the scope filter refuses
// what is off-topic with a cheap model; the allow list keeps others out.
func TestManagerAnswersFiltersAndAllows(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	u := usage.New(st, time.UTC)
	provs.SetUsage(u)
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	bin := filepath.Join(tmp, "claude")
	// the filter call gets "YES/NO" questions; an answer otherwise
	os.WriteFile(bin, []byte(`#!/bin/sh
in=$(cat)
out="đơn 123 đang giao"
case "$in" in *"YES hoặc NO"*) out=NO; case "$in" in *"đơn hàng"*"đơn 123"*) out=YES;; esac;; esac
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"'"$out"'","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	provs.Create(ctx, provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
	dir := t.TempDir()
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: dir})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	engine := chat.NewEngine(st, provs, u)

	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "telegram", Name: "Hỗ trợ", Enabled: true,
		Allow: []string{"42", "43"}, Scope: "đơn hàng của cửa hàng", FilterEnabled: true, Refusal: "Mình chỉ trả lời về đơn hàng."})
	m := channels.NewManager(st, engine, func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)

	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", UserName: "an", Text: "đơn 123 đâu rồi", Private: true}
	if got := bot.wait(t, "42", 1); !strings.Contains(got[0], "đơn 123 đang giao") {
		t.Fatalf("answer = %v", got)
	}
	bot.in <- channels.Incoming{ChatID: "43", UserID: "8", Text: "thời tiết hôm nay", Private: true}
	if got := bot.wait(t, "43", 1); got[0] != "Mình chỉ trả lời về đơn hàng." {
		t.Fatalf("refusal = %v", got)
	}
	bot.in <- channels.Incoming{ChatID: "99", UserID: "9", Text: "đơn 123 đâu rồi", Private: true}
	time.Sleep(500 * time.Millisecond)
	bot.mu.Lock()
	outsider := bot.sent["99"]
	bot.mu.Unlock()
	if len(outsider) != 0 {
		t.Fatalf("a chat not allowed got %v", outsider)
	}
	jobs, _ := st.Jobs().List(ctx, storage.JobFilter{ProjectID: project.ID})
	skipped := 0
	for _, j := range jobs {
		if j.Status == "skipped" && j.ErrorCode == "out_of_scope" && j.Trigger == "telegram" && j.OriginID == ch.ID {
			skipped++
		}
	}
	if skipped != 1 {
		t.Fatalf("jobs = %+v", jobs)
	}
	got, _ := st.Channels().Get(ctx, ch.ID)
	if got.BotName != "shop_bot" || got.LastMessageAt == nil {
		t.Fatalf("status = %+v", got)
	}
}
