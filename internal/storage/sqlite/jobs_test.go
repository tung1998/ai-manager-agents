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

func TestFailRunningCountsCostAndEndsTasks(t *testing.T) { // I6
	ctx := context.Background()
	st, p := openStore(t)
	now := time.Now().UTC()
	j, _ := st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "task", Origin: "automation", OriginID: "a", Status: "running", StartedAt: &now})
	cost := 0.4
	st.Runs().Create(ctx, storage.Run{Kind: "task", ProjectID: p.ID, JobID: j.ID, Status: "ok", CostUSD: &cost, InputTokens: 7})
	st.Jobs().FailRunning(ctx, "restart", "restart", now)
	if got, _ := st.Jobs().Get(ctx, j.ID); got.CostUSD != 0.4 || got.InputTokens != 7 {
		t.Fatalf("failed job = %+v", got)
	}
	task, _ := st.Tasks().Create(ctx, storage.Task{ProjectID: p.ID, Title: "x", Goal: "x", Mode: "single", Status: "running"})
	if n, err := st.Tasks().FailRunning(ctx, "office khởi động lại", now); err != nil || n != 1 {
		t.Fatalf("fail tasks = %d %v", n, err)
	}
	if got, _ := st.Tasks().Get(ctx, task.ID); got.Status != "failed" || got.FinishedAt == nil {
		t.Fatalf("task = %+v", got)
	}
}

func TestDebounceOnlyTouchesPending(t *testing.T) { // I2
	ctx := context.Background()
	st, p := openStore(t)
	now := time.Now().UTC()
	j, _ := st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "chat_turn", Origin: "automation", OriginID: "a", Status: "pending", DebounceKey: "k", NextAttemptAt: &now})
	st.Jobs().Claim(ctx, now.Add(time.Second), 1, nil) // the runner took it
	if ok, err := st.Jobs().Debounce(ctx, j.ID, `{"n":2}`, now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("debounced a running job: %v %v", ok, err)
	}
	if got, _ := st.Jobs().Get(ctx, j.ID); got.Status != "running" || got.Payload == `{"n":2}` {
		t.Fatalf("job = %+v", got)
	}
}

func TestScriptJobsAndAutomations(t *testing.T) { // ADR-041
	ctx := context.Background()
	st, p := openStore(t)
	j, err := st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "script", Origin: "automation", OriginID: "a", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Jobs().SetOutput(ctx, j.ID, "hello\n", 3); err != nil {
		t.Fatal(err)
	}
	child, _ := st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "chat_turn", Origin: "automation", OriginID: "a", Trigger: "escalate", Status: "pending", ParentJobID: j.ID})
	got, _ := st.Jobs().Get(ctx, j.ID)
	if got.Output != "hello\n" || got.ExitCode == nil || *got.ExitCode != 3 {
		t.Fatalf("script job = %+v", got)
	}
	if c, _ := st.Jobs().Get(ctx, child.ID); c.ParentJobID != j.ID {
		t.Fatalf("child = %+v", c)
	}
	if list, _ := st.Jobs().List(ctx, storage.JobFilter{Kind: "script"}); len(list) != 1 {
		t.Fatalf("script jobs = %d", len(list))
	}
	a, err := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "log", Source: "schedule", Action: "script", Enabled: true,
		Script:   storage.AutomationScript{Lang: "bash", Body: "echo ok", TimeoutS: 60},
		Escalate: storage.AutomationEscalate{When: "failure", Action: "chat", Prompt: "Lỗi: {{output}}"}})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Automations().Get(ctx, a.ID); b.Script.Body != "echo ok" || b.Escalate.When != "failure" || b.Action != "script" {
		t.Fatalf("automation = %+v", b)
	}
}
