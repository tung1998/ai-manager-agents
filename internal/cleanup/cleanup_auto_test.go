package cleanup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// autoOnce must not clobber settings an admin saves via SetAuto while a run
// is in flight: only LastRun/LastMsg should come from the run that just
// finished, everything else must come from the freshest saved settings.
func TestAutoOnceDoesNotClobberConcurrentSetAuto(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	s := New(st, nil, nil, tmp, filepath.Join(tmp, "o.db"))
	if _, err := s.SetAuto(ctx, Auto{Enabled: true, Days: 30, Level: LevelContent}); err != nil {
		t.Fatal(err)
	}

	unblock := make(chan struct{})
	started := make(chan struct{})
	s.runFn = func(ctx context.Context, r Request) (Result, error) {
		close(started)
		<-unblock
		return Result{Done: 3}, nil
	}

	done := make(chan struct{})
	go func() {
		s.autoOnce(ctx)
		close(done)
	}()

	<-started
	if _, err := s.SetAuto(ctx, Auto{Enabled: false, Days: 7, Level: LevelSummary}); err != nil {
		t.Fatal(err)
	}
	close(unblock)
	<-done

	a := s.AutoSettings(ctx)
	if a.Enabled {
		t.Fatalf("Enabled = true, want the false set mid-run")
	}
	if a.Days != 7 {
		t.Fatalf("Days = %d, want 7 (set mid-run)", a.Days)
	}
	if a.Level != LevelSummary {
		t.Fatalf("Level = %q, want %q (set mid-run)", a.Level, LevelSummary)
	}
	if a.LastRun == nil || time.Since(*a.LastRun) > time.Minute {
		t.Fatalf("LastRun = %v, want set to just now", a.LastRun)
	}
	if a.LastMsg == "" {
		t.Fatal("LastMsg not set")
	}
}
