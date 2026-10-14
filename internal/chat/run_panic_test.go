package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// TestRunPanicEndsJobWithError: a panic anywhere inside run()'s call chain
// (here: e.choices, through a nil *provider.Service — the same shape of bug
// as a nil deref deeper in the LLM response parsing) must still close the
// job as failed and post/emit an error, not just vanish leaving the job
// "running" forever and the person with no answer.
func TestRunPanicEndsJobWithError(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Migrate(ctx)

	project, err := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := st.Agents().Create(ctx, storage.Agent{ProjectID: project.ID, Key: "lead", Name: "Lead", ModelTier: "balanced"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: project.ID, AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	// providers left nil: e.choices(ctx, agent) calls into a nil
	// *provider.Service, panicking inside run()'s call chain — exactly the
	// class of bug (nil deref deep in choices/policy/tools/LLM parsing) the
	// fix guards against.
	e := NewEngine(st, nil, nil)

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	turn := &Turn{ID: "t1", ConversationID: conv.ID, wake: make(chan struct{}), cancel: cancel, agentID: agent.ID, agentName: agent.Name}
	job, err := e.beginJob(runCtx, conv, agent.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	turn.setJobID(job.ID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.run(runCtx, turn, conv, project, agent, nil, "hi", nil)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return — the panic was not recovered")
	}

	if turn.err == "" {
		t.Fatal("turn.err not set: the panic was not turned into a failed turn")
	}

	j, err := st.Jobs().Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != "failed" {
		t.Fatalf("job status = %q, want failed (endJob must still run after a panic)", j.Status)
	}

	msgs, err := st.Chat().ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawError bool
	for _, m := range msgs {
		if m.Role == "error" && strings.Contains(m.Content, "internal error") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("no error message posted to the chat; messages: %+v", msgs)
	}

	evs, doneEvs, _ := turn.Since(0)
	if !doneEvs {
		t.Fatal("turn never finished")
	}
	var sawErrorEvent bool
	for _, ev := range evs {
		if ev.Type == "error" {
			sawErrorEvent = true
		}
	}
	if !sawErrorEvent {
		t.Fatalf("no error event emitted; events: %+v", evs)
	}
}
