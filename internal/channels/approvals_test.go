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

// Proposals are decided from the chat by commands, by the people allowed to;
// /mode direct approves what comes next, except what must always be asked.
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

	conv, _ := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
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
	if len(dec.list()) != 0 || !strings.Contains(got[2], "không được duyệt") {
		t.Fatalf("an outsider decided: %v %q", dec.list(), got[2])
	}
	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", UserName: "an", Text: "/approve 1", Addressed: true}
	got = bot.wait(t, "42", 4)
	if d := dec.list(); len(d) != 1 || d[0] != "approve:action:"+act.ID+":discord:an" || !strings.Contains(got[3], "Đã duyệt") {
		t.Fatalf("approve = %v %q", d, got[3])
	}
	// direct: what comes next is approved, a push is still asked
	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", UserName: "an", Text: "/mode direct", Addressed: true}
	bot.wait(t, "42", 5)
	a2, _ := st.Actions().Create(ctx, storage.Action{ProjectID: project.ID, ConversationID: conv.ID, Kind: "run_command", Target: "pnpm test", Status: "pending"})
	push, _ := st.Actions().Create(ctx, storage.Action{ProjectID: project.ID, ConversationID: conv.ID, Kind: "git_push", Target: "origin fix", Status: "pending"})
	m.Reply(ctx, storage.Job{ID: "job_2", Payload: string(payload)}, "Xong", nil, true)
	got = bot.wait(t, "42", 8)
	if d := dec.list(); len(d) != 2 || d[1] != "approve:action:"+a2.ID+":discord:an" {
		t.Fatalf("direct = %v", d)
	}
	if !strings.Contains(got[6], "pnpm test") || !strings.Contains(got[7], "origin fix") || strings.Contains(got[7], "pnpm test") {
		t.Fatalf("after direct approvals = %q", got[6:])
	}
	_ = push
}

// "*" lets anyone who may message the bot decide.
func TestApproversAnyone(t *testing.T) {
	if !channels.MayDecide(storage.Channel{Approvers: []string{"*"}}, "8") {
		t.Fatal("* did not let a user decide")
	}
	if channels.MayDecide(storage.Channel{Approvers: []string{"7"}}, "8") {
		t.Fatal("a user not listed may decide")
	}
}
