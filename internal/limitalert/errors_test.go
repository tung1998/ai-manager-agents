package limitalert_test

import (
	"context"
	"path/filepath"
	"strings"
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
