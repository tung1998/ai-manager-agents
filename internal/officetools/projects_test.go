package officetools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/transfer"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// ADR-170: the office assistant exports a project to a .zip and proposes
// new projects on a card; one that only answers, or a project's agent, cannot.
func TestProjectTools(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	office, _ := st.Repos().Create(ctx, storage.Repo{Name: "Office"})
	shop, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	conv, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: office.ID})
	acts := actions.New(st, nil)
	tr := transfer.New(st, nil, nil, workflow.Library{Dir: t.TempDir()})
	tr.SetCloneRoot(t.TempDir())
	acts.SetProjects(tr)
	box := New(st, nil, acts)
	box.SetOffice(func(ctx context.Context) string { return office.ID })
	box.SetProjects(tr, t.TempDir())
	sc := Scope{ProjectID: office.ID, ConversationID: conv.ID, Office: true, Agent: "Trợ lý office", Level: "operate"}

	out, isErr := box.Call(ctx, sc, "export_project", json.RawMessage(`{"project":"shop"}`))
	if isErr || !strings.Contains(out, ".zip") {
		t.Fatalf("export: %v %s", isErr, out)
	}
	out, isErr = box.Call(ctx, sc, "propose_create_project", json.RawMessage(`{"name":"web help","reason":"dựng lại"}`))
	if isErr || !strings.Contains(out, "approval card") {
		t.Fatalf("create: %v %s", isErr, out)
	}
	answer := sc
	answer.AnswerOnly = true
	for _, tl := range box.ToolsFor(answer) {
		if strings.Contains(tl.Name, "project") && tl.Name != "projects" {
			t.Fatalf("answer only offers %s", tl.Name)
		}
	}
	if out, isErr := box.Call(ctx, answer, "export_project", json.RawMessage(`{"project":"shop"}`)); !isErr || !strings.Contains(out, "Chỉ trả lời") {
		t.Fatalf("export while answering only: %v %s", isErr, out)
	}
	if _, err := acts.Propose(ctx, actions.Scope{ProjectID: shop.ID, Agent: "dev"}, "create_project", "", "x", storage.ActionArgs{NewProject: &storage.NewProject{Name: "x"}}); err == nil {
		t.Fatal("a project's agent proposed a project")
	}
}
