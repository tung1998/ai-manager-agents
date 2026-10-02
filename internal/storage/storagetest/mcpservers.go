package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func testMCPServers(t *testing.T, s storage.Store) {
	ctx := context.Background()
	r := s.MCPServers()
	a, err := r.Create(ctx, storage.MCPServer{Name: "context7", URL: "https://mcp.example/mcp", HeadersEnc: "enc", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Kind != "http" || a.Scope != "machine" || a.Origin != "manual" {
		t.Fatalf("created = %+v", a)
	}
	if _, err := r.Create(ctx, storage.MCPServer{Name: "context7"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate name err = %v", err)
	}
	b, _ := r.Create(ctx, storage.MCPServer{Name: "abc", URL: "https://b.example/mcp"})
	a.URL, a.Enabled = "https://mcp.example/v2", false
	if err := r.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	b.Name = "context7"
	if err := r.Update(ctx, b); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("rename onto a taken name err = %v", err)
	}
	ro := true
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := r.SetCheck(ctx, a.ID, "ok", "", []storage.MCPTool{{Name: "resolve", Description: "d", ReadOnly: &ro}}, now); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetByName(ctx, "context7")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://mcp.example/v2" || got.Enabled || got.HeadersEnc != "enc" || got.LastCheckStatus != "ok" ||
		got.LastCheckAt == nil || !got.LastCheckAt.Equal(now) || len(got.LastTools) != 1 || got.LastTools[0].ReadOnly == nil || !*got.LastTools[0].ReadOnly {
		t.Fatalf("after updates = %+v", got)
	}
	if err := r.SetOAuth(ctx, got.ID, "oauth-enc"); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(ctx, got); err != nil { // an edit keeps the login
		t.Fatal(err)
	}
	if got, _ = r.Get(ctx, got.ID); got.OAuthEnc != "oauth-enc" {
		t.Fatalf("oauth after update = %q", got.OAuthEnc)
	}
	list, _ := r.List(ctx)
	if len(list) != 2 || list[0].Name != "abc" {
		t.Fatalf("List by name = %+v", list)
	}
	if err := r.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, a.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get deleted err = %v", err)
	}
}
