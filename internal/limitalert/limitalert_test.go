package limitalert_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/limitalert"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// Past the threshold a window is told once per cycle, once more at 95%; a
// new cycle (another reset) starts over; nothing without a destination.
func TestAlerts(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	var sent []string
	a := limitalert.New(st, func(_ context.Context, channelID, chatID, text string) error {
		sent = append(sent, channelID+"|"+chatID+"|"+text)
		return nil
	})
	reset := time.Now().Add(2 * time.Hour)
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.9, reset)
	if len(sent) != 0 {
		t.Fatal("told with no destination")
	}
	limitalert.Save(ctx, st, limitalert.Settings{ChannelID: "chn_1", ChatID: "c9", Threshold: 80})
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.5, reset)
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.82, reset)
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.85, reset)
	if len(sent) != 1 || !strings.HasPrefix(sent[0], "chn_1|c9|") || !strings.Contains(sent[0], "82%") {
		t.Fatalf("sent = %q", sent)
	}
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.96, reset)
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.99, reset)
	if len(sent) != 2 || !strings.Contains(sent[1], "96%") {
		t.Fatalf("at 95%%: %q", sent)
	}
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.81, reset.Add(5*time.Hour)) // the next cycle
	a.Check(ctx, "prv_1", "Claude Code", "seven_day", 0.81, reset)                  // another window
	if len(sent) != 4 {
		t.Fatalf("new cycle / window: %q", sent)
	}
}

// A notify error must not be treated as "told": the next Check for the same
// level has to try sending again.
func TestAlertRetriesAfterNotifyError(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	limitalert.Save(ctx, st, limitalert.Settings{ChannelID: "chn_1", ChatID: "c9", Threshold: 80})
	reset := time.Now().Add(2 * time.Hour)

	fail := true
	var sent []string
	a := limitalert.New(st, func(_ context.Context, channelID, chatID, text string) error {
		if fail {
			return errors.New("bot down")
		}
		sent = append(sent, text)
		return nil
	})

	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.82, reset)
	if len(sent) != 0 {
		t.Fatalf("should not have sent: %q", sent)
	}
	fail = false
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.85, reset)
	if len(sent) != 1 {
		t.Fatalf("expected retry to send once notify recovers, got %q", sent)
	}

	// A later failed 95% attempt must not roll "told" back below 80: a
	// follow-up Check still at 80% must not re-alert.
	fail = true
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.96, reset)
	if len(sent) != 1 {
		t.Fatalf("95%% send should have failed and not recorded: %q", sent)
	}
	fail = false
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.85, reset)
	if len(sent) != 1 {
		t.Fatalf("must not re-alert already-told 80%% level: %q", sent)
	}
	a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.97, reset)
	if len(sent) != 2 {
		t.Fatalf("expected 95%% retry to send once notify recovers, got %q", sent)
	}
}

// Runs ending together report the same window at once: told once.
func TestAlertOnceWhenTogether(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	limitalert.Save(ctx, st, limitalert.Settings{ChannelID: "chn_1", ChatID: "c9", Threshold: 80})
	var n atomic.Int32
	a := limitalert.New(st, func(context.Context, string, string, string) error { n.Add(1); return nil })
	reset := time.Now().Add(2 * time.Hour)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); a.Check(ctx, "prv_1", "Claude Code", "five_hour", 0.85, reset) }()
	}
	wg.Wait()
	if n.Load() != 1 {
		t.Fatalf("told %d times", n.Load())
	}
}
