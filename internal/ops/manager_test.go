package ops

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
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

// TestStopDuringStartRace reproduces Stop() landing between cmd.Start() and
// launch() taking the lock on a fresh Start(): without the generation check
// launch()'s unconditional state overwrite resurrects the process as "running"
// even though Stop() already reported "stopped".
func TestStopDuringStartRace(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "svc", Kind: "service", Command: "sleep 30"})

	var once sync.Once
	afterStart = func() { once.Do(func() { _ = m.Stop(def.ID) }) }
	t.Cleanup(func() { afterStart = nil })

	if err := m.Start(ctx, def.ID); err != nil {
		t.Fatal(err)
	}
	// Reaching "stopped" at all requires cmd.Wait() to return, i.e. the real
	// process was actually killed, not just the status field flipped.
	waitFor(t, "stopped after start race", func() bool { return m.State(def.ID).Status == "stopped" })
	time.Sleep(300 * time.Millisecond)
	if s := m.State(def.ID); s.Status != "stopped" || s.PID != 0 {
		t.Fatalf("resurrected after start race: %+v", s)
	}
}

// TestStopDuringRestartRace reproduces Stop() landing between cmd.Start() and
// the lock during an auto-restart's launch() call: without the generation
// check, the service kept crashing and auto-restarting forever as if Stop had
// never been called.
func TestStopDuringRestartRace(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "api", Kind: "service", Autorestart: true, Command: "echo boom; exit 3"})

	if err := m.Start(ctx, def.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restarting", func() bool { return m.State(def.ID).Status == "restarting" })

	var once sync.Once
	afterStart = func() { once.Do(func() { _ = m.Stop(def.ID) }) }
	t.Cleanup(func() { afterStart = nil })

	waitFor(t, "stopped after restart race", func() bool { return m.State(def.ID).Status == "stopped" })
	restarts := m.State(def.ID).Restarts
	time.Sleep(1500 * time.Millisecond) // longer than the restart backoff: no further restart must happen
	if s := m.State(def.ID); s.Status != "stopped" || s.Restarts != restarts {
		t.Fatalf("resurrected/restarted after restart race: %+v", s)
	}
}

// TestStopBeforeStartRace reproduces Stop() landing even earlier: after the
// caller (Start) released its lock but before launch() calls cmd.Start() at
// all (building cmd/pipes/openLog takes time too). The generation must be
// captured under the same lock that decided to launch, not inside launch()
// itself, otherwise this earlier window still resurrects the process.
func TestStopBeforeStartRace(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "svc", Kind: "service", Command: "sleep 30"})

	var once sync.Once
	beforeStart = func() { once.Do(func() { _ = m.Stop(def.ID) }) }
	t.Cleanup(func() { beforeStart = nil })

	if err := m.Start(ctx, def.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped after before-start race", func() bool { return m.State(def.ID).Status == "stopped" })
	time.Sleep(300 * time.Millisecond)
	if s := m.State(def.ID); s.Status != "stopped" || s.PID != 0 {
		t.Fatalf("resurrected after before-start race: %+v", s)
	}
}

// TestStopBeforeRestartRace is TestStopBeforeStartRace's counterpart on the
// auto-restart path: Stop() lands after the AfterFunc released its lock but
// before the restart's launch() calls cmd.Start().
func TestStopBeforeRestartRace(t *testing.T) {
	m, st, proj := setup(t)
	ctx := context.Background()
	def, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "api", Kind: "service", Autorestart: true, Command: "echo boom; exit 3"})

	if err := m.Start(ctx, def.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restarting", func() bool { return m.State(def.ID).Status == "restarting" })

	var once sync.Once
	beforeStart = func() { once.Do(func() { _ = m.Stop(def.ID) }) }
	t.Cleanup(func() { beforeStart = nil })

	waitFor(t, "stopped after before-restart race", func() bool { return m.State(def.ID).Status == "stopped" })
	restarts := m.State(def.ID).Restarts
	time.Sleep(1500 * time.Millisecond)
	if s := m.State(def.ID); s.Status != "stopped" || s.Restarts != restarts {
		t.Fatalf("resurrected/restarted after before-restart race: %+v", s)
	}
}
