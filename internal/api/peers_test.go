package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// Two offices pair through the dashboard API (ADR-167): B invites sharing
// one project, A joins with the code; A then reaches only that project, and
// the peer endpoints refuse a call without its key.
func TestPeerPairing(t *testing.T) {
	ctx := context.Background()
	a, b := setup(t), setup(t)
	shop, _ := b.st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	private, _ := b.st.Repos().Create(ctx, storage.Repo{Name: "private", Path: t.TempDir()})
	team.NewService(b.st, nil).AddFromLibrary(ctx, shop.ID, team.StarterAgent)
	cb, ca := b.client(t), a.client(t)
	login(t, b, cb, "admin@x.io", "admin-password")
	login(t, a, ca, "admin@x.io", "admin-password")

	// members do not pair
	cm := b.client(t)
	login(t, b, cm, "member@x.io", "member-password")
	if resp, _ := do(t, cm, "POST", b.srv.URL+"/api/peers/invite", map[string]any{"name": "b", "url": b.srv.URL}, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member invite = %d", resp.StatusCode)
	}

	resp, out := do(t, cb, "POST", b.srv.URL+"/api/peers/invite", map[string]any{"name": "office-b", "url": b.srv.URL, "shared": []string{shop.ID}}, nil)
	code, _ := out["code"].(string)
	if resp.StatusCode != http.StatusCreated || !strings.HasPrefix(code, "ofi_") {
		t.Fatalf("invite = %d %v", resp.StatusCode, out)
	}
	resp, out = do(t, ca, "POST", a.srv.URL+"/api/peers/join", map[string]any{"code": code, "name": "office-a", "url": a.srv.URL}, nil)
	if resp.StatusCode != http.StatusCreated || out["name"] != "office-b" {
		t.Fatalf("join = %d %v", resp.StatusCode, out)
	}
	pa, _ := out["id"].(string)
	if _, list := do(t, cb, "GET", b.srv.URL+"/api/peers", nil, nil); len(list["peers"].([]any)) != 1 {
		t.Fatalf("B's peers = %v", list)
	}

	// a project of A links to B's shop: the policy takes the ref
	mine, _ := a.st.Repos().Create(ctx, storage.Repo{Name: "mine", Path: t.TempDir()})
	ref := peer.RefPrefix + pa + "/" + shop.ID
	if resp, out := do(t, ca, "GET", a.srv.URL+"/api/projects/"+mine.ID+"/policy", nil, nil); resp.StatusCode != 200 || !strings.Contains(toJSON(out["offices"]), ref) {
		t.Fatalf("policy offices = %v", out["offices"])
	}
	if resp, out := do(t, ca, "PUT", a.srv.URL+"/api/projects/"+mine.ID+"/policy", map[string]any{"linked_projects": []string{ref}}, nil); resp.StatusCode != 200 {
		t.Fatalf("link = %d %v", resp.StatusCode, out)
	}
	if resp, _ := do(t, ca, "PUT", a.srv.URL+"/api/projects/"+mine.ID+"/policy", map[string]any{"linked_projects": []string{peer.RefPrefix + "peer_nope/x"}}, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an unknown office was linked: %d", resp.StatusCode)
	}

	// A sends: a new chat in the shared project, none in the other
	from := peer.Origin{Chat: "cnv_a", Project: "mine", ProjectID: mine.ID, Hops: 1}
	r, err := a.peers.Send(ctx, pa, peer.Message{Project: shop.ID, Text: "làm trang shop", From: from})
	if err != nil || r.Chat == "" {
		t.Fatalf("send: %+v %v", r, err)
	}
	if c, err := b.st.Chat().GetConversation(ctx, r.Chat); err != nil || c.ProjectID != shop.ID {
		t.Fatalf("B's chat: %+v %v", c, err)
	}
	if _, err := a.peers.Send(ctx, pa, peer.Message{Project: private.ID, Text: "x", From: from}); err == nil {
		t.Fatal("a project not shared was reached")
	}

	// no key, a wrong one, a used invite
	if resp, _ := do(t, http.DefaultClient, "POST", b.srv.URL+"/api/peer/relay", map[string]any{"project": shop.ID, "text": "x"}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("relay without key = %d", resp.StatusCode)
	}
	if resp, _ := do(t, http.DefaultClient, "GET", b.srv.URL+"/api/peer/projects", nil, map[string]string{"Authorization": "Bearer ofp_0000"}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key = %d", resp.StatusCode)
	}
	if resp, _ := do(t, ca, "POST", a.srv.URL+"/api/peers/join", map[string]any{"code": code, "name": "office-a", "url": a.srv.URL}, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an invite used twice = %d", resp.StatusCode)
	}

	// B unpairs: A's key stops working
	bl, _ := b.peers.List(ctx)
	if resp, _ := do(t, cb, "DELETE", b.srv.URL+"/api/peers/"+bl[0].ID, nil, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d", resp.StatusCode)
	}
	if _, err := a.peers.Send(ctx, pa, peer.Message{Project: shop.ID, Text: "x", From: from}); err == nil {
		t.Fatal("an unpaired office still sends")
	}
}
