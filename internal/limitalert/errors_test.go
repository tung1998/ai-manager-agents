package limitalert_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/limitalert"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// Failed prompts: told once a streak reaches its count, once when too many
// fall in a window, and once more when the connection works again; a prompt
// over budget is not a failure.
func TestErrorAlerts(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	var sent []string
	a := limitalert.New(st, func(_ context.Context, _, _, text string) error {
		sent = append(sent, text)
		return nil
	})
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	run := func(status string, min int) {
		a.Run(ctx, storage.Run{ProviderID: "prv_1", ProviderName: "Codex", Model: "gpt-5.5", Status: status, Error: "unauthorized (401)", CreatedAt: at.Add(time.Duration(min) * time.Minute)})
	}
	run("error", 0)
	if len(sent) != 0 {
		t.Fatal("told with no destination")
	}
	limitalert.Save(ctx, st, limitalert.Settings{ChannelID: "chn_1", ChatID: "c9", Errors: &limitalert.ErrorRule{Streak: 3, Count: 4, Window: 30}})
	run("error", 1)
	run("blocked", 2) // budget: neither a failure nor a success
	run("error", 3)
	if len(sent) != 0 {
		t.Fatalf("too early: %q", sent)
	}
	run("error", 4) // 3 in a row
	run("error", 5)
	if len(sent) != 1 || !strings.Contains(sent[0], "3 lần gửi prompt lỗi liên tiếp") || !strings.Contains(sent[0], "unauthorized (401)") {
		t.Fatalf("streak: %q", sent)
	}
	run("ok", 6)
	if len(sent) != 2 || !strings.Contains(sent[1], "được lại") {
		t.Fatalf("recovered: %q", sent)
	}
	run("ok", 7)
	// spread out, never 3 in a row: the window rule (4 within 30 minutes), quiet a window after
	run("error", 40)
	run("ok", 41)
	run("error", 42)
	run("ok", 43)
	run("error", 44)
	run("ok", 45)
	run("error", 46)
	if len(sent) != 3 || !strings.Contains(sent[2], "4 lần gửi prompt lỗi trong 30 phút") {
		t.Fatalf("window: %q", sent)
	}
	run("error", 50)
	if len(sent) != 3 {
		t.Fatalf("told again within the window: %q", sent)
	}
}

// A failed send must not latch toldStreak/told: the next failing Run() has
// to retry the alert, and no "recovered" message should fire without a
// failure alert actually having gone through.
func TestErrorAlertRetriesAfterNotifyFailure(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	var sent []string
	fail := true
	a := limitalert.New(st, func(_ context.Context, _, _, text string) error {
		if fail {
			return fmt.Errorf("transient send error")
		}
		sent = append(sent, text)
		return nil
	})
	limitalert.Save(ctx, st, limitalert.Settings{ChannelID: "chn_1", ChatID: "c9", Errors: &limitalert.ErrorRule{Streak: 3, Count: 0, Window: 30}})
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	run := func(status string, min int) {
		a.Run(ctx, storage.Run{ProviderID: "prv_1", ProviderName: "Codex", Model: "gpt-5.5", Status: status, Error: "unauthorized (401)", CreatedAt: at.Add(time.Duration(min) * time.Minute)})
	}
	run("error", 0)
	run("error", 1)
	run("error", 2) // crosses the streak threshold, but notify fails
	if len(sent) != 0 {
		t.Fatalf("notify failed, should not have recorded a send: %q", sent)
	}
	run("error", 3) // still failing: must retry since toldStreak wasn't wrongly latched
	if len(sent) != 0 {
		t.Fatalf("notify still failing: %q", sent)
	}
	fail = false
	run("error", 4) // notify now succeeds
	if len(sent) != 1 || !strings.Contains(sent[0], "lần gửi prompt lỗi liên tiếp") {
		t.Fatalf("expected the failure alert to go through: %q", sent)
	}
	run("ok", 5)
	if len(sent) != 2 || !strings.Contains(sent[1], "được lại") {
		t.Fatalf("recovery should fire once a failure alert actually went through: %q", sent)
	}
}

