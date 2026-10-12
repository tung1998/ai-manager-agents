package workflow_test

import (
	"context"
	"os"
	"path/filepath"
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

// A content save (Source/Bindings) must not revert an Enabled toggle that
// lands in the gap between the save's Get and its write, and vice versa:
// the two must write disjoint columns. This is the scenario the reviewer
// flagged as still broken by a single CAS'd full-row write (tab A toggles
// Enabled, tab B edits Source, neither sends the other's field).
func TestServiceUpdateContentSaveDoesNotRevertInterleavedToggle(t *testing.T) {
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
	plain := &workflow.Service{Store: st}
	w, err := plain.Create(ctx, p.ID, updateTestSource, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}

	wrapped := raceStore{Store: st, wf: &raceRepo{WorkflowRepo: st.Workflows(), onGet: func() {
		// the toggle's whole Get+write cycle happens between the content
		// save's Get and its write.
		if _, err := plain.Update(ctx, w.ID, workflow.Change{Enabled: boolPtr(false)}); err != nil {
			t.Fatalf("interleaved toggle: %v", err)
		}
	}}}
	raceSvc := &workflow.Service{Store: wrapped}
	newSrc := strings.Replace(updateTestSource, "Thử", "Thử 2", 1)
	if _, err := raceSvc.Update(ctx, w.ID, workflow.Change{Source: &newSrc}); err != nil {
		t.Fatalf("content save: %v", err)
	}

	cur, err := st.Workflows().Get(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cur.Source, "Thử 2") {
		t.Fatalf("content save was lost: %q", cur.Source)
	}
	if cur.Enabled {
		t.Fatal("the interleaved Enabled toggle was reverted by the content save")
	}
}

// The mirror image: an Enabled toggle must not revert a content save that
// lands in the gap between the toggle's Get and its write.
func TestServiceUpdateEnabledToggleDoesNotRevertInterleavedContentSave(t *testing.T) {
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
	plain := &workflow.Service{Store: st}
	w, err := plain.Create(ctx, p.ID, updateTestSource, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}

	newSrc := strings.Replace(updateTestSource, "Thử", "Thử 2", 1)
	wrapped := raceStore{Store: st, wf: &raceRepo{WorkflowRepo: st.Workflows(), onGet: func() {
		if _, err := plain.Update(ctx, w.ID, workflow.Change{Source: &newSrc}); err != nil {
			t.Fatalf("interleaved content save: %v", err)
		}
	}}}
	raceSvc := &workflow.Service{Store: wrapped}
	if _, err := raceSvc.Update(ctx, w.ID, workflow.Change{Enabled: boolPtr(false)}); err != nil {
		t.Fatalf("toggle: %v", err)
	}

	cur, err := st.Workflows().Get(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cur.Source, "Thử 2") {
		t.Fatalf("the interleaved content save was reverted by the toggle: %q", cur.Source)
	}
}

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

// FromLibrary's final write (source_key/source_hash) must not revert a
// concurrent Enabled toggle or Bindings change that lands between its
// content write and that final write (regression for the blind full-row
// Update that used to close FromLibrary).
func TestFromLibraryDoesNotRevertInterleavedToggle(t *testing.T) {
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
	plain := &workflow.Service{Store: st}
	w, err := plain.Create(ctx, p.ID, updateTestSource, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	// seed SourceKey so FromLibrary has a library item to fetch.
	seeded := w
	seeded.SourceKey = "thu"
	if err := st.Workflows().Update(ctx, seeded); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	libSrc := strings.Replace(updateTestSource, "Thử", "Thử nhất", 1)
	if err := os.WriteFile(filepath.Join(dir, "thu.md"), []byte(libSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	wrapped := raceStore{Store: st, wf: &contentWriteRaceRepo{WorkflowRepo: st.Workflows(), onContentWrite: func() {
		// w (read by FromLibrary before its content write) still has
		// Enabled=true (Create's default); this toggle to false must
		// survive FromLibrary's final write, not be reverted back to the
		// stale true it captured.
		if _, err := plain.Update(ctx, w.ID, workflow.Change{Enabled: boolPtr(false)}); err != nil {
			t.Fatalf("interleaved toggle: %v", err)
		}
	}}}
	raceSvc := &workflow.Service{Store: wrapped, Lib: workflow.Library{Dir: dir}}
	if _, err := raceSvc.FromLibrary(ctx, w.ID); err != nil {
		t.Fatalf("FromLibrary: %v", err)
	}

	cur, err := st.Workflows().Get(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cur.Source, "Thử nhất") {
		t.Fatalf("FromLibrary's content write was lost: %q", cur.Source)
	}
	if cur.Enabled {
		t.Fatal("the interleaved Enabled toggle was reverted by FromLibrary's final write")
	}
	if cur.SourceKey != "thu" || cur.SourceHash == "" {
		t.Fatalf("source_key/source_hash not updated: %+v", cur)
	}
	if raceSvc.HasUpdate(cur) {
		t.Fatal("HasUpdate should be false right after FromLibrary")
	}
}
