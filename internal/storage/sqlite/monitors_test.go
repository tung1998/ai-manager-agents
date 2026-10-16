package sqlite_test

import (
	"context"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A monitor's own cooldown must not be defeated by another monitor's events
// filling up a project-wide, size-capped window.
func TestMonitorRecentAnalysis(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	repo := st.Monitors()

	mustMonitor := func(name string) string {
		m, err := repo.Create(ctx, storage.Monitor{ProjectID: p.ID, Name: name, Type: "container", Target: name})
		if err != nil {
			t.Fatalf("create monitor: %v", err)
		}
		return m.ID
	}
	monOther, monX, monY, monZ, monW := mustMonitor("other"), mustMonitor("x"), mustMonitor("y"), mustMonitor("z"), mustMonitor("w")

	add := func(monitorID, status string, at time.Time) storage.MonitorEvent {
		ev, err := repo.AddEvent(ctx, storage.MonitorEvent{MonitorID: monitorID, ProjectID: p.ID, Kind: "down", At: at})
		if err != nil {
			t.Fatalf("add event: %v", err)
		}
		ev.AnalysisStatus = status
		if err := repo.UpdateEvent(ctx, ev); err != nil {
			t.Fatalf("update event: %v", err)
		}
		return ev
	}

	now := time.Now().UTC()
	// flood with another monitor's events, more than the old 50-event window
	for i := 0; i < 60; i++ {
		add(monOther, "done", now.Add(-time.Duration(i)*time.Second))
	}
	recentDone := add(monX, "done", now.Add(-10*time.Minute))
	if hit, err := repo.RecentAnalysis(ctx, monX, "", now.Add(-30*time.Minute)); err != nil || !hit {
		t.Fatalf("expected recent done analysis to be found, hit=%v err=%v", hit, err)
	}
	// excluding the event itself when it's the only one should find nothing
	if hit, err := repo.RecentAnalysis(ctx, monX, recentDone.ID, now.Add(-30*time.Minute)); err != nil || hit {
		t.Fatalf("expected no match when excluding the only event, hit=%v err=%v", hit, err)
	}

	// a running analysis also counts
	add(monY, "running", now)
	if hit, err := repo.RecentAnalysis(ctx, monY, "", now.Add(-30*time.Minute)); err != nil || !hit {
		t.Fatalf("expected running analysis to be found, hit=%v err=%v", hit, err)
	}

	// failed, or older than the cooldown, must not count
	add(monZ, "failed", now)
	if hit, err := repo.RecentAnalysis(ctx, monZ, "", now.Add(-30*time.Minute)); err != nil || hit {
		t.Fatalf("expected failed analysis not to count, hit=%v err=%v", hit, err)
	}
	add(monW, "done", now.Add(-40*time.Minute))
	if hit, err := repo.RecentAnalysis(ctx, monW, "", now.Add(-30*time.Minute)); err != nil || hit {
		t.Fatalf("expected old analysis not to count, hit=%v err=%v", hit, err)
	}
}
