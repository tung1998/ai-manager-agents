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
	"bitbucket.org/senprints/agent-office/internal/trigger"
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

// chatExec runs automation chats on the engine (as the office's executor does).
type chatExec struct{ engine *chat.Engine }

func (x chatExec) RunChat(ctx context.Context, projectID, agentID, conv, prompt, edit string) (string, string, error) {
	turn, _, err := x.engine.Send(ctx, conv, prompt, nil)
	if err != nil {
		return conv, "", err
	}
	for seq := 0; ; {
		evs, done, wake := turn.Since(seq)
		seq += len(evs)
		for _, e := range evs {
			if e.Type == "done" && e.Message != nil {
				return conv, e.Message.Content, nil
			}
		}
		if done {
			return conv, "", nil
		}
		<-wake
	}
}
func (chatExec) RunTask(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (chatExec) RunQueuedTask(context.Context, string, string) (string, error) { return "", nil }

// ADR-049: a channel's messages go to the first automation (rule) that
// matches: keywords for free, a scope asked of a cheap model; a script
// answers without AI; nothing matching gets the channel's refusal; the allow
// list keeps others out.
func TestManagerRules(t *testing.T) {
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
	argsLog := filepath.Join(tmp, "args.log")
	os.WriteFile(bin, []byte(`#!/bin/sh
printf '%s\n===\n' "$*" >> `+argsLog+`
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
	runner := trigger.New(st, chatExec{engine})

	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "telegram", Name: "Hỗ trợ", Enabled: true,
		Allow: []string{"42", "43", "44"}, Refusal: "Mình chỉ trả lời về đơn hàng."})
	// rule 1: "mã" → a script, no AI; rule 2: about orders → the agent answers
	st.Automations().Create(ctx, storage.Automation{ProjectID: project.ID, Name: "Tra mã", Source: "telegram", Action: "script", Enabled: true,
		Config: storage.AutomationConfig{ChannelID: ch.ID, Keywords: []string{"MÃ"}},
		Script: storage.AutomationScript{Lang: "bash", Body: `echo "Mã của $(cat | sed 's/.*"user":"\([^"]*\)".*/\1/'): OK"`, TimeoutS: 10}})
	st.Automations().Create(ctx, storage.Automation{ProjectID: project.ID, Name: "Trả lời", Source: "telegram", Action: "chat", Enabled: true,
		Config: storage.AutomationConfig{ChannelID: ch.ID, Scope: "đơn hàng của cửa hàng"}})
	m := channels.NewManager(st, engine, runner, func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runner.SetOnReply(m.Reply)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)

	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", UserName: "an", Text: "đơn 123 đâu rồi", Private: true}
	if got := bot.wait(t, "42", 1); !strings.Contains(got[0], "đơn 123 đang giao") {
		t.Fatalf("answer = %v", got)
	}
	bot.in <- channels.Incoming{ChatID: "44", UserID: "9", UserName: "binh", Text: "tra mã giúp", Private: true}
	if got := bot.wait(t, "44", 1); got[0] != "Mã của binh: OK" {
		t.Fatalf("script answer = %v", got)
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
	var skipped, answered int
	for _, j := range jobs {
		if j.Status == "skipped" && j.ErrorCode == "no_rule" && j.Trigger == "telegram" && j.OriginID == ch.ID {
			skipped++
		}
		if j.Origin == "automation" && j.Trigger == "telegram" && j.Status == "done" {
			answered++
		}
	}
	if skipped != 1 || answered != 2 {
		t.Fatalf("skipped %d answered %d: %+v", skipped, answered, jobs)
	}
	got, _ := st.Channels().Get(ctx, ch.ID)
	if got.BotName != "shop_bot" || got.LastMessageAt == nil {
		t.Fatalf("status = %+v", got)
	}
	// review C2/C3: outside people drive these runs: no file, MCP or office tools
	raw, _ := os.ReadFile(argsLog)
	for _, call := range strings.Split(strings.TrimSpace(string(raw)), "\n===") {
		line, _, _ := strings.Cut(call, "--append-system-prompt")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(line, "--tools  ") || strings.Contains(line, "--settings") || strings.Contains(line, "--mcp-config") || strings.Contains(line, "Read") {
			t.Errorf("a channel run had tools: %s", line)
		}
	}
}

// A channel set up before rules existed keeps answering: its agent and scope
// become its first rule, once.
func TestLegacyChannelBecomesARule(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop"})
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Hỗ trợ", Enabled: true, AgentID: "agt_1",
		Allow: []string{"*"}, Scope: "đơn hàng", FilterEnabled: true})
	for range 2 {
		if err := channels.MigrateRules(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := st.Automations().List(ctx, project.ID)
	if len(list) != 1 || list[0].Source != "discord" || list[0].Action != "chat" || list[0].Config.ChannelID != ch.ID ||
		list[0].Config.Scope != "đơn hàng" || list[0].AgentID != "agt_1" || !list[0].Enabled {
		t.Fatalf("rules = %+v", list)
	}
}

// Review I6: a flood from one chat is not queued without end.
func TestFloodIsCapped(t *testing.T) {
	if n := channels.MaxPending; n < 1 || n > 5 {
		t.Fatalf("MaxPending = %d", n)
	}
	var q channels.Pending
	taken := 0
	for range 20 {
		if q.Take("chat") {
			taken++
		}
	}
	if taken != channels.MaxPending {
		t.Fatalf("took %d of 20, want %d", taken, channels.MaxPending)
	}
	q.Done("chat")
	if !q.Take("chat") {
		t.Fatal("a slot freed is not taken again")
	}
}
