package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func testWorkflows(t *testing.T, s storage.Store) {
	ctx := context.Background()
	p, err := s.Repos().Create(ctx, storage.Repo{Name: "p", Path: "/tmp/wf-p"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.Workflows().Create(ctx, storage.Workflow{ProjectID: p.ID, Key: "council", Name: "Hội đồng", Source: "---\n---\nx", Enabled: true,
		Bindings: map[string]string{"a": "agt_1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Workflows().Create(ctx, storage.Workflow{ProjectID: p.ID, Key: "council", Name: "x", Source: "x"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate key err = %v", err)
	}
	w.Enabled, w.Bindings = false, nil
	if err := s.Workflows().Update(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, err := s.Workflows().GetByKey(ctx, p.ID, "council")
	if err != nil || got.Enabled || got.Bindings == nil || len(got.Bindings) != 0 {
		t.Fatalf("GetByKey = %+v, %v", got, err)
	}
	if list, err := s.Workflows().List(ctx, p.ID); err != nil || len(list) != 1 {
		t.Fatalf("List = %d, %v", len(list), err)
	}

	r, err := s.WorkflowRuns().Create(ctx, storage.WorkflowRun{ProjectID: p.ID, ConversationID: "cnv_1", WorkflowKey: "council", WorkflowName: "Hội đồng",
		Status: storage.RunRunning, Roles: []storage.RunRole{{Role: "a", AgentID: "agt_1"}}})
	if err != nil {
		t.Fatal(err)
	}
	r.Turns, r.Log = 2, []storage.RunLog{{At: time.Now().UTC(), Text: "giao a"}}
	r.Roles[0].SessionID = "ses"
	if err := s.WorkflowRuns().Update(ctx, r); err != nil {
		t.Fatal(err)
	}
	back, err := s.WorkflowRuns().Get(ctx, r.ID)
	if err != nil || back.Turns != 2 || len(back.Log) != 1 || back.Roles[0].SessionID != "ses" || back.Gates == nil {
		t.Fatalf("Get = %+v, %v", back, err)
	}
	if n, err := s.WorkflowRuns().FailRunning(ctx, "restart", time.Now()); err != nil || n != 1 {
		t.Fatalf("FailRunning = %d, %v", n, err)
	}
	list, err := s.WorkflowRuns().List(ctx, "", "cnv_1", 10)
	if err != nil || len(list) != 1 || list[0].Status != storage.RunFailed || list[0].FinishedAt == nil {
		t.Fatalf("List runs = %+v, %v", list, err)
	}
	if err := s.Workflows().Delete(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
}
