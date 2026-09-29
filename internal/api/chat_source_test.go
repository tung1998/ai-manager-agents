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

// A chat that helps write a skill (its answers fill the editor): its own
// purpose, out of the project's chat list.
func TestSkillChatPurpose(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "GET", e.srv.URL+"/api/templates", nil, nil)
	solo := ""
	for _, x := range body["templates"].([]any) {
		if m := x.(map[string]any); m["key"] == "solo" {
			solo = m["id"].(string)
		}
	}
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop", "template_id": solo}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	resp, b := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/conversations", map[string]any{"purpose": "skill"}, nil)
	if resp.StatusCode != 201 || b["conversation"].(map[string]any)["purpose"] != "skill" {
		t.Fatalf("create = %d %v", resp.StatusCode, b)
	}
	_, b = do(t, admin, "GET", e.srv.URL+"/api/projects/"+pid+"/conversations?source=all", nil, nil)
	if len(b["conversations"].([]any)) != 0 {
		t.Fatalf("a skill chat is in the list: %v", b)
	}
}

// The Jobs page filters by where a job came from and finds them by title.
func TestJobsBySourceAndTitle(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	for _, j := range []storage.Job{
		{ProjectID: pid, Kind: "chat_turn", Origin: "user", Trigger: "ui", Title: "hỏi về đơn", Status: "done"},
		{ProjectID: pid, Kind: "chat_turn", Origin: "automation", Trigger: "discord", Title: "đơn 123 đâu", Status: "done"},
		{ProjectID: pid, Kind: "script", Origin: "automation", Trigger: "schedule", Title: "báo cáo sáng", Status: "done"},
	} {
		e.st.Jobs().Create(ctx, j)
	}
	titles := func(q string) []string {
		_, b := do(t, admin, "GET", e.srv.URL+"/api/jobs?"+q, nil, nil)
		var out []string
		for _, x := range b["jobs"].([]any) {
			out = append(out, x.(map[string]any)["title"].(string))
		}
		return out
	}
	for q, want := range map[string]string{"source=discord": "đơn 123 đâu", "source=web": "hỏi về đơn", "source=auto": "báo cáo sáng", "q=b%C3%A1o": "báo cáo sáng"} {
		if got := titles(q); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want [%s]", q, got, want)
		}
	}
}
