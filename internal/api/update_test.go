package api

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ops"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func TestCommitSubject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	if got := commitSubject("../..", "HEAD"); got == "" {
		t.Fatal("HEAD of this repo has no subject")
	}
	if got := commitSubject("../..", "0000000000000000000000000000000000000000"); got != "" {
		t.Fatalf("unknown revision = %q", got)
	}
	if got := commitSubject(t.TempDir(), "HEAD"); got != "" {
		t.Fatalf("no repo = %q", got)
	}
	if b := loadBuild("dev", ""); b.Subject != "" || b.Version != "dev" {
		t.Fatalf("no source = %+v", b)
	}
}

// TestBusyWorkCountsRunningProcesses guards against the "tasks" count
// staying stuck at 0 (regression after Việc was removed, ADR-057).
func TestBusyWorkCountsRunningProcesses(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	proj, err := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m := ops.NewManager(st, t.TempDir(), nil)
	defer m.Shutdown()

	s := &server{cfg: Config{Ops: m}}
	if b := s.busyWork(); b["tasks"] != 0 {
		t.Fatalf("no process running: tasks = %d, want 0", b["tasks"])
	}

	def, err := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "dev", Kind: "service",
		Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx, def.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(m.Running()) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if b := s.busyWork(); b["tasks"] != 1 {
		t.Fatalf("one process running: tasks = %d, want 1", b["tasks"])
	}
}
