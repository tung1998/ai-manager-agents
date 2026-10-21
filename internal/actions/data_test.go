package actions

import (
	"context"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// ADR-172: an agent deleting a record proposes it, even at the top level; a
// person approving deletes it (its history keeps it).
func TestDataDelete(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "p"})
	c, _ := st.Collections().Create(ctx, storage.Collection{ProjectID: proj.ID, Key: "customers", Name: "Khách", Fields: []storage.Field{{Name: "name", Type: "text"}}})
	x, _ := st.Collections().Insert(ctx, storage.Record{CollectionID: c.ID, ProjectID: proj.ID, Data: map[string]any{"name": "An"}})
	svc := New(st, nil)
	sc := Scope{ProjectID: proj.ID, ConversationID: "", RunRef: "r1", Agent: "CSKH", Level: perm.Operate, Access: perm.Access{Level: perm.Operate, Caps: perm.Preset(perm.Operate)}}
	if _, err := svc.Propose(ctx, sc, "data_delete", "", "", storage.ActionArgs{Record: &storage.RecordRef{Collection: "customers", ID: "rec_nope"}}); err == nil {
		t.Fatal("a record that is not there")
	}
	a, err := svc.Propose(ctx, sc, "data_delete", "", "trùng", storage.ActionArgs{Record: &storage.RecordRef{Collection: "customers", ID: x.ID}})
	if err != nil || a.Status != "pending" || a.Target != "Khách: An" {
		t.Fatalf("proposed = %+v %v", a, err)
	}
	if a, err = svc.Decide(ctx, a.ID, true, "admin@x"); err != nil || a.Status != "done" {
		t.Fatalf("approved = %+v %v", a, err)
	}
	if _, err := st.Collections().Record(ctx, c.ID, x.ID); err == nil {
		t.Fatal("still there")
	}
	if h, _ := st.Collections().History(ctx, x.ID, 5); len(h) != 2 || h[0].Op != "delete" || h[0].By != "agent:CSKH" {
		t.Fatalf("history = %+v", h)
	}
}
