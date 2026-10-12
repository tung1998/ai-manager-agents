package api_test

import (
	"context"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// TestBurnItemActionFirstResetsReviewErrAttempts: the "first" action must
// give a failed item a fresh review-error budget, otherwise one more
// transient review error re-fails it immediately (ADR-120).
func TestBurnItemActionFirstResetsReviewErrAttempts(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	parent := t.TempDir()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects/new", map[string]any{"idea": "shop", "stack": "go", "name": "shop", "parent": parent}, nil)
	p := body["project"].(map[string]any)
	projectID := p["id"].(string)
	ctx := context.Background()

	b, err := e.st.Burn().Session(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := e.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "flaky", Kind: "upgrade", Status: "failed", ReviewErrAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := do(t, admin, "POST", e.srv.URL+"/api/burn-items/"+failed.ID+"/first", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("first = %d %v", resp.StatusCode, body)
	}
	if it, err := e.st.Burn().Item(ctx, failed.ID); err != nil || it.Status != "queued" || it.ReviewErrAttempts != 0 {
		t.Fatalf("after first: %+v %v", it, err)
	}

	// A "first" on an item already queued mid-retry must not wipe its
	// in-progress attempt count, only bump its priority.
	queued, err := e.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "retrying", Kind: "upgrade", Status: "queued", ReviewErrAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := do(t, admin, "POST", e.srv.URL+"/api/burn-items/"+queued.ID+"/first", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("first = %d %v", resp.StatusCode, body)
	}
	if it, err := e.st.Burn().Item(ctx, queued.ID); err != nil || it.Status != "queued" || it.ReviewErrAttempts != 2 {
		t.Fatalf("already-queued item's attempts changed: %+v %v", it, err)
	}
}
