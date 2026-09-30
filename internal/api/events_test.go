package api_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A page open on the dashboard hears what changed, whoever changed it (here
// a message written straight to the store, as a bot or an agent does).
func TestEventsStream(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, "GET", e.srv.URL+"/api/events", nil)
	resp, err := admin.Do(req)
	if err != nil || resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events = %v %v", resp, err)
	}
	defer resp.Body.Close()
	go func() {
		time.Sleep(100 * time.Millisecond)
		e.st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: pid, Title: "từ Discord", CreatedBy: "discord:an", Purpose: "channel"})
	}()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data:") && strings.Contains(line, "conversations") {
			return
		}
	}
	t.Fatal("no change heard")
}
