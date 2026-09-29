package tasks_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/tasks"
)

// Tasks from before jobs existed get their job once, dated as they were, so
// the Jobs page shows the whole history.
func TestBackfillJobs(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "p"})
	old, _ := st.Tasks().Create(ctx, storage.Task{ProjectID: p.ID, Title: "Sửa lỗi", Goal: "x", Status: "done", CostUSD: 0.42, CreatedBy: "human:a@x.io"})
	with, _ := st.Tasks().Create(ctx, storage.Task{ProjectID: p.ID, Title: "Có job", Goal: "y", Status: "done"})
	st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "task", Origin: "user", TaskID: with.ID, Status: "done"})
	for range 2 {
		if err := tasks.BackfillJobs(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ := st.Jobs().List(ctx, storage.JobFilter{Kind: "task"})
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	var got storage.Job
	for _, j := range jobs {
		if j.TaskID == old.ID {
			got = j
		}
	}
	if got.ID == "" || got.Status != "done" || got.CostUSD != 0.42 || got.Title != "Sửa lỗi" || got.CreatedBy != "human:a@x.io" ||
		got.CreatedAt.Sub(old.CreatedAt).Abs() > time.Second {
		t.Fatalf("backfilled = %+v (task at %v)", got, old.CreatedAt)
	}
}
