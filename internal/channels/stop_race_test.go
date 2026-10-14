package channels

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// raceAdapter records what got sent, so a test can tell whether a finished
// answer was actually delivered or silently dropped.
type raceAdapter struct {
	sent []string
}

func (a *raceAdapter) Run(ctx context.Context, onReady func(string), onMessage func(Incoming)) error {
	return nil
}
func (a *raceAdapter) Send(ctx context.Context, chatID, text string) ([]string, error) {
	a.sent = append(a.sent, text)
	return []string{"m1"}, nil
}
func (a *raceAdapter) SetCommands(ctx context.Context, cmds []Command) {}
func (a *raceAdapter) Typing(ctx context.Context, chatID string)       {}

func (a *raceAdapter) has(needle string) bool {
	for _, s := range a.sent {
		if s == needle {
			return true
		}
	}
	return false
}

// fakeStopEngine is a stopEngine (see stop.go) whose StopAll calls back into
// the manager's Reply before returning — exactly the interleaving the real
// bug needed a lucky goroutine schedule to hit: the job's own completion path
// (Reply) runs while stopChat is in the middle of stopping it, and finds
// nothing left to actually cancel (0).
type fakeStopEngine struct {
	onStopAll func() int
}

func (f *fakeStopEngine) StopAll(conversationID string) int               { return f.onStopAll() }
func (f *fakeStopEngine) Active(conversationID string) (*chat.Turn, bool) { return nil, false }

// TestStopChatDoesNotDropAReplyThatAlreadyFinished reproduces the race in
// stopChat (internal/channels/stop.go): /stop used to mark a job's wait entry
// stopped=true before confirming the job's run was actually still in flight
// (m.engine.StopAll). When the job's own completion call (Reply) landed
// concurrently and the run had already finished (StopAll finds nothing left
// to cancel, returns 0), Reply would observe stopped=true and drop the
// already-finished answer for good — stopChat's later setStopped(id, false)
// "revert" could not undo it, because by then Reply had already deleted the
// waiting entry.
//
// The fake engine's StopAll calls Reply synchronously from inside stopChat's
// own call to StopAll, which deterministically reproduces that interleaving
// without depending on goroutine scheduling luck.
func TestStopChatDoesNotDropAReplyThatAlreadyFinished(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	project, err := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "telegram", Name: "Dev", Enabled: true, Allow: []string{"*"}, Header: "-"})
	if err != nil {
		t.Fatal(err)
	}
	ad := &raceAdapter{}
	convID := "conv-1"
	key := "telegram:" + ch.ID
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "c1", UserID: "7", ConversationID: convID})
	job, err := st.Jobs().Create(ctx, storage.Job{ProjectID: project.ID, Kind: "chat_turn", Origin: "automation", Trigger: "telegram", Payload: string(payload), Status: "running", ConversationID: convID})
	if err != nil {
		t.Fatal(err)
	}

	answer := "answer-" + job.ID
	m := &Manager{store: st, adapters: map[string]Adapter{ch.ID: ad}, waiting: map[string]waiter{job.ID: {key: key, typing: func() {}}}, pending: Pending{n: map[string]int{}}}
	m.stopEngine = &fakeStopEngine{onStopAll: func() int {
		m.Reply(ctx, job, answer, nil, true) // the job finishes and replies right as stopChat is stopping it
		return 0                             // ...and there was nothing left for StopAll to actually cancel
	}}

	m.stopChat(ctx, key)

	if !ad.has(answer) {
		t.Fatal("a finished answer was silently dropped by a racing /stop")
	}
}
