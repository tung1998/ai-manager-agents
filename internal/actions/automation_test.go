package actions

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func TestProposeAutomation(t *testing.T) { // ADR-041
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	svc := New(st, nil)
	sc := Scope{ProjectID: proj.ID, RunRef: "r1", Agent: "Lead", Level: perm.Operate, Access: perm.Access{Level: perm.Operate, Caps: perm.Preset(perm.Operate)}}
	spec := func(v map[string]any) storage.ActionArgs {
		raw, _ := json.Marshal(v)
		return storage.ActionArgs{Automation: raw}
	}
	good := map[string]any{"name": "Kiểm tra log", "source": "schedule", "cron": "0 8 * * 1-5", "timezone": "Asia/Ho_Chi_Minh", "action": "script",
		"script": map[string]any{"lang": "bash", "body": "grep -c ERROR app.log || true"}, "escalate": map[string]any{"when": "signal", "action": "chat"}}
	bad := map[string]any{"name": "x", "source": "schedule", "cron": "99 * * * *", "action": "script", "script": map[string]any{"lang": "bash", "body": "echo"}}
	if _, err := svc.Propose(ctx, sc, "create_automation", "x", "vì sao", spec(bad)); err == nil {
		t.Fatal("bad cron accepted")
	}
	if _, err := svc.Propose(ctx, sc, "create_automation", "x", "", spec(map[string]any{"name": "x", "source": "schedule", "every_minutes": 5, "action": "script",
		"script": map[string]any{"lang": "ruby", "body": "puts 1"}})); err == nil {
		t.Fatal("unknown language accepted")
	}
	a, err := svc.Propose(ctx, sc, "create_automation", "Kiểm tra log", "báo lỗi mỗi sáng", spec(good))
	if err != nil || a.Status != "pending" {
		t.Fatalf("propose = %+v %v (never automatic, even at operate)", a, err)
	}
	done, err := svc.Decide(ctx, a.ID, true, "human:a@b.c")
	if err != nil || done.Status != "done" {
		t.Fatalf("decide = %+v %v", done, err)
	}
	list, _ := st.Automations().List(ctx, proj.ID)
	if len(list) != 1 || !list[0].Enabled || list[0].NextRunAt == nil || list[0].Script.Lang != "bash" || list[0].Escalate.When != "signal" || list[0].CreatedBy != "human:a@b.c" {
		t.Fatalf("automations = %+v", list)
	}
	// update: the script changes, the rest stays
	good["automation_id"] = list[0].ID
	good["script"] = map[string]any{"lang": "bash", "body": "echo v2"}
	u, err := svc.Propose(ctx, sc, "update_automation", list[0].ID, "sửa", spec(good))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(ctx, u.ID, true, "human:a@b.c"); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Automations().Get(ctx, list[0].ID); b.Script.Body != "echo v2" || b.ID != list[0].ID {
		t.Fatalf("updated = %+v", b)
	}
}
