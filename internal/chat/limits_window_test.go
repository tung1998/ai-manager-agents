package chat

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// limitsEngine is a bare Engine with just enough wiring to exercise
// keepLimits/overLimit against a real settings store.
func limitsEngine(t *testing.T) *Engine {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	return NewEngine(st, provs, usage.New(st, time.UTC))
}

func getLimits(t *testing.T, e *Engine, id string) (Limits, bool) {
	t.Helper()
	var l Limits
	ok, err := e.store.Settings().Get(context.Background(), LimitsKey(id), &l)
	if err != nil {
		t.Fatal(err)
	}
	return l, ok
}

// A hard "rejected" with no window detail (a flat block the CLI gave no
// reset time for) must still be saved — fallback depends on it.
func TestKeepLimitsSavesRejectedWithoutWindows(t *testing.T) {
	e := limitsEngine(t)
	p := storage.Provider{ID: "p1", Name: "P"}
	e.keepLimits(p, &Limits{Status: "rejected", UpdatedAt: time.Now().UTC()})
	l, ok := getLimits(t, e, p.ID)
	if !ok || l.Status != "rejected" {
		t.Fatalf("limits = %+v, ok=%v", l, ok)
	}
}

// A status-only report that isn't "rejected" (e.g. "allowed" with no window
// detail in that event) must not wipe out window data already on file.
func TestKeepLimitsKeepsOldWindowsOnStatusOnlyReport(t *testing.T) {
	e := limitsEngine(t)
	p := storage.Provider{ID: "p1", Name: "P"}
	want := map[string]LimitWindow{"five_hour": {Utilization: 0.4, ResetsAt: time.Now().Add(time.Hour)}}
	e.keepLimits(p, &Limits{Status: "allowed", Windows: want, UpdatedAt: time.Now().UTC()})
	e.keepLimits(p, &Limits{Status: "allowed", UpdatedAt: time.Now().UTC()})
	l, ok := getLimits(t, e, p.ID)
	if !ok || len(l.Windows) != 1 {
		t.Fatalf("limits = %+v, ok=%v (expected old windows kept)", l, ok)
	}
}

// overLimit treats a windowless "rejected" as hit only within the cooldown
// since it was reported, then lets it expire.
func TestOverLimitWindowlessRejectedCooldown(t *testing.T) {
	e := limitsEngine(t)
	p := storage.Provider{ID: "p1", Name: "P"}
	e.keepLimits(p, &Limits{Status: "rejected", UpdatedAt: time.Now().UTC()})
	if !e.overLimit(context.Background(), p.ID) {
		t.Fatal("expected over limit right after a windowless rejection")
	}
	e.keepLimits(p, &Limits{Status: "rejected", UpdatedAt: time.Now().UTC().Add(-RejectedCooldown - time.Minute)})
	if e.overLimit(context.Background(), p.ID) {
		t.Fatal("expected cooldown to have expired")
	}
}
