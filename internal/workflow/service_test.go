package workflow_test

import (
	"context"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

const updateTestSource = `---
key: thu
name: Thử
description: d
roles:
  - key: a
    access: analyze
---
Làm đi.
`

// Update's version check (ADR-072) at the API layer is decorative unless the
// write itself is conditioned on the row the caller read: two saves racing
// on the same Source/Bindings must not let the second silently clobber the
// first (regression for the TOCTOU between the API's Get and this Update).
func TestServiceUpdateExpectIsCompareAndSwap(t *testing.T) {
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
	svc := &workflow.Service{Store: st}
	w, err := svc.Create(ctx, p.ID, updateTestSource, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.Workflows().Get(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}

	// tab A toggling Enabled must not clobber tab B's Source change read
	// from the same "before" row.
	if _, err := svc.Update(ctx, w.ID, workflow.Change{Enabled: boolPtr(false)}); err != nil {
		t.Fatalf("enable toggle: %v", err)
	}
	other := strings.Replace(updateTestSource, "Thử", "Thử 2", 1)
	if _, err := svc.Update(ctx, w.ID, workflow.Change{Source: &other, Expect: &before}); err != nil {
		t.Fatalf("Update with current Expect: %v", err)
	}

	// a second save against the same stale "before" must 409, not overwrite.
	stale := strings.Replace(updateTestSource, "Thử", "Thử 3", 1)
	if _, err := svc.Update(ctx, w.ID, workflow.Change{Source: &stale, Expect: &before}); err != workflow.ErrConflict {
		t.Fatalf("Update with stale Expect = %v, want ErrConflict", err)
	}
	cur, err := st.Workflows().Get(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cur.Source, "Thử 2") || strings.Contains(cur.Source, "Thử 3") {
		t.Fatalf("a conflicted Update changed the row: %q", cur.Source)
	}
	if cur.Enabled {
		t.Fatal("the conflicted Update's loss should not also erase the earlier Enabled toggle")
	}
}

func boolPtr(b bool) *bool { return &b }

// raceRepo runs onGet (once) right after Get returns, letting a test splice
// an interleaved write between the caller's Get and its own later write —
// simulating two requests racing inside the gap Service.Update's single Get
// leaves open.
type raceRepo struct {
	storage.WorkflowRepo
	onGet func()
}

func (r *raceRepo) Get(ctx context.Context, id string) (storage.Workflow, error) {
	w, err := r.WorkflowRepo.Get(ctx, id)
	if f := r.onGet; f != nil {
		r.onGet = nil // once, so the interleaved call's own Get doesn't recurse
		f()
	}
	return w, err
}

type raceStore struct {
	storage.Store
	wf storage.WorkflowRepo
}

func (s raceStore) Workflows() storage.WorkflowRepo { return s.wf }

// contentWriteRaceRepo runs onContentWrite (once) right after UpdateContent
// returns, letting a test splice a write between FromLibrary's content write
// (via Service.Update) and its own final source_key/source_hash write.
type contentWriteRaceRepo struct {
	storage.WorkflowRepo
	onContentWrite func()
}

func (r *contentWriteRaceRepo) UpdateContent(ctx context.Context, w storage.Workflow) error {
	err := r.WorkflowRepo.UpdateContent(ctx, w)
	if f := r.onContentWrite; f != nil {
		r.onContentWrite = nil
		f()
	}
	return err
}
