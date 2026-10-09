package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A connection past the stop threshold set on one of its windows takes no
// turn until that window resets; with a fallback still under, that one runs
// (ADR-136).
func TestChoicesSkipCappedConnection(t *testing.T) {
	ctx := context.Background()
	e := limitsEngine(t)
	main, err := e.providers.Create(ctx, provider.Input{Name: "Main", Kind: storage.ProviderClaudeCLI,
		LimitCaps: map[string]int{"seven_day": 95}})
	if err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(48 * time.Hour)
	e.keepLimits(main, &Limits{Status: "allowed", UpdatedAt: time.Now().UTC(),
		Windows: map[string]LimitWindow{"five_hour": {Utilization: 0.99, ResetsAt: time.Now().Add(time.Hour)}, "seven_day": {Utilization: 0.94, ResetsAt: reset}}})
	agent := storage.Agent{ModelTier: storage.TierBalanced}
	if c, err := e.choices(ctx, agent); err != nil || len(c) != 1 {
		t.Fatalf("under the weekly cap (no cap on 5 hours): choices = %v, %v", c, err)
	}

	e.keepLimits(main, &Limits{Status: "allowed", UpdatedAt: time.Now().UTC(),
		Windows: map[string]LimitWindow{"seven_day": {Utilization: 0.95, ResetsAt: reset}}})
	_, err = e.choices(ctx, agent)
	var ce *CapError
	if !errors.As(err, &ce) || ce.Window != "seven_day" || !ce.Until.Equal(reset) || !OutOfTokens(err) {
		t.Fatalf("at the cap: err = %v", err)
	}
	if until, hit := e.CapStop(ctx, agent); !hit || !until.Equal(reset) {
		t.Fatalf("CapStop = %v, %v", until, hit)
	}

	spare, err := e.providers.Create(ctx, provider.Input{Name: "Spare", Kind: storage.ProviderClaudeCLI})
	if err != nil {
		t.Fatal(err)
	}
	agent.FallbackProviderIDs = []string{spare.ID}
	if c, err := e.choices(ctx, agent); err != nil || len(c) != 1 || c[0].Provider.ID != spare.ID {
		t.Fatalf("with a fallback: choices = %v, %v", c, err)
	}
	if _, hit := e.CapStop(ctx, agent); hit {
		t.Fatal("CapStop with a fallback under its cap")
	}

	// the window reset: free again
	e.keepLimits(main, &Limits{Status: "allowed", UpdatedAt: time.Now().UTC(),
		Windows: map[string]LimitWindow{"seven_day": {Utilization: 0.95, ResetsAt: time.Now().Add(-time.Minute)}}})
	if c, err := e.choices(ctx, storage.Agent{ModelTier: storage.TierBalanced}); err != nil || len(c) != 1 {
		t.Fatalf("after the reset: choices = %v, %v", c, err)
	}
}

func TestBadLimitCap(t *testing.T) {
	e := limitsEngine(t)
	for _, caps := range []map[string]int{{"seven_day": 120}, {"hourly": 50}} {
		if _, err := e.providers.Create(context.Background(), provider.Input{Name: "X", Kind: storage.ProviderClaudeCLI, LimitCaps: caps}); !errors.Is(err, provider.ErrBadCap) {
			t.Fatalf("%v: err = %v", caps, err)
		}
	}
}
