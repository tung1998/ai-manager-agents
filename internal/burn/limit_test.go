package burn

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func limitTestService(t *testing.T) (*Service, storage.Agent) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	p, err := st.Providers().Create(ctx, storage.Provider{Name: "P", Kind: storage.ProviderClaudeCLI, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: tmp})
	ag, err := st.Agents().Create(ctx, storage.Agent{ProjectID: repo.ID, Name: "main", ModelTier: "balanced"})
	if err != nil {
		t.Fatal(err)
	}
	ag.ProviderID = p.ID
	if err := st.Agents().Update(ctx, ag); err != nil {
		t.Fatal(err)
	}
	return New(st, nil, nil), ag
}

// A windowless "rejected" report (no reset time at all) must expire on its
// own cooldown, timed from when it was reported — not from "now" on every
// check, which would push the wait out forever.
func TestLimitHitWindowlessRejectedExpires(t *testing.T) {
	s, ag := limitTestService(t)
	ctx := context.Background()
	set := func(updatedAt time.Time) {
		s.store.Settings().Set(ctx, chat.LimitsKey(ag.ProviderID), chat.Limits{Status: "rejected", UpdatedAt: updatedAt})
	}

	set(time.Now().UTC())
	if _, hit := s.limitHit(ctx, ag.ID); !hit {
		t.Fatal("expected a fresh windowless rejection to be a hit")
	}

	set(time.Now().UTC().Add(-chat.RejectedCooldown - time.Minute))
	if _, hit := s.limitHit(ctx, ag.ID); hit {
		t.Fatal("expected a stale windowless rejection to have expired")
	}
}

// A "rejected" report that does carry a window still waits for that window's
// actual reset time, unaffected by the windowless-cooldown fallback.
func TestLimitHitUsesWindowResetWhenPresent(t *testing.T) {
	s, ag := limitTestService(t)
	ctx := context.Background()
	resetsAt := time.Now().Add(2 * time.Hour)
	s.store.Settings().Set(ctx, chat.LimitsKey(ag.ProviderID), chat.Limits{
		Status:    "rejected",
		UpdatedAt: time.Now().UTC(),
		Windows:   map[string]chat.LimitWindow{"five_hour": {Utilization: 1, ResetsAt: resetsAt}},
	})
	until, hit := s.limitHit(ctx, ag.ID)
	if !hit || !until.Equal(resetsAt) {
		t.Fatalf("until = %v, hit = %v, want %v", until, hit, resetsAt)
	}
}
