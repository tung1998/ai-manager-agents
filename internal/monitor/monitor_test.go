package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ops"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func setup(t *testing.T) (*Service, storage.Store, storage.Repo, *ops.Manager) {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ := st.Repos().Create(context.Background(), storage.Repo{Name: "p", Path: t.TempDir()})
	m := ops.NewManager(st, t.TempDir(), nil)
	t.Cleanup(m.Shutdown)
	return New(st, m, nil), st, p, m
}

// A process monitor follows the process: up while it runs, down at once
// when it stops (one failure is enough), with an event for each change.
func TestProcessMonitor(t *testing.T) {
	s, st, p, o := setup(t)
	ctx := context.Background()
	proc, _ := st.Processes().Create(ctx, storage.Process{ProjectID: p.ID, Name: "dev", Kind: "service", Command: "sleep 30"})
	if err := o.Start(ctx, proc.ID); err != nil {
		t.Fatal(err)
	}
	m, _ := st.Monitors().Create(ctx, storage.Monitor{ProjectID: p.ID, Name: "dev", Type: "process", Target: proc.ID, IntervalS: 30, Enabled: true})
	if m, _ = s.CheckNow(ctx, m.ID); m.Status != "up" {
		t.Fatalf("running: %+v", m)
	}
	o.Stop(proc.ID)
	deadline := time.Now().Add(5 * time.Second)
	for o.State(proc.ID).Status == "running" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if m, _ = s.CheckNow(ctx, m.ID); m.Status != "down" {
		t.Fatalf("stopped: %+v", m)
	}
	evs, _ := st.Monitors().Events(ctx, p.ID, 10)
	if len(evs) != 1 || evs[0].Kind != "down" {
		t.Fatalf("events: %+v", evs)
	}
	// the process deleted: its monitor goes with it
	if err := st.Processes().Delete(ctx, proc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Monitors().Get(ctx, m.ID); err != storage.ErrNotFound {
		t.Fatalf("monitor of a deleted process: %v", err)
	}
}

// A container monitor needs two failures in a row before it is down.
func TestContainerFlapGuard(t *testing.T) {
	s, st, p, _ := setup(t)
	s.ops = nil // no compose: every check fails
	ctx := context.Background()
	m, _ := st.Monitors().Create(ctx, storage.Monitor{ProjectID: p.ID, Name: "db", Type: "container", Target: "db", IntervalS: 30, Enabled: true})
	if m, _ = s.CheckNow(ctx, m.ID); m.Status == "down" || m.Fails != 1 {
		t.Fatalf("one failure must not flip: %+v", m)
	}
	if m, _ = s.CheckNow(ctx, m.ID); m.Status != "down" {
		t.Fatalf("two failures: %+v", m)
	}
}
