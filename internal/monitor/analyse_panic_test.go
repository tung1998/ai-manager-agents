package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// TestMaybeAnalysePanicDoesNotStickEventRunning: a panic inside the AI
// analysis goroutine (here: chat.Engine.Invoke with a nil *provider.Service,
// the same shape of bug as a nil deref deep in response parsing) must still
// move the event's AnalysisStatus off "running", not leave it stuck there
// forever.
func TestMaybeAnalysePanicDoesNotStickEventRunning(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	org := team.NewService(st, nil)
	if err := org.AddFromLibrary(ctx, p.ID, team.StarterAgent); err != nil {
		t.Fatal(err)
	}

	// providers nil: s.engine.Invoke(...) panics inside maybeAnalyse's
	// background goroutine via e.choices, same mechanism as
	// TestRunPanicEndsJobWithError in internal/chat.
	engine := chat.NewEngine(st, nil, nil)
	s := New(st, nil, engine)

	// no ops manager: the container check fails, the monitor goes down
	m, err := st.Monitors().Create(ctx, storage.Monitor{ProjectID: p.ID, Name: "redis", Type: "container", Target: "redis", IntervalS: 30, Enabled: true, AIEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	s.CheckNow(ctx, m.ID)
	s.CheckNow(ctx, m.ID) // two failures: down, AI analysis kicked off in the background

	deadline := time.Now().Add(5 * time.Second)
	for {
		evs, err := st.Monitors().Events(ctx, p.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) == 0 {
			t.Fatal("no event recorded for the down monitor")
		}
		if evs[0].AnalysisStatus != "running" {
			if evs[0].AnalysisStatus != "failed" {
				t.Fatalf("AnalysisStatus = %q, want failed", evs[0].AnalysisStatus)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("event stuck on AnalysisStatus=running: the panic was not recovered into a failed status")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
