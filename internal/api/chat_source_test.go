package api_test

import (
	"context"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// The chat list says where each chat started (web, a bot, an automation) and
// filters by it: a Discord chat opened from a job is in the list too.
func TestChatListBySource(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	for _, c := range []storage.Conversation{
		{ProjectID: pid, Title: "web", CreatedBy: "human:admin@x.io"},
		{ProjectID: pid, Title: "discord", CreatedBy: "discord:binh", Purpose: "channel"},
		{ProjectID: pid, Title: "telegram", CreatedBy: "telegram:an", Purpose: "channel"},
		{ProjectID: pid, Title: "auto", CreatedBy: "auto:Báo cáo"},
	} {
		if _, err := e.st.Chat().CreateConversation(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(q string) map[string]string {
		_, b := do(t, admin, "GET", e.srv.URL+"/api/projects/"+pid+"/conversations"+q, nil, nil)
		out := map[string]string{}
		for _, x := range b["conversations"].([]any) {
			c := x.(map[string]any)
			out[c["title"].(string)] = c["source"].(string)
		}
		return out
	}
	if got := titles("?source=all"); len(got) != 4 || got["web"] != "web" || got["discord"] != "discord" || got["telegram"] != "telegram" || got["auto"] != "auto" {
		t.Fatalf("all = %v", got)
	}
	if got := titles("?source=discord"); len(got) != 1 || got["discord"] != "discord" {
		t.Fatalf("discord = %v", got)
	}
	if got := titles("?source=web"); len(got) != 1 || got["web"] != "web" {
		t.Fatalf("web = %v", got)
	}
	if got := titles(""); len(got) != 2 || got["web"] == "" || got["auto"] == "" { // as before: the project's own chats
		t.Fatalf("default = %v", got)
	}
}

// The task list filters by where a task came from, the same way.
func TestTaskListBySource(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	for _, by := range []string{"human:admin@x.io", "discord:binh", "auto:Báo cáo"} {
		if _, err := e.st.Tasks().Create(ctx, storage.Task{ProjectID: pid, Title: by, Goal: by, Status: "done", CreatedBy: by}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) (int, string) {
		_, b := do(t, admin, "GET", e.srv.URL+"/api/projects/"+pid+"/tasks"+q, nil, nil)
		list := b["tasks"].([]any)
		src := ""
		if len(list) > 0 {
			src, _ = list[0].(map[string]any)["source"].(string)
		}
		return len(list), src
	}
	if n, _ := count(""); n != 3 {
		t.Fatalf("all = %d", n)
	}
	if n, src := count("?source=discord"); n != 1 || src != "discord" {
		t.Fatalf("discord = %d %q", n, src)
	}
}
