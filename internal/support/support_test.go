package support

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/channels"
	"bitbucket.org/senprints/agent-office/internal/features"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// ADR-171: off by default; while off the bot tap takes nothing, the routes
// answer 404 and no tool is listed.
func TestOffChangesNothing(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop"})
	s := New(st)
	pass := func(h http.HandlerFunc) http.Handler { return h }
	mux := http.NewServeMux()
	s.Routes(mux, pass, pass)
	get := func() int {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/projects/"+p.ID+"/support", nil))
		return w.Code
	}

	if s.On(ctx, p.ID) || get() != http.StatusNotFound || s.Tools(officetools.Scope{ProjectID: p.ID}) != nil ||
		s.Take(ctx, storage.Channel{ProjectID: p.ID}, channels.Incoming{Text: "hi"}) {
		t.Fatal("off by default: nothing taken, 404, no tools")
	}
	features.Save(ctx, st, p.ID, features.Config{Packs: map[string]bool{"support": true}})
	if list, _ := st.Collections().List(ctx, p.ID); len(list) != 0 {
		t.Fatal("off: no collection made")
	}
	if !s.On(ctx, p.ID) || get() != http.StatusOK {
		t.Fatal("turned on: the routes answer")
	}
	if list, _ := st.Collections().List(ctx, p.ID); len(list) != 2 || list[0].Key != "customers" || list[0].Pack != "support" {
		t.Fatalf("on: the pack's collections, got %+v", list)
	}
}
