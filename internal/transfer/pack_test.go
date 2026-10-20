package transfer_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/transfer"
)

// ADR-170: a project goes to another office as a pack (.zip): config, chats
// and notes as chosen, never a local path or a credential.
func TestPackRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := newOffice(t)
	repo, _ := src.st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop", GitRemote: "https://bot:ghp_x@git.example/acme/shop.git", Description: "Shop"})
	if err := src.org.AddFromLibrary(ctx, repo.ID, "office/team-lead", "office/backend-dev"); err != nil {
		t.Fatal(err)
	}
	agents, _ := src.st.Agents().List(ctx, repo.ID)
	lead := agents[0]
	c, _ := src.st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: repo.ID, AgentID: lead.ID, AgentName: lead.Name, Title: "làm trang shop", CreatedBy: "a@x"})
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	src.st.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "user", Author: "a@x", Content: "làm trang shop", CreatedAt: at})
	src.st.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "assistant", AgentID: lead.ID, Content: "xong", CreatedAt: at.Add(time.Minute)})
	src.st.Memories().Create(ctx, storage.Memory{ProjectID: repo.ID, AgentID: lead.ID, Text: "dùng pnpm", Source: "agent", Topic: "build", Summary: "lệnh build"})

	only, err := src.tr.ExportPack(ctx, repo.ID, nil, nil)
	if err != nil || len(only.Chats) != 0 || len(only.Memories) != 0 || only.Project.Agents == nil {
		t.Fatalf("default export is config only: %+v %v", only, err)
	}
	pack, err := src.tr.ExportPack(ctx, repo.ID, transfer.Includes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Project.Path != "" || pack.Project.GitRemote != "https://git.example/acme/shop.git" || len(pack.Chats) != 1 || len(pack.Memories) != 1 {
		t.Fatalf("pack: path %q remote %q chats %d notes %d", pack.Project.Path, pack.Project.GitRemote, len(pack.Chats), len(pack.Memories))
	}
	var buf bytes.Buffer
	if err := transfer.WriteZip(&buf, pack); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "ghp_x") {
		t.Fatal("the pack carries a credential")
	}
	file := filepath.Join(t.TempDir(), "shop.zip")
	os.WriteFile(file, buf.Bytes(), 0o600)
	if _, err := transfer.ReadZip([]byte("not a zip")); err == nil {
		t.Fatal("read a file that is no pack")
	}

	dst := newOffice(t)
	folder := t.TempDir()
	np := storage.NewProject{Path: folder, Pack: file, Include: []string{"config", "chats", "memory"}}
	if err := dst.tr.CheckNewProject(ctx, &np); err != nil || np.Name != "shop" || np.URL != "" {
		t.Fatalf("check: %+v %v", np, err)
	}
	got, err := dst.tr.CreateProject(ctx, np)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "shop" || got.Description != "Shop" {
		t.Fatalf("project: %+v", got)
	}
	gotAgents, _ := dst.st.Agents().List(ctx, got.ID)
	if len(gotAgents) != 2 {
		t.Fatalf("agents: %d", len(gotAgents))
	}
	convs, _ := dst.st.Chat().ListConversationsFrom(ctx, got.ID, "all", 10)
	if len(convs) != 1 || convs[0].Title != "làm trang shop" {
		t.Fatalf("chats: %+v", convs)
	}
	msgs, _ := dst.st.Chat().ListMessages(ctx, convs[0].ID)
	if len(msgs) != 2 || !msgs[0].CreatedAt.Equal(at) || msgs[1].AgentID == "" {
		t.Fatalf("messages: %+v", msgs)
	}
	var leadHere storage.Agent
	for _, a := range gotAgents {
		if a.Key == lead.Key {
			leadHere = a
		}
	}
	notes, _ := dst.st.Memories().List(ctx, got.ID, leadHere.ID)
	if len(notes) != 1 || notes[0].Topic != "build" {
		t.Fatalf("notes: %+v", notes)
	}
	// again: a note already there is not added twice
	if _, err := dst.tr.ImportPack(ctx, pack, got, []string{"memory"}); err != nil {
		t.Fatal(err)
	}
	if notes, _ := dst.st.Memories().List(ctx, got.ID, leadHere.ID); len(notes) != 1 {
		t.Fatalf("notes after a second import: %d", len(notes))
	}
	// the same folder is not a second project
	if err := dst.tr.CheckNewProject(ctx, &storage.NewProject{Path: folder, Name: "x"}); err == nil {
		t.Fatal("a folder became two projects")
	}
}

// A new project with no url or path is an empty git repo under the clone root.
func TestCreateEmptyProject(t *testing.T) {
	ctx := context.Background()
	o := newOffice(t)
	root := t.TempDir()
	o.tr.SetCloneRoot(root)
	r, err := o.tr.CreateProject(ctx, storage.NewProject{Name: "Web Help", Description: "extension"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != filepath.Join(root, "web-help") && !strings.HasSuffix(r.Path, "/web-help") {
		t.Fatalf("path %q", r.Path)
	}
	if _, err := os.Stat(filepath.Join(r.Path, ".git")); err != nil {
		t.Fatal("not a git repo")
	}
	if agents, _ := o.st.Agents().List(ctx, r.ID); len(agents) == 0 {
		t.Fatal("a new project has no agent")
	}
}
