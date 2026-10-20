package chat_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

type fakePeers struct {
	p    peer.Peer
	mu   sync.Mutex
	sent []peer.Message
}

func (f *fakePeers) Get(ctx context.Context, id string) (peer.Peer, error) {
	if id != f.p.ID {
		return peer.Peer{}, storage.ErrNotFound
	}
	return f.p, nil
}

func (f *fakePeers) Send(ctx context.Context, id string, m peer.Message) (peer.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return peer.Reply{Chat: "cnv_there"}, nil
}

func (f *fakePeers) LinksOf(ctx context.Context, projectID string) []peer.Link { return nil }

// Another office hands work to a shared project (ADR-167): a chat opens in
// the pairing person's name, its agent answers, and the answer goes back to
// the office's chat it came from. What is not shared stays out of reach.
func TestReceiveFromPeer(t *testing.T) {
	var last struct {
		sync.Mutex
		system, prompt string
	}
	f := setup(t, capturing(t, &last))
	ctx := context.Background()
	fp := &fakePeers{p: peer.Peer{ID: "peer_a", Name: "office-a", URL: "https://a.example", SelfURL: "https://b.example",
		Shared: []string{f.project.ID}, As: "human:b@x"}}
	f.engine.SetPeers(fp)
	if _, err := f.st.Users().Create(ctx, storage.User{Email: "b@x", Role: storage.RoleMember}); err != nil {
		t.Fatal(err)
	}
	gone := fp.p
	gone.As = "human:gone@x"
	if _, err := f.engine.Receive(ctx, gone, peer.Message{Project: f.project.ID, Text: "x", From: peer.Origin{Chat: "cnv_a", Hops: 1}}); err == nil {
		t.Fatal("a pairing whose person has no account here was taken")
	}
	if _, err := f.engine.Receive(ctx, fp.p, peer.Message{Project: f.project.ID, Text: "x", From: peer.Origin{Chat: "cnv_a", Hops: -100}}); err == nil {
		t.Fatal("negative hops went past the cap")
	}
	from := peer.Origin{Chat: "cnv_a", Title: "mockup", Agent: "Lead", Project: "mockup", ProjectID: "rep_a", Hops: 1}

	if _, err := f.engine.Receive(ctx, fp.p, peer.Message{Project: "rep_other", Text: "x", From: from}); err == nil {
		t.Fatal("a project not shared was opened")
	}
	if _, err := f.engine.Receive(ctx, fp.p, peer.Message{Project: f.project.ID, Text: "x", From: peer.Origin{Chat: "cnv_a", Report: true}}); err == nil {
		t.Fatal("a report opened a new chat")
	}
	stranger, _ := f.st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: f.project.ID, Title: "riêng"})
	if _, err := f.engine.Receive(ctx, fp.p, peer.Message{Chat: stranger.ID, Text: "x", From: from}); err == nil {
		t.Fatal("a chat not between the two offices was reached")
	}

	r, err := f.engine.Receive(ctx, fp.p, peer.Message{Project: f.project.ID, Text: "làm trang shop", From: from})
	if err != nil || r.Chat == "" {
		t.Fatalf("receive: %+v %v", r, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		fp.mu.Lock()
		n := len(fp.sent)
		fp.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no report back to the office")
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := fp.sent[0]
	if got.Chat != "cnv_a" || !got.From.Report || got.From.Hops != 2 || got.From.Chat != r.Chat || !strings.Contains(got.Text, "https://b.example/projects/") {
		t.Fatalf("report = %+v", got)
	}
	msgs, _ := f.st.Chat().ListMessages(ctx, r.Chat)
	if len(msgs) == 0 || msgs[0].Author != "human:b@x" || !strings.Contains(msgs[0].Context, `"relayed_from_peer":"peer_a"`) {
		t.Fatalf("message = %+v", msgs)
	}
	if rl := chat.RelayOf(msgs[0]); rl == nil || rl.URL != "https://a.example/projects/rep_a?tab=chat&c=cnv_a" || rl.ProjectID != "" || rl.Project != "office-a/mockup" {
		t.Fatalf("relay = %+v", rl)
	}
	c, _ := f.st.Chat().GetConversation(ctx, r.Chat)
	if !f.engine.PeerChat(ctx, c, fp.p) || f.engine.PeerChat(ctx, stranger, fp.p) {
		t.Fatal("the office reads the chat it opened, not another")
	}
	// it goes on in that chat: it is now between the two offices
	if _, err := f.engine.Receive(ctx, fp.p, peer.Message{Chat: r.Chat, Text: "thêm nút", From: from}); err != nil {
		t.Fatalf("follow-up: %v", err)
	}
}
