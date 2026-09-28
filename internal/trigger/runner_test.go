package trigger_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/trigger"
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

type fakeExec struct {
	mu    sync.Mutex
	chats []string
	tasks []string // "agent|goal"
	busy  int      // return ErrBusy this many times first
	fail  bool
}

func (f *fakeExec) RunChat(ctx context.Context, projectID, agentID, conv, prompt, edit string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy > 0 {
		f.busy--
		return "", trigger.ErrBusy
	}
	f.chats = append(f.chats, prompt)
	if f.fail {
		return "cnv_x", errors.New("HTTP 529")
	}
	return "cnv_x", nil
}
func (f *fakeExec) RunTask(ctx context.Context, projectID, agentID, goal, edit string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = append(f.tasks, agentID+"|"+goal)
	return "tsk_x", nil
}
func (f *fakeExec) RunQueuedTask(ctx context.Context, projectID, payload string) (string, error) {
	return "tsk_q", nil
}

func TestScheduleRunsOnceAndNoOverlap(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	ex := &fakeExec{}
	r := trigger.New(st, ex)
	past := time.Now().UTC().Add(-time.Hour) // missed many runs: catch up once
	a, _ := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "check", Source: "schedule", Action: "chat", Enabled: true,
		Prompt: "kiểm tra {{today}}", Config: storage.AutomationConfig{EveryMinutes: 5}, NextRunAt: &past})
	now := time.Now().UTC()
	r.Tick(ctx, now)
	r.Wait()
	r.Tick(ctx, now) // not due again
	r.Wait()
	if len(ex.chats) != 1 || !strings.HasPrefix(ex.chats[0], "kiểm tra ") {
		t.Fatalf("chats = %v", ex.chats)
	}
	a, _ = st.Automations().Get(ctx, a.ID)
	if a.NextRunAt == nil || !a.NextRunAt.After(now) || a.LastRunAt == nil {
		t.Fatalf("next = %v last = %v", a.NextRunAt, a.LastRunAt)
	}
	jobs, _ := st.Jobs().List(ctx, storage.JobFilter{OriginID: a.ID})
	if len(jobs) != 1 || jobs[0].Status != "done" || jobs[0].Trigger != "schedule" || jobs[0].Origin != "automation" {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestBusyRetriesAndFailuresDisable(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	ex := &fakeExec{busy: 1, fail: true}
	r := trigger.New(st, ex)
	a, _ := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "h", Source: "webhook", Action: "chat", Enabled: true,
		Limits: storage.AutomationLimits{DisableAfterFailures: 2}})
	for i := 0; i < 2; i++ {
		if _, status, err := r.Enqueue(ctx, a, "webhook", `{"n":1}`, fmt.Sprint(i), ""); err != nil || status != "queued" {
			t.Fatalf("enqueue = %s %v", status, err)
		}
	}
	now := time.Now().UTC()
	r.Tick(ctx, now) // first is busy → retry in 60s; second runs and fails
	r.Wait()
	r.Tick(ctx, now.Add(61*time.Second))
	r.Wait()
	a, _ = st.Automations().Get(ctx, a.ID)
	if a.Enabled || a.DisabledCode != "failures" || a.Failures != 2 {
		t.Fatalf("automation = %+v", a)
	}
	failed, _ := st.Jobs().List(ctx, storage.JobFilter{OriginID: a.ID, Status: "failed"})
	if len(failed) != 2 || failed[0].ErrorCode != "agent_error" {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestRestartedJobDoesNotRunAgain(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	ex := &fakeExec{}
	a, _ := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "h", Source: "webhook", Action: "chat", Enabled: true})
	now := time.Now().UTC()
	st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "chat_turn", Origin: "automation", OriginID: a.ID, Status: "running", StartedAt: &now})
	st.Jobs().FailRunning(ctx, "restart", "restart", now)
	r := trigger.New(st, ex)
	r.Tick(ctx, now.Add(time.Minute))
	r.Wait()
	if len(ex.chats) != 0 {
		t.Fatalf("re-ran a restarted job: %v", ex.chats)
	}
}

// A task automation can go to one agent (its daily job) instead of the team.
func TestTaskAutomationForOneAgent(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	ex := &fakeExec{}
	r := trigger.New(st, ex)
	past := time.Now().UTC().Add(-time.Minute)
	st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "daily", Source: "schedule", Action: "task", AgentID: "agt_1", Enabled: true,
		Prompt: "báo cáo", Config: storage.AutomationConfig{EveryMinutes: 60}, NextRunAt: &past})
	r.Tick(ctx, time.Now().UTC())
	r.Wait()
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if len(ex.tasks) != 1 || ex.tasks[0] != "agt_1|báo cáo" {
		t.Fatalf("tasks = %v", ex.tasks)
	}
}
