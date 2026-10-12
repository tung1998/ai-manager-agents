package chat

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// blockingChatStore wraps a real ChatRepo but blocks ListMessages on started,
// until release is closed, to reproduce the window superviseOnce leaves
// unlocked between its first byConv check and the one that follows it.
type blockingChatStore struct {
	storage.ChatRepo
	started chan struct{}
	release chan struct{}
}

func (b *blockingChatStore) ListMessages(ctx context.Context, conversationID string) ([]storage.Message, error) {
	close(b.started)
	<-b.release
	return b.ChatRepo.ListMessages(ctx, conversationID)
}

type blockingStore struct {
	storage.Store
	chat *blockingChatStore
}

func (b blockingStore) Chat() storage.ChatRepo { return b.chat }

// TestSuperviseOnceStopsWhenRunFinishesMidFlight reproduces the race where
// finishRun deletes the run from byConv while superviseOnce is unlocked,
// mid-flight inside ListMessages: without a re-check after the lock is
// retaken, superviseOnce would go on to start a supervisor turn for a run
// that has already finished.
func TestSuperviseOnceStopsWhenRunFinishesMidFlight(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	real, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { real.Close() })
	real.Migrate(ctx)

	project, err := real.Repos().Create(ctx, storage.Repo{Name: "demo", Path: tmp})
	if err != nil {
		t.Fatal(err)
	}
	coord, err := real.Agents().Create(ctx, storage.Agent{ProjectID: project.ID, Key: "coord", Name: "Coord", ModelTier: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	sup, err := real.Agents().Create(ctx, storage.Agent{ProjectID: project.ID, Key: "sup", Name: "Sup", ModelTier: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := real.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := real.Chat().AddMessage(ctx, storage.Message{ConversationID: conv.ID, Role: "user", Author: "someone", Content: "go"}); err != nil {
		t.Fatal(err)
	}

	bc := &blockingChatStore{ChatRepo: real.Chat(), started: make(chan struct{}), release: make(chan struct{})}
	st := blockingStore{Store: real, chat: bc}
	e := NewEngine(st, nil, nil)

	rec, err := real.WorkflowRuns().Create(ctx, storage.WorkflowRun{
		ProjectID: project.ID, Status: storage.RunRunning,
		Roles: []storage.RunRole{{Role: "giam-sat", AgentID: sup.ID, AgentName: sup.Name}},
	})
	if err != nil {
		t.Fatal(err)
	}
	run := &wfRun{
		rec:   rec,
		def:   workflow.Def{Supervise: &workflow.Supervise{Role: "giam-sat"}},
		coord: coord,
		done:  make(chan struct{}),
	}
	e.wf.init()
	e.wf.mu.Lock()
	e.wf.byConv[conv.ID] = run
	e.wf.mu.Unlock()

	done := make(chan bool, 1)
	go func() { done <- e.superviseOnce(run, conv, project) }()

	select {
	case <-bc.started:
	case <-time.After(5 * time.Second):
		t.Fatal("superviseOnce never reached ListMessages")
	}

	// the run finishes while superviseOnce is unlocked inside ListMessages
	e.finishRun(conv.ID, rec.ID, storage.RunDone, "ok", "")
	close(bc.release)

	select {
	case got := <-done:
		if got {
			t.Fatal("superviseOnce = true, want false for a run that finished mid-flight")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("superviseOnce never returned")
	}
	if run.watching || run.watchSeen != "" {
		t.Fatalf("superviseOnce started a supervisor turn for a finished run: watching=%v watchSeen=%q", run.watching, run.watchSeen)
	}
}

func TestObjectionOf(t *testing.T) {
	for in, want := range map[string]string{
		"PHẢN BIỆN: bản giao sửa nhầm file\nbằng chứng": "bản giao sửa nhầm file",
		"**Phản biện** – thiếu ràng buộc":               "thiếu ràng buộc",
		"PHAN BIEN:": "xem câu trả lời",
		"**Objection:** the brief edits the wrong file": "the brief edits the wrong file",
		"Đã làm xong, không có phản biện.":              "",
		"": "",
	} {
		if got := objectionOf(in); got != want {
			t.Errorf("objectionOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBallot(t *testing.T) {
	for in, want := range map[string]string{
		"VOTE: AGREE\nbecause":       "yes",
		"**VOTE: DISAGREE**":         "no",
		"PHIẾU: ĐỒNG Ý":              "yes",
		"PHIẾU: KHÔNG ĐỒNG Ý":        "no",
		"I think it is fine overall": "unclear",
	} {
		if got := ballot(in); got != want {
			t.Errorf("ballot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDriftOf(t *testing.T) {
	if what, drift := driftOf("GIÁM SÁT: LỆCH: đang sửa ngoài phạm vi\nchi tiết"); !drift || what != "đang sửa ngoài phạm vi" {
		t.Fatalf("drift = %q %v", what, drift)
	}
	if _, drift := driftOf("**GIÁM SÁT: ỔN**\nđúng hướng"); drift {
		t.Fatal("ok read as drift")
	}
	if what, drift := driftOf("SUPERVISOR: DRIFT: editing out of scope\ndetails"); !drift || what != "editing out of scope" {
		t.Fatalf("drift = %q %v", what, drift)
	}
	if _, drift := driftOf("**SUPERVISOR: OK** no drift so far"); drift {
		t.Fatal("ok read as drift")
	}
	if _, drift := driftOf("không rõ"); drift {
		t.Fatal("unclear read as drift")
	}
}

// a workflow's role works itself: with full access, no subagents of its own
func TestClaudeArgsNoSubagents(t *testing.T) {
	var r claudeRunner
	a := r.args(RunRequest{FullAccess: true, NoSubagents: true}, false)
	if i := slices.Index(a, "--disallowedTools"); i < 0 || a[i+1] != "Agent Task" {
		t.Fatalf("args = %v", a)
	}
	if a := r.args(RunRequest{FullAccess: true}, false); slices.Contains(a, "--disallowedTools") {
		t.Fatalf("full access without it = %v", a)
	}
}
