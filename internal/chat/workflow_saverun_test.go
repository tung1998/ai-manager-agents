package chat

import (
	"context"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// TestSaveRunDropsOutOfOrderWrite reproduces the race where two goroutines
// hold snapshots of a run taken in order (older, then newer/finished) but
// call saveRun out of order (newer first, as finishRun would, then the
// older one lands after). Without a sequence guard, the older write wins
// and the persisted row reverts to "running" with stale Turns/Log even
// though the run actually finished.
func TestSaveRunDropsOutOfOrderWrite(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)

	project, err := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: tmp})
	if err != nil {
		t.Fatal(err)
	}

	rec, err := st.WorkflowRuns().Create(ctx, storage.WorkflowRun{
		ProjectID: project.ID,
		Status:    storage.RunRunning,
		Turns:     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	e := NewEngine(st, nil, nil)
	run := &wfRun{rec: rec}

	// capture the older, still-running snapshot first (seq 1)
	older, olderSeq := run.snapshot()
	older.Turns = 5

	// then capture the newer, finished snapshot (seq 2)
	run.rec.Turns = 6
	run.rec.Status = storage.RunDone
	now := run.rec.StartedAt
	run.rec.FinishedAt = &now
	run.rec.Result = "ok"
	newer, newerSeq := run.snapshot()

	// writes land out of order: the finished state is saved first, then the
	// stale running snapshot's write arrives afterward
	e.saveRun(run, newer, newerSeq)
	e.saveRun(run, older, olderSeq)

	got, err := st.WorkflowRuns().Get(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != storage.RunDone {
		t.Fatalf("Status = %q, want %q (stale write must not revert a finished run)", got.Status, storage.RunDone)
	}
	if got.Turns != 6 {
		t.Fatalf("Turns = %d, want 6 (stale write must not undo the newer count)", got.Turns)
	}
	if got.Result != "ok" {
		t.Fatalf("Result = %q, want %q", got.Result, "ok")
	}
}
