package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/api"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A running chat_turn job whose Turn is no longer in memory (already
// finished, or lost on a restart) must still end up cancelled in storage,
// not reported as cancelled while staying "running" forever.
func TestCancelJobRunningWithoutTurn(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	c, _ := e.st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: pid, Title: "x"})
	now := time.Now().UTC()
	j, err := e.st.Jobs().Create(ctx, storage.Job{ProjectID: pid, Kind: "chat_turn", Origin: "user", ConversationID: c.ID, Title: "x", Status: "running", StartedAt: &now})
	if err != nil {
		t.Fatal(err)
	}

	httpResp, resp := do(t, admin, "POST", e.srv.URL+"/api/jobs/"+j.ID+"/cancel", nil, nil)
	if httpResp.StatusCode != 200 {
		t.Fatalf("status = %d, body = %v", httpResp.StatusCode, resp)
	}
	got := resp["job"].(map[string]any)
	if got["status"] != "cancelled" {
		t.Fatalf("response job status = %v, want cancelled", got["status"])
	}
	saved, err := e.st.Jobs().Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "cancelled" {
		t.Fatalf("stored job status = %q, want cancelled (previously stayed \"running\" while the API reported success)", saved.Status)
	}
}

// Cancelling a job that already finished must not look like a success.
func TestCancelJobAlreadyDone(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	c, _ := e.st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: pid, Title: "x"})
	now := time.Now().UTC()
	j, _ := e.st.Jobs().Create(ctx, storage.Job{ProjectID: pid, Kind: "chat_turn", Origin: "user", ConversationID: c.ID, Title: "x", Status: "running", StartedAt: &now})
	if _, err := e.st.Jobs().Finish(ctx, j.ID, "done", "", "", now); err != nil {
		t.Fatal(err)
	}

	httpResp, _ := do(t, admin, "POST", e.srv.URL+"/api/jobs/"+j.ID+"/cancel", nil, nil)
	if httpResp.StatusCode != 409 {
		t.Fatalf("status = %d, want 409", httpResp.StatusCode)
	}
	saved, err := e.st.Jobs().Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "done" {
		t.Fatalf("stored job status = %q, want untouched done", saved.Status)
	}
}

// FinishIfRunning is the CAS primitive cancelJob relies on: it must refuse
// (ErrConflict) once the job has moved past "running", so a cancel racing
// the job's own completion cannot overwrite the real outcome.
func TestFinishIfRunningConflict(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	now := time.Now().UTC()
	j, _ := e.st.Jobs().Create(ctx, storage.Job{ProjectID: pid, Kind: "chat_turn", Origin: "user", Title: "x", Status: "running", StartedAt: &now})
	if _, err := e.st.Jobs().Finish(ctx, j.ID, "done", "", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Jobs().FinishIfRunning(ctx, j.ID, "cancelled", "cancelled", "", now); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	saved, err := e.st.Jobs().Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "done" {
		t.Fatalf("stored job status = %q, want untouched done", saved.Status)
	}
}

// FinishIfPending is the CAS primitive cancelJob's pending branch relies on:
// once Claim has moved a job to "running", FinishIfPending must refuse
// (ErrConflict) instead of overwriting it with "cancelled" while the run
// keeps executing for real.
func TestFinishIfPendingConflict(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	j, _ := e.st.Jobs().Create(ctx, storage.Job{ProjectID: pid, Kind: "chat_turn", Origin: "user", Title: "x", Status: "pending"})

	if _, err := e.st.Jobs().Claim(ctx, time.Now().UTC(), 10, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Jobs().FinishIfPending(ctx, j.ID, "cancelled", "cancelled", "", time.Now().UTC()); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	saved, err := e.st.Jobs().Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "running" {
		t.Fatalf("stored job status = %q, want untouched running", saved.Status)
	}

	j2, _ := e.st.Jobs().Create(ctx, storage.Job{ProjectID: pid, Kind: "chat_turn", Origin: "user", Title: "y", Status: "pending"})
	if _, err := e.st.Jobs().FinishIfPending(ctx, j2.ID, "cancelled", "cancelled", "", time.Now().UTC()); err != nil {
		t.Fatalf("FinishIfPending on a still-pending job: %v", err)
	}
}

// cancelJob on a job claimed between Get and the cancel write (StartReady
// already flipped it to "running", but execute() hasn't registered a Turn
// yet — that registration only happens once RunChat actually starts) must
// refuse with 409, not mark it "cancelled": at this point the run is about
// to really execute, so reporting "cancelled" here would lie to the caller
// while the job keeps running and burning tokens for real (ADR: the job
// must never show "cancelled" while an actual run completes over it).
func TestCancelJobPendingClaimedConcurrently(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	c, _ := e.st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: pid, Title: "x"})
	j, _ := e.st.Jobs().Create(ctx, storage.Job{ProjectID: pid, Kind: "chat_turn", Origin: "user", ConversationID: c.ID, Title: "x", Status: "pending"})

	// Force the exact race window: cancelJob has just done its Get (seeing
	// "pending"), and now Claim runs before cancelJob's own write.
	defer api.SetCancelJobRaceHook(func() {
		if _, err := e.st.Jobs().Claim(ctx, time.Now().UTC(), 10, nil); err != nil {
			t.Fatal(err)
		}
	})()

	httpResp, resp := do(t, admin, "POST", e.srv.URL+"/api/jobs/"+j.ID+"/cancel", nil, nil)
	if httpResp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, body = %v, want 409 (no turn registered yet does not mean the run is dead right after Claim)", httpResp.StatusCode, resp)
	}
	saved, err := e.st.Jobs().Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "running" {
		t.Fatalf("stored job status = %q, want untouched running (must not be marked cancelled while the claimed run goes on to execute)", saved.Status)
	}
}
