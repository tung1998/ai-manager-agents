package channels_test

import (
	"context"
	"encoding/json"
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

// fakeDecider records the decisions it is asked for.
type fakeDecider struct {
	mu   sync.Mutex
	done []string // "approve:kind:id:by"
}

func (d *fakeDecider) Decide(_ context.Context, kind, id string, approve bool, by string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v := "reject"
	if approve {
		v = "approve"
	}
	d.done = append(d.done, v+":"+kind+":"+id+":"+by)
	return "Thành công", nil
}

func (d *fakeDecider) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.done...)
}

// Proposals are decided from the chat by commands, by the bot's admins only;
// there are no modes any more (ADR-081): what comes next waits as well.
func TestApprovalsFromChat(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	u := usage.New(st, time.UTC)
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	engine := chat.NewEngine(st, provs, u)
	runner := trigger.New(st, chatExec{engine})
	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true,
		Allow: []string{"7", "8"}, Approvers: []string{"7"}})
	m := channels.NewManager(st, engine, runner, func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	dec := &fakeDecider{}
	m.SetDecider(dec)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	// the bot is up (its adapter is where answers go) before anything is sent
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, _ := st.Channels().Get(ctx, ch.ID); c.BotName != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}

	conv, _ := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	// the bot's rule answered there: once decided, its agent goes on (ADR-084)
	rule, _ := st.Automations().Create(ctx, storage.Automation{ProjectID: project.ID, Name: "@bot", Source: "discord", Action: "chat", Enabled: true, Config: storage.AutomationConfig{ChannelID: ch.ID}})
	st.Jobs().Create(ctx, storage.Job{ProjectID: project.ID, Kind: "chat_turn", Origin: "automation", OriginID: rule.ID, Trigger: "discord", ConversationID: conv.ID, Status: "done"})
	act, _ := st.Actions().Create(ctx, storage.Action{ProjectID: project.ID, ConversationID: conv.ID, Kind: "run_command", Target: "pnpm lint", Status: "pending"})
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "42", ConversationID: conv.ID})
	origin := storage.Job{ID: "job_1", Payload: string(payload)}
	m.Reply(ctx, origin, "Đã xem xong", nil, true)
	got := bot.wait(t, "42", 2)
	if !strings.Contains(got[1], "pnpm lint") || !strings.Contains(got[1], "/approve 1") {
		t.Fatalf("pending list = %q", got[1])
	}
	// not an approver: refused
	bot.in <- channels.Incoming{ChatID: "42", UserID: "8", UserName: "binh", Text: "/approve 1", Addressed: true}
	got = bot.wait(t, "42", 3)
	if len(dec.list()) != 0 || !strings.Contains(got[2], "danh sách Admin") {
		t.Fatalf("an outsider decided: %v %q", dec.list(), got[2])
	}
	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", UserName: "an", Text: "/approve 1", Addressed: true}
	got = bot.wait(t, "42", 4)
	if d := dec.list(); len(d) != 1 || d[0] != "approve:action:"+act.ID+":discord:an" || !strings.Contains(got[3], "Đã duyệt") {
		t.Fatalf("approve = %v %q", d, got[3])
	}
	resumed := false
	for deadline := time.Now().Add(3 * time.Second); !resumed && time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		jobs, _ := st.Jobs().List(ctx, storage.JobFilter{OriginID: rule.ID})
		for _, j := range jobs {
			var p trigger.ChannelPayload
			if json.Unmarshal([]byte(j.Payload), &p) == nil && p.ConversationID == conv.ID && strings.Contains(p.Message, "pnpm lint") && strings.Contains(p.Message, "Làm tiếp") {
				resumed = true
			}
		}
	}
	if !resumed {
		t.Fatal("the agent was not run again with what was decided")
	}
	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", UserName: "an", Text: "/mode direct", Addressed: true}
	if got = bot.wait(t, "42", 5); !strings.Contains(got[4], "không còn chế độ") {
		t.Fatalf("/mode = %q", got[4])
	}
	a2, _ := st.Actions().Create(ctx, storage.Action{ProjectID: project.ID, ConversationID: conv.ID, Kind: "run_command", Target: "pnpm test", Status: "pending"})
	m.Reply(ctx, storage.Job{ID: "job_2", Payload: string(payload)}, "Xong", nil, true)
	got = bot.wait(t, "42", 7)
	if d := dec.list(); len(d) != 1 || !strings.Contains(got[6], "pnpm test") {
		t.Fatalf("a proposal was approved by itself: %v %q", d, got[6])
	}
	_ = a2
}

// Admins are named one by one: "*" is nobody's admin (ADR-081).
func TestAdminsAreNamed(t *testing.T) {
	if channels.MayDecide(storage.Channel{Approvers: []string{"*"}}, "8") {
		t.Fatal(`"*" made a user an admin`)
	}
	if channels.MayDecide(storage.Channel{Approvers: []string{"7"}}, "8") || !channels.MayDecide(storage.Channel{Approvers: []string{"7"}}, "7") {
		t.Fatal("the Admin list is not who decides")
	}
}
