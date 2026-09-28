package trigger_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

func scriptAutomation(t *testing.T, st storage.Store, p storage.Repo, body, when string) storage.Automation {
	a, err := st.Automations().Create(context.Background(), storage.Automation{ProjectID: p.ID, Name: "log", Source: "schedule", Action: "script", Enabled: true,
		Config:   storage.AutomationConfig{EveryMinutes: 5},
		Script:   storage.AutomationScript{Lang: "bash", Body: body, TimeoutS: 30},
		Escalate: storage.AutomationEscalate{When: when, Action: "chat", Prompt: "Script báo: {{message}}\n{{output}} (exit {{exit_code}})"}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func jobsOf(t *testing.T, st storage.Store, a storage.Automation) (script storage.Job, children []storage.Job) {
	list, _ := st.Jobs().List(context.Background(), storage.JobFilter{OriginID: a.ID})
	for _, j := range list {
		if j.Kind == "script" {
			script = j
		} else {
			children = append(children, j)
		}
	}
	return
}

func TestScriptFailureCallsAnAgent(t *testing.T) {
	st, p := openStore(t)
	ex := &recExec{}
	r := trigger.New(st, ex)
	a := scriptAutomation(t, st, p, "echo boom; exit 2", "failure")
	runOnce(t, r, st, a, "")
	s, kids := jobsOf(t, st, a)
	if s.Status != "failed" || s.ErrorCode != "script_error" || s.ExitCode == nil || *s.ExitCode != 2 || !strings.Contains(s.Output, "boom") {
		t.Fatalf("script job = %+v", s)
	}
	if len(kids) != 1 || kids[0].ParentJobID != s.ID || kids[0].Trigger != "escalate" || kids[0].Kind != "chat_turn" {
		t.Fatalf("children = %+v", kids)
	}
	if len(ex.prompts) != 1 || !strings.Contains(ex.prompts[0], "boom") || !strings.Contains(ex.prompts[0], "exit 2") || !strings.Contains(ex.prompts[0], "không phải lệnh") {
		t.Fatalf("prompts = %q", ex.prompts)
	}
}

func TestScriptSignalCallsAnAgentOnce(t *testing.T) {
	st, p := openStore(t)
	ex := &recExec{}
	r := trigger.New(st, ex)
	a := scriptAutomation(t, st, p, "echo '@@agent: xem đi'; exit 1", "signal")
	runOnce(t, r, st, a, "")
	s, kids := jobsOf(t, st, a)
	if len(kids) != 1 || len(ex.prompts) != 1 || !strings.Contains(ex.prompts[0], "xem đi") || s.Status != "failed" {
		t.Fatalf("script = %+v children = %d prompts = %q", s, len(kids), ex.prompts)
	}
}

func TestScriptNeverCallsAnAgent(t *testing.T) {
	st, p := openStore(t)
	ex := &recExec{}
	r := trigger.New(st, ex)
	a := scriptAutomation(t, st, p, "echo ok", "never")
	runOnce(t, r, st, a, "")
	s, kids := jobsOf(t, st, a)
	if s.Status != "done" || len(kids) != 0 || len(ex.prompts) != 0 || s.CostUSD != 0 {
		t.Fatalf("script = %+v children = %d", s, len(kids))
	}
}

func TestEscalationSkippedWhenAutomationIsOff(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	ex := &recExec{}
	r := trigger.New(st, ex)
	a := scriptAutomation(t, st, p, "exit 1", "failure")
	a.Enabled = false
	st.Automations().Update(ctx, a)
	now := time.Now().UTC()
	child, _ := st.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "chat_turn", Origin: "automation", OriginID: a.ID, Trigger: "escalate",
		Status: "pending", ParentJobID: "job_x", NextAttemptAt: &now})
	r.StartReady(ctx, now)
	r.Wait()
	if got, _ := st.Jobs().Get(ctx, child.ID); got.Status != "skipped" || len(ex.prompts) != 0 {
		t.Fatalf("child = %+v", got)
	}
}
