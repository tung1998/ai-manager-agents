package workflow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// After a restart the runs cut off are failed and the chats that called
// them are told (a chat would otherwise wait for an output forever).
func TestEndInterruptedTellsTheCaller(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: "/p"})
	caller, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p.ID, Title: "c"})
	own, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p.ID, Title: "run", Purpose: "workflow_run"})
	top, _ := st.WorkflowRuns().Create(ctx, storage.WorkflowRun{ProjectID: p.ID, ConversationID: own.ID, CallerConversationID: caller.ID, WorkflowKey: "council", WorkflowName: "Hội đồng", Status: storage.RunRunning, StartedAt: time.Now()})
	_, _ = st.WorkflowRuns().Create(ctx, storage.WorkflowRun{ProjectID: p.ID, ConversationID: "cnv_x", CallerConversationID: own.ID, ParentRunID: top.ID, Depth: 1, WorkflowKey: "advisor", WorkflowName: "Cố vấn", Status: storage.RunRunning, StartedAt: time.Now()})
	n, err := (&workflow.Service{Store: st}).EndInterrupted(ctx)
	if err != nil || n != 2 {
		t.Fatalf("ended %d, %v", n, err)
	}
	if r, _ := st.WorkflowRuns().Get(ctx, top.ID); r.Status != storage.RunFailed {
		t.Fatalf("status = %s", r.Status)
	}
	msgs, _ := st.Chat().ListMessages(ctx, caller.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "/council") {
		t.Fatalf("caller's chat = %+v", msgs)
	}
	if msgs, _ := st.Chat().ListMessages(ctx, own.ID); len(msgs) != 1 { // its own (a sub-run tells its own chat, not this one twice)
		t.Fatalf("run's chat = %+v", msgs)
	}
}
