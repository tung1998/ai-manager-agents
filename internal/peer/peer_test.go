package peer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

type office struct {
	st  storage.Store
	svc *Service
	srv *httptest.Server
	got []Message // what came in on /api/peer/relay
}

// newOffice serves the peer endpoints as the API does.
func newOffice(t *testing.T) *office {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(dir, "k"))
	o := &office{st: st, svc: New(st, box)}
	auth := func(r *http.Request) (Peer, bool) {
		return o.svc.Auth(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/peer/pair", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		out, err := o.svc.Accept(r.Context(), body)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusForbidden)
			return
		}
		w.Write(out)
	})
	mux.HandleFunc("POST /api/peer/relay", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth(r); !ok {
			http.Error(w, `{"error":"denied"}`, http.StatusUnauthorized)
			return
		}
		var m Message
		json.NewDecoder(r.Body).Decode(&m)
		o.got = append(o.got, m)
		json.NewEncoder(w).Encode(Reply{Chat: "cnv_there", Title: "t", Project: m.Project})
	})
	mux.HandleFunc("PUT /api/peer/projects", func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth(r)
		if !ok {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		var in struct{ Projects []RemoteProject }
		json.NewDecoder(r.Body).Decode(&in)
		o.svc.SetRemote(r.Context(), p.ID, in.Projects)
	})
	mux.HandleFunc("GET /api/peer/projects", func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth(r)
		if !ok {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"projects": o.svc.SharedProjects(r.Context(), p)})
	})
	o.srv = httptest.NewServer(mux)
	t.Cleanup(o.srv.Close)
	return o
}

// Two offices pair with a one-time invite; each then calls the other with
// the key it was given, and reaches only the projects shared with it.
func TestPairAndSend(t *testing.T) {
	ctx := context.Background()
	a, b := newOffice(t), newOffice(t)
	shop, _ := b.st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir(), Description: "the store"})
	b.st.Repos().Create(ctx, storage.Repo{Name: "private", Path: t.TempDir()})

	code, _, err := b.svc.Invite(ctx, "human:b@x", "office-b", b.srv.URL, []string{shop.ID})
	if err != nil || !strings.HasPrefix(code, "ofi_") {
		t.Fatalf("invite: %q %v", code, err)
	}
	pa, err := a.svc.Join(ctx, "human:a@x", code, "office-a", a.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if pa.Name != "office-b" || len(pa.Remote) != 1 || pa.Remote[0].Name != "shop" || pa.Remote[0].Description != "the store" {
		t.Fatalf("A sees B as %+v", pa)
	}
	if _, err := a.svc.Join(ctx, "human:a@x", code, "office-a", a.srv.URL); err == nil {
		t.Fatal("an invite works only once")
	}
	bl, _ := b.svc.List(ctx)
	if len(bl) != 1 || bl[0].Name != "office-a" || bl[0].As != "human:b@x" || bl[0].Shared[0] != shop.ID {
		t.Fatalf("B sees A as %+v", bl)
	}

	// A links its project to B's shop and sends there
	mine, _ := a.st.Repos().Create(ctx, storage.Repo{Name: "mine", Path: t.TempDir()})
	perm.SavePolicyIf(ctx, a.st, mine.ID, "", perm.Policy{LinkedProjects: []string{RefPrefix + pa.ID + "/" + shop.ID}})
	p, rp, err := Resolve(ctx, a.st, a.svc, mine.ID, "office-B/Shop")
	if err != nil || p.ID != pa.ID || rp.ID != shop.ID {
		t.Fatalf("resolve: %v %+v %+v", err, p, rp)
	}
	if _, _, err := Resolve(ctx, a.st, a.svc, mine.ID, "private"); err == nil {
		t.Fatal("a project not linked resolved")
	}
	r, err := a.svc.Send(ctx, pa.ID, Message{Project: shop.ID, Text: "hi", From: Origin{Chat: "c1", Hops: 1}})
	if err != nil || r.Chat != "cnv_there" || len(b.got) != 1 || b.got[0].Text != "hi" {
		t.Fatalf("send: %v %+v %+v", err, r, b.got)
	}
	// and B answers A with the key A gave it
	if _, err := b.svc.Send(ctx, bl[0].ID, Message{Chat: "c1", Text: "done", From: Origin{Chat: "cnv_there", Report: true}}); err != nil || len(a.got) != 1 {
		t.Fatalf("send back: %v %+v", err, a.got)
	}

	// B shares nothing more: A hears of it
	if err := b.svc.SetShared(ctx, bl[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := a.svc.Get(ctx, pa.ID)
		if len(got.Remote) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("A still sees %+v", got.Remote)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(a.svc.LinksOf(ctx, mine.ID)) != 0 {
		t.Fatal("a project no longer shared is still linked")
	}

	// unpaired: its key stops working
	b.svc.Remove(ctx, bl[0].ID)
	if _, err := a.svc.Send(ctx, pa.ID, Message{Project: shop.ID, Text: "x", From: Origin{Chat: "c1"}}); err == nil {
		t.Fatal("a removed office's key still works")
	}
}

func TestInviteExpires(t *testing.T) {
	ctx := context.Background()
	a, b := newOffice(t), newOffice(t)
	code, _, _ := b.svc.Invite(ctx, "human:b@x", "b", b.srv.URL, nil)
	b.svc.now = func() time.Time { return time.Now().Add(inviteTTL + time.Minute) }
	if _, err := a.svc.Join(ctx, "human:a@x", code, "a", a.srv.URL); err == nil {
		t.Fatal("an expired invite was accepted")
	}
	if _, err := a.svc.Join(ctx, "human:a@x", "ofi_garbage", "a", a.srv.URL); err == nil {
		t.Fatal("a bad code was accepted")
	}
}

func TestCleanURL(t *testing.T) {
	ok := map[string]string{
		"https://office.example.com/": "https://office.example.com",
		"https://office.example/sub/": "https://office.example/sub",
		"http://localhost:3000":       "http://localhost:3000",
		"http://127.0.0.1:8787":       "http://127.0.0.1:8787",
		"http://100.101.1.2:3000":     "http://100.101.1.2:3000",
		"http://box.tail1234.ts.net/": "http://box.tail1234.ts.net",
	}
	for in, want := range ok {
		if got, err := CleanURL(in); err != nil || got != want {
			t.Errorf("CleanURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"http://office.example.com", "http://192.168.1.2", "ftp://x", "https://u:p@x", "https://x/?a=1", "office.example"} {
		if _, err := CleanURL(in); err == nil {
			t.Errorf("CleanURL(%q) accepted", in)
		}
	}
}
