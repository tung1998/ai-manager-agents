package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func TestAuditRecordsBeforeAfter(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "Hook", "source": "webhook", "action": "chat", "prompt": "x"}, nil)
	aid := body["automation"].(map[string]any)["id"].(string)
	resp, _ := do(t, admin, "PATCH", e.srv.URL+"/api/automations/"+aid, map[string]any{
		"name": "Hook 2", "source": "webhook", "action": "chat", "prompt": "x", "enabled": true}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("update = %d", resp.StatusCode)
	}
	rows, err := e.st.Audit().List(context.Background(), storage.AuditFilter{Resource: "automation", ResourceID: aid})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %d %v", len(rows), err)
	}
	up := rows[0]
	if up.Action != "automation.update" || up.ActorKind != "human" || up.ActorName != "admin@x.io" || up.ActorID == "" || up.Via != "ui" || up.ProjectID != pid {
		t.Fatalf("update row: %+v", up)
	}
	if up.Before["name"] != "Hook" || up.After["name"] != "Hook 2" {
		t.Fatalf("before/after: %v → %v", up.Before, up.After)
	}
	if create := rows[1]; create.Before != nil || create.After["name"] != "Hook" || create.ProjectID != pid {
		t.Fatalf("create row: %+v", create)
	}
}

func TestAuditRedactsProviderKey(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	resp, body := do(t, admin, "POST", e.srv.URL+"/api/providers", map[string]any{"name": "p", "kind": "anthropic", "api_key": "sk-secret-1"}, nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create provider = %d %v", resp.StatusCode, body)
	}
	id := body["provider"].(map[string]any)["id"].(string)
	if resp, body := do(t, admin, "PATCH", e.srv.URL+"/api/providers/"+id, map[string]any{"name": "p2", "kind": "anthropic", "api_key": "sk-secret-2"}, nil); resp.StatusCode != 200 {
		t.Fatalf("update provider = %d %v", resp.StatusCode, body)
	}
	rows, _ := e.st.Audit().List(context.Background(), storage.AuditFilter{Resource: "provider", ResourceID: id})
	for _, r := range rows {
		for _, snap := range []map[string]any{r.Before, r.After, r.Detail} {
			for k, v := range snap {
				if s, ok := v.(string); ok && (s == "sk-secret-1" || s == "sk-secret-2") {
					t.Fatalf("%s leaked key in %q", r.Action, k)
				}
			}
		}
	}
	if len(rows) != 2 || rows[0].Before["name"] != "p" || rows[0].After["name"] != "p2" {
		t.Fatalf("provider rows: %+v", rows)
	}
}

// An agent's proposal that is approved but fails still leaves a row naming
// the agent, the approver and the job (ADR-043).
func TestAuditApprovedProposalThatFails(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	spec, _ := json.Marshal(map[string]any{"automation_id": "aut_gone", "name": "x", "source": "schedule", "every_minutes": 30, "action": "script",
		"script": map[string]any{"lang": "bash", "body": "echo"}})
	a, err := e.st.Actions().Create(ctx, storage.Action{ProjectID: pid, Kind: "update_automation", Target: "x", ProposedBy: "Lead", JobID: "job_9",
		Args: storage.ActionArgs{Automation: spec}})
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := do(t, admin, "POST", e.srv.URL+"/api/actions/"+a.ID+"/approve", map[string]any{}, nil); resp.StatusCode != 200 {
		t.Fatalf("approve = %d %v", resp.StatusCode, body)
	}
	rows, _ := e.st.Audit().List(ctx, storage.AuditFilter{JobID: "job_9"})
	var approve *storage.AuditEntry
	for i := range rows {
		if rows[i].Action == "action.approve" {
			approve = &rows[i]
		}
	}
	if approve == nil || approve.ActorKind != "agent" || approve.ActorName != "Lead" || approve.ApprovedBy != "admin@x.io" ||
		approve.ActionID != a.ID || approve.ProjectID != pid || approve.OK {
		t.Fatalf("rows: %+v", rows)
	}
}
