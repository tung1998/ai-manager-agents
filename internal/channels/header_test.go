package channels_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
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

// The line on top of a bot's answer: its template, the parts it has, off.
func TestHeaderLine(t *testing.T) {
	for _, c := range []struct{ tpl, kind, agent, project, branch, want string }{
		{"", "discord", "Trợ lý", "shop", "main", "-# Trợ lý · shop · main"},
		{"", "telegram", "Trợ lý", "shop", "main", "Trợ lý · shop · main"},
		{"", "discord", "", "shop", "", "-# shop"}, // a script's answer: no agent; not a git folder
		{"{project} ({branch})", "discord", "A", "shop", "dev", "-# shop (dev)"},
		{"-", "discord", "A", "shop", "dev", ""},
	} {
		if got := channels.HeaderLine(c.tpl, c.kind, c.agent, c.project, c.branch); got != c.want {
			t.Errorf("%q %s: %q, want %q", c.tpl, c.kind, got, c.want)
		}
	}
}

// A bot's answer starts with who answered, in which project and branch; a
// follow-up of another agent names that agent instead of signing the text.
func TestReplyHeader(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	engine := chat.NewEngine(st, provs, usage.New(st, time.UTC))
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "-b", "feature-x", dir).CombinedOutput(); err != nil {
		t.Skip("git:", string(out))
	}
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: dir})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	agents, _ := engine.Agents(ctx, project.ID)
	st.Agents().Create(ctx, storage.Agent{OrgModelID: agents[0].OrgModelID, Key: "tester", Name: "Tester", Tier: "worker", ReportsTo: []string{agents[0].Key}, ModelTier: "fast"})
	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, _ := st.Channels().Get(ctx, ch.ID); c.BotName != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}
	conv, _ := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "42", ConversationID: conv.ID})
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Đã xem xong", nil, false)
	got := bot.wait(t, "42", 1)
	if got[0] != "-# "+conv.AgentName+" · shop · feature-x\nĐã xem xong" {
		t.Fatalf("answer = %q", got[0])
	}
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Tester:\nTest đạt", nil, false)
	got = bot.wait(t, "42", 2)
	if got[1] != "-# Tester · shop · feature-x\nTest đạt" {
		t.Fatalf("follow-up = %q", got[1])
	}
	ch.Header = "-"
	st.Channels().Update(ctx, ch)
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Xong rồi", nil, false)
	if got = bot.wait(t, "42", 3); got[2] != "Xong rồi" || strings.Contains(got[2], "-#") {
		t.Fatalf("off = %q", got[2])
	}
}
