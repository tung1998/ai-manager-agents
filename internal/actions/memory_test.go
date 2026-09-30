package actions

import (
	"context"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/memory"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// An agent's "remember" waits for a person, unless the project keeps its
// agents' notes on its own; approved, the note is the agent's.
func TestRemember(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, proj.ID, solo.ID, false)
	model, _ := st.OrgModels().GetForRepo(ctx, proj.ID)
	agents, _ := st.Agents().List(ctx, model.ID)
	mem := memory.New(st, nil)
	svc := New(st, nil)
	svc.SetMemory(mem)
	sc := Scope{ProjectID: proj.ID, RunRef: "r1", Agent: agents[0].Name, Level: perm.Propose}
	a, err := svc.Propose(ctx, sc, "remember", "project dùng pnpm", "thấy lockfile")
	if err != nil || a.Status != "pending" || a.TargetID != agents[0].ID {
		t.Fatalf("propose = %+v %v", a, err)
	}
	if _, err := svc.Decide(ctx, a.ID, true, "human:a"); err != nil {
		t.Fatal(err)
	}
	list, _ := st.Memories().List(ctx, proj.ID, agents[0].ID)
	if len(list) != 1 || list[0].Text != "project dùng pnpm" || list[0].Source != "agent" {
		t.Fatalf("notes = %+v", list)
	}
	mem.SetAuto(ctx, proj.ID, true)
	sc.RunRef = "r2"
	if a, _ = svc.Propose(ctx, sc, "remember", "chạy pnpm test trước khi báo xong", ""); a.Status != "done" {
		t.Fatalf("auto = %+v", a)
	}
	if _, err := svc.Propose(ctx, Scope{ProjectID: proj.ID, Agent: "không có", Level: perm.Propose}, "remember", "x", ""); err == nil {
		t.Fatal("a note of an agent that is not in the project")
	}
}
