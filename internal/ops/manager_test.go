package ops

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func setup(t *testing.T) (*Manager, storage.Store, storage.Repo) {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	proj, err := st.Repos().Create(context.Background(), storage.Repo{Name: "p", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(st, t.TempDir(), nil)
	t.Cleanup(m.Shutdown)
	return m, st, proj
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestServiceLifecycle(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "dev", Kind: "service",
		Command: `echo hello; echo "  ➜ Local: \033[36mhttp://localhost:4567/\033[0m"; sh -c 'sleep 60' & wait`})
	if err := m.Start(ctx, def.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx, def.ID); err != ErrRunning {
		t.Fatalf("second start: %v", err)
	}
	waitFor(t, "port", func() bool { return m.State(def.ID).Port == 4567 })
	lines, _, _ := m.Since(def.ID, 0)
	if !strings.Contains(lines[1].Text, "hello") || strings.Contains(m.Tail(def.ID, 10), "\x1b") {
		t.Fatalf("lines: %+v", lines)
	}
	m.Sample()
	if m.State(def.ID).MemBytes == 0 {
		t.Fatal("no memory sample")
	}
	if err := m.Stop(def.ID); err != nil {
		t.Fatal(err)
	}
	if s := m.State(def.ID); s.Status != "stopped" || s.PID != 0 {
		t.Fatalf("after stop: %+v", s)
	}
}

func TestJobAndCrashRestart(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	ok, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "build", Kind: "job", Command: "echo built"})
	_ = m.Start(ctx, ok.ID)
	waitFor(t, "job exit", func() bool { return m.State(ok.ID).Status == "exited" })

	bad, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "api", Kind: "service", Autorestart: true, Command: "echo boom; exit 3"})
	_ = m.Start(ctx, bad.ID)
	waitFor(t, "restart", func() bool { return m.State(bad.ID).Restarts >= 1 })
	_ = m.Stop(bad.ID)
	if s := m.State(bad.ID).Status; s != "stopped" && s != "crashed" {
		t.Fatalf("stop during restart: %s", s)
	}

	escape, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "x", Cwd: "../..", Command: "true"})
	if err := m.Start(ctx, escape.ID); err != ErrBadCwd {
		t.Fatalf("cwd escape: %v", err)
	}
}

// A process's own environment is stored and runs over the office's.
func TestProcessEnv(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "env", Kind: "job",
		Command: `echo "port=$PORT ci=$CI"`, Env: map[string]string{"PORT": "4321", "CI": "1"}})
	if got, _ := st.Processes().Get(ctx, def.ID); got.Env["PORT"] != "4321" {
		t.Fatalf("stored env: %v", got.Env)
	}
	_ = m.Start(ctx, def.ID)
	waitFor(t, "job exit", func() bool { return m.State(def.ID).Status == "exited" })
	if out := m.Tail(def.ID, 10); !strings.Contains(out, "port=4321 ci=1") {
		t.Fatalf("output: %q", out)
	}
	def.Env = nil
	if err := st.Processes().Update(ctx, def); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Processes().Get(ctx, def.ID); len(got.Env) != 0 {
		t.Fatalf("cleared env: %v", got.Env)
	}
}

func TestResumeAfterRestart(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "resume.json")
	m.SetResumeFile(file)
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "dev", Kind: "service", Command: "sleep 60"})
	_ = m.Start(ctx, def.ID)

	m.SaveResume() // not asked: nothing saved
	m.Shutdown()
	m.Autostart(ctx)
	if s := m.State(def.ID).Status; s != "stopped" {
		t.Fatalf("started without being asked: %s", s)
	}

	_ = m.Start(ctx, def.ID)
	m.ResumeAfterRestart(true)
	m.SaveResume()
	m.Shutdown()
	// the next office: a new manager on the same file
	next := NewManager(st, t.TempDir(), nil)
	t.Cleanup(next.Shutdown)
	next.SetResumeFile(file)
	next.Autostart(ctx)
	if s := next.State(def.ID).Status; s != "running" {
		t.Fatalf("not resumed: %s", s)
	}
	next.Shutdown()
	next.Autostart(ctx) // the file is used once
	if s := next.State(def.ID).Status; s != "stopped" {
		t.Fatalf("resumed twice: %s", s)
	}
}

// A status kept in memory is told as it changes (started, ended), so the
// dashboard follows it without asking.
func TestOnChange(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	var n atomic.Int32
	m.OnChange(func() { n.Add(1) })
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "build", Kind: "job", Command: "echo built"})
	_ = m.Start(ctx, def.ID)
	waitFor(t, "job exit", func() bool { return m.State(def.ID).Status == "exited" })
	waitFor(t, "start and end told", func() bool { return n.Load() >= 2 })
}
