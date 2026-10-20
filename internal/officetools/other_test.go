package officetools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// An office older than ADR-169 ignores the project a chat is asked as: a
// project's agent reads or sends into a chat it reaches only by its link's
// project once the other office says the chat is of it.
func TestOtherOldOffice(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(dir, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(dir, "k"))
	project := "" // what the other office says of the chat ("" = an old office)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": "nội dung blog", "project": project})
	}))
	defer srv.Close()
	key, _ := box.Seal("ofp_x")
	st.Settings().Set(ctx, "peers", []peer.Peer{{ID: "peer_b", Name: "office-b", URL: srv.URL, OutKeyEnc: key,
		Remote: []peer.RemoteProject{{ID: "rep_shop", Name: "shop"}, {ID: "rep_blog", Name: "blog"}}}})
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "mine"})
	perm.SavePolicy(ctx, st, p.ID, perm.Policy{LinkedProjects: []string{"peer:peer_b/rep_shop"}})
	box2 := New(st, nil, nil)
	box2.SetPeers(peer.New(st, box))
	sc := Scope{ProjectID: p.ID, Level: perm.Operate}
	read := func(args string) (string, bool) { return box2.CallOther(ctx, sc, "read_chat", json.RawMessage(args)) }

	link := srv.URL + "/projects/rep_shop?tab=chat&c=cnv_blog"
	if out, isErr := read(`{"chat":"` + link + `"}`); !isErr || !strings.Contains(out, "không xác nhận") {
		t.Fatalf("an old office's chat read on the link's word: %v %s", isErr, out)
	}
	project = "rep_blog" // a new office: the chat is blog's
	if _, isErr := read(`{"chat":"` + link + `"}`); !isErr {
		t.Fatal("a blog chat read as shop's")
	}
	project = "rep_shop"
	if out, isErr := read(`{"chat":"` + link + `"}`); isErr || !strings.Contains(out, "nội dung blog") {
		t.Fatalf("shop's chat: %v %s", isErr, out)
	}
	// by office + project (name) + id, as the tool says; an id alone asks for the project
	if out, isErr := read(`{"office":"office-b","project":"shop","chat":"cnv_blog"}`); isErr {
		t.Fatalf("by id with project: %s", out)
	}
	if out, isErr := read(`{"office":"office-b","chat":"cnv_blog"}`); !isErr || !strings.Contains(out, "project") {
		t.Fatalf("by id alone: %v %s", isErr, out)
	}
	if _, isErr := read(`{"office":"office-b","project":"blog","chat":"cnv_blog"}`); !isErr {
		t.Fatal("an unlinked project's chat by id")
	}
}
