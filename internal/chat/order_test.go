package chat_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/worktree"
)

// A chat's proposals are approved in the order its agent made them: a later
// one waits for the earlier ones; rejecting never waits.
func TestCheckOrder(t *testing.T) {
	g := newGroup(t)
	ctx := g.context
	first, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, Kind: "run_command", Target: "git add -A"})
	time.Sleep(2 * time.Millisecond)
	patch, _ := g.f.st.Chat().AddPatch(ctx, storage.Patch{ConversationID: g.conv.ID, Diff: "x", Files: []string{"a.go", "b.go"}})
	time.Sleep(2 * time.Millisecond)
	last, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, Kind: "run_command", Target: "git status"})

	if err := g.engine.CheckOrder(ctx, g.conv.ID, first.ID); err != nil {
		t.Fatalf("the oldest one is free: %v", err)
	}
	var oe *chat.OrderError
	if err := g.engine.CheckOrder(ctx, g.conv.ID, last.ID); !errors.As(err, &oe) || oe.Earlier == "" {
		t.Fatalf("a later one waits for the earlier: %v", err)
	}
	first.Status = "rejected"
	_ = g.f.st.Actions().Update(ctx, first)
	if err := g.engine.CheckOrder(ctx, g.conv.ID, patch.ID); err != nil {
		t.Fatalf("the diff is now the oldest: %v", err)
	}
	if err := g.engine.CheckOrder(ctx, g.conv.ID, last.ID); !errors.As(err, &oe) || oe.Earlier != "diff 2 file" {
		t.Fatalf("the diff still waits before it: %v", err)
	}
	if !g.engine.Waiting(ctx, g.conv.ID) {
		t.Fatal("two proposals still wait")
	}
}

// The agent goes on only once every proposal of the chat is decided: one
// decided while another waits holds its word until the last is (Settled for
// a skipped one).
func TestDecidedWaitsForAllProposals(t *testing.T) {
	g := newGroup(t)
	g.sendAll(t, "chuẩn bị deploy")
	ctx := g.context
	other, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, Kind: "run_command", Target: "pnpm build"})
	g.engine.SetDecidedWait(50 * time.Millisecond)
	g.engine.Decided(g.conv.ID, "admin@x.io", "✅ Đã duyệt Chạy lệnh: pnpm test — ok")
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(g.dir, "call2.args")); err == nil {
		t.Fatal("the agent went on while a proposal still waits")
	}
	other.Status = "rejected" // skipped: no word of its own
	_ = g.f.st.Actions().Update(ctx, other)
	g.engine.Settled(g.conv.ID)
	g.waitAuthors(t, 2)
	if _, in := call(t, g.dir, 2); !strings.Contains(in, "pnpm test") {
		t.Fatalf("the held decision goes on:\n%s", in)
	}
}

// A diff the agent may apply on its own (CapApply) waits like the rest while
// an earlier proposal of the chat still waits.
func TestAutoApplyWaitsForEarlierProposals(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	srv := fakeAnthropic(t, "your own git worktree", []map[string]any{
		{"name": "edit_file", "input": map[string]any{"path": "hello.txt", "old": "hello", "new": "xin chào"}},
		{"name": "write_file", "input": map[string]any{"path": "extra.txt", "content": "x\n"}},
	})
	defer srv.Close()
	f := setup(t, func(provs *provider.Service) storage.Provider {
		key := "sk-ant-test-key-0000"
		p, _ := provs.Create(context.Background(), provider.Input{Name: "C", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: &key})
		return p
	})
	gitRun(t, f.dir, "init", "-q")
	gitRun(t, f.dir, "add", "-A")
	gitRun(t, f.dir, "commit", "-q", "-m", "init")
	f.engine.SetWorktrees(worktree.New(filepath.Join(t.TempDir(), "worktrees")))
	ctx := actor.With(context.Background(), "human:a@b.c")
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	f.engine.SetMode(ctx, conv.ID, perm.Edit)

	last := func(text string) chat.MessageDTO {
		turn, _, err := f.engine.Send(ctx, conv.ID, text, nil)
		if err != nil {
			t.Fatal(err)
		}
		evs := collect(t, turn)
		return *evs[len(evs)-1].Message
	}
	if m := last("đổi lời chào"); len(m.Patches) != 1 || m.Patches[0].Status != "applied" {
		t.Fatalf("nothing waits: applied on its own, got %+v", m.Patches)
	}
	msgs, _ := f.st.Chat().ListMessages(ctx, conv.ID)
	_, _ = f.st.Actions().Create(ctx, storage.Action{ProjectID: f.project.ID, ConversationID: conv.ID, MessageID: msgs[len(msgs)-1].ID, Kind: "run_command", Target: "pnpm test"})
	m := last("thêm file")
	if len(m.Patches) != 1 || m.Patches[0].Status != "pending" || m.Patches[0].Seq != 3 {
		t.Fatalf("an earlier proposal waits: the diff waits too (#3), got %+v", m.Patches)
	}
}

// Numbers follow the chat (answer by answer), and the order checked is the
// numbers' even when an answer's proposal was made before an earlier answer
// ended (two agents at once).
func TestOrderFollowsTheChat(t *testing.T) {
	g := newGroup(t)
	ctx := g.context
	g.sendAll(t, "một")
	g.sendAll(t, "hai")
	var answers []string
	msgs, _ := g.f.st.Chat().ListMessages(ctx, g.conv.ID)
	for _, m := range msgs {
		if m.Role == "assistant" {
			answers = append(answers, m.ID)
		}
	}
	if len(answers) < 2 {
		t.Fatalf("answers = %v", answers)
	}
	// made first, but belongs to the later answer
	late, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, MessageID: answers[1], Kind: "run_command", Target: "b"})
	time.Sleep(2 * time.Millisecond)
	early, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, MessageID: answers[0], Kind: "run_command", Target: "a"})
	var oe *chat.OrderError
	if err := g.engine.CheckOrder(ctx, g.conv.ID, late.ID); !errors.As(err, &oe) || oe.ID != early.ID || oe.Seq != 1 || !strings.Contains(err.Error(), "#1") {
		t.Fatalf("the earlier answer's goes first: %v", err)
	}
	h, _ := g.engine.History(ctx, g.conv.ID)
	seq := map[string]int{}
	for _, m := range h {
		for _, a := range m.Actions {
			seq[a.ID] = a.Seq
		}
	}
	if seq[early.ID] != 1 || seq[late.ID] != 2 {
		t.Fatalf("numbers = %v", seq)
	}
	if got := g.engine.WaitingOrder(ctx, g.conv.ID); len(got) != 2 || got[0] != early.ID {
		t.Fatalf("order = %v", got)
	}
}
