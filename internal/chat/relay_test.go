package chat_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// send_to_chat: the message goes into the other chat as its person wrote it,
// marked as relayed (so what it starts does not relay on by itself).
func TestRelayToAnotherChat(t *testing.T) {
	bin, _ := fakeClaude(t, "B", true)
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, _ := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		return p
	})
	ctx := actor.With(context.Background(), "human:a@b.c")
	from, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	to, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	now := time.Now().UTC()
	job, _ := f.st.Jobs().Create(ctx, storage.Job{ProjectID: f.project.ID, Kind: "chat_turn", Origin: "user", CreatedBy: "human:a@b.c", ConversationID: from.ID, Status: "running", StartedAt: &now})
	a := storage.Action{ProjectID: f.project.ID, ConversationID: from.ID, JobID: job.ID, Kind: "send_message", TargetID: to.ID, ProposedBy: "Trợ lý",
		Args: storage.ActionArgs{Message: "Sửa giúp lỗi giỏ hàng"}}
	if _, err := f.engine.Relay(ctx, a); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(f.engine.Running(to.ID)) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	msgs, _ := f.st.Chat().ListMessages(ctx, to.ID)
	if len(msgs) < 1 || msgs[0].Role != "user" || msgs[0].Author != "human:a@b.c" || msgs[0].Content != "Sửa giúp lỗi giỏ hàng" ||
		!strings.Contains(msgs[0].Context, from.ID) {
		t.Fatalf("messages = %+v", msgs)
	}
	if !f.engine.Relayed(ctx, to.ID) || f.engine.Relayed(ctx, from.ID) {
		t.Fatal("relayed mark wrong")
	}

	// a bot's or an automation's run cannot speak for a person
	bot, _ := f.st.Jobs().Create(ctx, storage.Job{ProjectID: f.project.ID, Kind: "chat_turn", Origin: "automation", CreatedBy: "discord:1", ConversationID: from.ID, Status: "running", StartedAt: &now})
	a.JobID = bot.ID
	if _, err := f.engine.Relay(ctx, a); err == nil {
		t.Fatal("a bot's run relayed in a person's name")
	}
}

// send_to_chat with a linked project and no chat: a new chat opens there in
// the person's name, its agent answers, and where it went comes back.
func TestRelayNewChatInLinkedProject(t *testing.T) {
	bin, _ := fakeClaude(t, "B", true)
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, _ := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		return p
	})
	ctx := actor.With(context.Background(), "human:a@b.c")
	admin, _ := f.st.Repos().Create(ctx, storage.Repo{Name: "admin v3", Path: t.TempDir()})
	solo, _ := team.PackByKey("solo")
	team.NewService(f.st, nil).ApplyPack(ctx, admin.ID, solo, false)
	from, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	now := time.Now().UTC()
	job, _ := f.st.Jobs().Create(ctx, storage.Job{ProjectID: f.project.ID, Kind: "chat_turn", Origin: "user", CreatedBy: "human:a@b.c", ConversationID: from.ID, Status: "running", StartedAt: &now})
	a := storage.Action{ProjectID: f.project.ID, ConversationID: from.ID, JobID: job.ID, Kind: "send_message", ProposedBy: "Lead",
		Args: storage.ActionArgs{Message: "Sửa trang admin như đã bàn", Project: admin.ID}}
	detail, err := f.engine.Relay(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	convs, _ := f.st.Chat().ListConversations(ctx, admin.ID, 10)
	if len(convs) != 1 || convs[0].CreatedBy != "human:a@b.c" || !strings.Contains(detail, "/projects/"+admin.ID+"?tab=chat&c="+convs[0].ID) {
		t.Fatalf("detail %q, chats %+v", detail, convs)
	}
	to := convs[0].ID
	deadline := time.Now().Add(10 * time.Second)
	for len(f.engine.Running(to)) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	msgs, _ := f.st.Chat().ListMessages(ctx, to)
	if len(msgs) < 1 || msgs[0].Content != "Sửa trang admin như đã bàn" || !strings.Contains(msgs[0].Context, `"relayed_from_project":"demo"`) {
		t.Fatalf("messages = %+v", msgs)
	}
}