// Two Run() calls for the same provider can run concurrently (usage.Service
// records each result in its own goroutine), so a first call still inside
// notify() must hold a.errMu the whole time: a second call arriving while
// the first is in flight has to block on the lock, not read an unarmed
// streak and send a duplicate alert. Proven by: once the first call is
// confirmed blocked in notify, the second call must NOT be able to finish
// within a generous window — if it does, it got through without waiting for
// the lock, which is exactly the old bug (lock released before notify).
func TestErrorAlertConcurrentDuplicateSend(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	var mu sync.Mutex
	var sent []string
	inNotify := make(chan struct{})
	release := make(chan struct{})
	var first bool
	a := limitalert.New(st, func(_ context.Context, _, _, text string) error {
		mu.Lock()
		isFirst := !first
		first = true
		mu.Unlock()
		if isFirst {
			close(inNotify)
			<-release
		}
		mu.Lock()
		sent = append(sent, text)
		mu.Unlock()
		return nil
	})
	limitalert.Save(ctx, st, limitalert.Settings{ChannelID: "chn_1", ChatID: "c9", Errors: &limitalert.ErrorRule{Streak: 3, Count: 0, Window: 30}})
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	run := func(min int) {
		a.Run(ctx, storage.Run{ProviderID: "prv_1", ProviderName: "Codex", Model: "gpt-5.5", Status: "error", Error: "unauthorized (401)", CreatedAt: at.Add(time.Duration(min) * time.Minute)})
	}
	run(0)
	run(1)

	done3 := make(chan struct{})
	go func() {
		run(2) // the 3rd failure: crosses the streak threshold, blocks in notify holding a.errMu
		close(done3)
	}()
	<-inNotify // confirmed: run(2) is inside notify

	done4 := make(chan struct{})
	go func() {
		run(3) // the 4th failure, concurrent with run(2)'s still-unresolved send
		close(done4)
	}()
	select {
	case <-done4:
		t.Fatal("the 4th Run() finished while the 3rd was still inside notify() for the same provider — a.errMu was released too early, letting it see an unarmed streak and send a duplicate alert")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	<-done3
	<-done4

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("expected exactly one alert from concurrent Run() calls, got %q", sent)
	}
}

// A success landing for the same provider while a failure alert is still in
// flight must block on a.errMu, not run ahead of it and let the failure's
// delayed commit later overwrite its own reset with a stale "told". Proven
// the same way: the success Run() must not be able to finish while the
// failure send is still blocked in notify().
func TestErrorAlertConcurrentSuccessInterleave(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	var mu sync.Mutex
	var sent []string
	inNotify := make(chan struct{})
	release := make(chan struct{})
	var first bool
	a := limitalert.New(st, func(_ context.Context, _, _, text string) error {
		mu.Lock()
		isFirst := !first
		first = true
		mu.Unlock()
		if isFirst {
			close(inNotify)
			<-release // the failure alert: block here until released
		}
		mu.Lock()
		sent = append(sent, text)
		mu.Unlock()
		return nil
	})
	limitalert.Save(ctx, st, limitalert.Settings{ChannelID: "chn_1", ChatID: "c9", Errors: &limitalert.ErrorRule{Streak: 3, Count: 0, Window: 30}})
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	run := func(status string, min int) {
		a.Run(ctx, storage.Run{ProviderID: "prv_1", ProviderName: "Codex", Model: "gpt-5.5", Status: status, Error: "unauthorized (401)", CreatedAt: at.Add(time.Duration(min) * time.Minute)})
	}
	run("error", 0)
	run("error", 1)

	doneFail := make(chan struct{})
	go func() {
		run("error", 2) // crosses the streak threshold, blocks in notify holding a.errMu
		close(doneFail)
	}()
	<-inNotify // confirmed: the failure send is inside notify

	doneOK := make(chan struct{})
	go func() {
		run("ok", 3) // must wait for a.errMu, can't interleave with the failure's commit
		close(doneOK)
	}()
	select {
	case <-doneOK:
		t.Fatal("the success Run() finished while the failure alert was still inside notify() — a.errMu was released too early, letting the success race the failure's delayed commit")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	<-doneFail
	<-doneOK

	run("ok", 4) // a second success: must not produce a second "recovered" message

	mu.Lock()
	defer mu.Unlock()
	var recovered int
	for _, s := range sent {
		if strings.Contains(s, "được lại") {
			recovered++
		}
	}
	if len(sent) != 2 || recovered != 1 {
		t.Fatalf("expected one failure alert and exactly one recovery message, got %q", sent)
	}
}

// The defaults: 3 in a row, or 5 within 30 minutes.
func TestErrorRuleDefaults(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	if e := limitalert.Load(ctx, st).Errors; e == nil || *e != limitalert.DefaultErrors {
		t.Fatalf("errors = %+v", e)
	}
	limitalert.Save(ctx, st, limitalert.Settings{Errors: &limitalert.ErrorRule{}})
	if e := limitalert.Load(ctx, st).Errors; *e != (limitalert.ErrorRule{}) {
		t.Fatalf("turned off = %+v", e)
	}
}
