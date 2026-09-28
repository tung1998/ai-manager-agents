package api_test

import (
	"context"
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
