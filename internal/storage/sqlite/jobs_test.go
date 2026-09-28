package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func openStore(t *testing.T) (storage.Store, storage.Repo) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	return st, p
}

func TestJobsQueueAndFinish(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	now := time.Now().UTC()
	j, err := st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "task", Origin: "automation", OriginID: "aut_1", Trigger: "webhook", Status: "pending", DedupeKey: "d1", NextAttemptAt: &now})
	if err != nil || j.ID == "" {
		t.Fatalf("create = %+v %v", j, err)
	}
	if _, err := st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "task", Origin: "automation", OriginID: "aut_1", Status: "pending", DedupeKey: "d1"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate dedupe err = %v", err)
	}
	// busy origins are skipped; claimed jobs become running
	if got, _ := st.Jobs().Claim(ctx, now.Add(time.Second), 2, []string{"aut_1"}); len(got) != 0 {
		t.Fatalf("claimed a busy origin: %+v", got)
	}
	got, err := st.Jobs().Claim(ctx, now.Add(time.Second), 2, nil)
	if err != nil || len(got) != 1 || got[0].Status != "running" || got[0].StartedAt == nil {
		t.Fatalf("claim = %+v %v", got, err)
	}
	cost := 0.25
	st.Runs().Create(ctx, storage.Run{Kind: "task", ProjectID: p.ID, JobID: j.ID, Status: "ok", CostUSD: &cost, InputTokens: 10, OutputTokens: 4, DurationMS: 1500})
	st.Runs().Create(ctx, storage.Run{Kind: "task", ProjectID: p.ID, JobID: j.ID, Status: "ok", CostUSD: &cost, InputTokens: 5, OutputTokens: 1, DurationMS: 500})
	done, err := st.Jobs().Finish(ctx, j.ID, "done", "", "", now.Add(3*time.Second))
	if err != nil || done.CostUSD != 0.5 || done.InputTokens != 15 || done.DurationMS < 2000 {
		t.Fatalf("finish = %+v %v", done, err)
	}
	// restart: running jobs fail
	st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "chat_turn", Origin: "user", Status: "running", StartedAt: &now})
	if n, _ := st.Jobs().FailRunning(ctx, "restart", "office khởi động lại", now); n != 1 {
		t.Fatalf("fail running = %d", n)
	}
	list, _ := st.Jobs().List(ctx, storage.JobFilter{ProjectID: p.ID, Status: "failed"})
	if len(list) != 1 || list[0].ErrorCode != "restart" {
		t.Fatalf("failed list = %+v", list)
	}
	stats, _ := st.Jobs().Stats(ctx, storage.JobFilter{ProjectID: p.ID}, "kind")
	if len(stats) != 2 {
		t.Fatalf("stats by kind = %+v", stats)
	}
}

func TestAutomationsDue(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	past := time.Now().UTC().Add(-time.Minute)
	a, err := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "sáng", Source: "schedule", Action: "task", Enabled: true,
		Config: storage.AutomationConfig{Cron: "0 8 * * 1-5", Timezone: "Asia/Ho_Chi_Minh"}, NextRunAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "hook", Source: "webhook", Action: "chat", Enabled: true, NextRunAt: &past})
	due, _ := st.Automations().Due(ctx, time.Now().UTC())
	if len(due) != 1 || due[0].ID != a.ID || due[0].Config.Timezone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("due = %+v", due)
	}
	a.Enabled = false
	st.Automations().Update(ctx, a)
	if due, _ := st.Automations().Due(ctx, time.Now().UTC()); len(due) != 0 {
		t.Fatalf("disabled is due: %+v", due)
	}
}
