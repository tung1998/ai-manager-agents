package burn_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/burn"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
	"bitbucket.org/senprints/agent-office/internal/usage"
	"bitbucket.org/senprints/agent-office/internal/worktree"
)

type fx struct {
	st      storage.Store
	svc     *burn.Service
	engine  *chat.Engine
	trees   *worktree.Manager
	project storage.Repo
	dir     string
	bin     string // the fake Claude Code: a test may write its own
}

// setup: a git project, its lead, a Claude Code that writes a file where it
// runs (the item's worktree) and takes a moment. wrap, when given, lets a
// test swap in a storage.Store that misbehaves on purpose (eg. panics).
func setup(t *testing.T, wrap ...func(storage.Store) storage.Store) fx {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	ctx := context.Background()
	tmp := t.TempDir()
	real, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	t.Cleanup(func() { real.Close() })
	real.Migrate(ctx)
	var st storage.Store = real
	for _, w := range wrap {
		st = w(st)
	}
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	u := usage.New(st, time.UTC)
	provs.SetUsage(u)
	org := team.NewService(st, nil)
	bin := filepath.Join(tmp, "claude")
	os.WriteFile(bin, []byte(`#!/bin/sh
cat > /dev/null
echo "do agent viết" > made-by-agent.txt
sleep 1
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"xong lượt","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	provs.Create(ctx, provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
	dir := t.TempDir()
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-c", "user.email=t@x.io", "-c", "user.name=T"}, args...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	git("add", "-A")
	git("commit", "-qm", "init")
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: dir})
	solo, _ := team.PackByKey("solo")
	org.ApplyPack(ctx, project.ID, solo, false)
	engine := chat.NewEngine(st, provs, u)
	trees := worktree.New(filepath.Join(tmp, "trees"))
	engine.SetWorktrees(trees)
	svc := burn.New(st, engine, trees)
	svc.SetFindWork(false) // these tests queue their pieces themselves
	return fx{st: st, svc: svc, engine: engine, trees: trees, project: project, dir: dir, bin: bin}
}

func waitItem(t *testing.T, st storage.Store, id, status string) storage.BurnItem {
	t.Helper()
	return waitItemFor(t, st, id, status, 15*time.Second)
}

func waitItemFor(t *testing.T, st storage.Store, id, status string, timeout time.Duration) storage.BurnItem {
	t.Helper()
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		if it, err := st.Burn().Item(context.Background(), id); err == nil && it.Status == status {
			return it
		}
	}
	it, _ := st.Burn().Item(context.Background(), id)
	t.Fatalf("item %s: %s, want %s", id, it.Status, status)
	return it
}

// waitDelivered waits for a piece done to be merged into its run (its
// worktree dropped).
func waitDelivered(t *testing.T, st storage.Store, id string) storage.BurnItem {
	t.Helper()
	it := waitItem(t, st, id, "done")
	for deadline := time.Now().Add(10 * time.Second); it.Worktree != "" && time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		it, _ = st.Burn().Item(context.Background(), id)
	}
	if it.Worktree != "" {
		t.Fatalf("piece not merged into its run: %+v", it)
	}
	return it
}

// A chosen piece runs in its own worktree; reported done, what it changed is
// committed to the run's one burn/ branch (ADR-123), the next piece's too
// (nothing pushed, nothing on main).
func TestBurnDoesAPieceOnItsBranch(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	ends := time.Now().Add(time.Hour)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", MaxParallel: 1, ResultMode: "branch", EndsAt: &ends, State: "stopped"})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	out, err := f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Sửa lỗi thanh toán", Kind: "bug", Detail: "checkout"})
	if err != nil || !strings.Contains(out, "bit_") {
		t.Fatal(out, err)
	}
	if again, _ := f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "sửa lỗi  thanh toán", Kind: "bug"}); !strings.Contains(again, "Đã có") {
		t.Fatalf("the same piece twice: %s", again)
	}
	items, _ := f.st.Burn().Items(ctx, b.ID)
	it := items[0]
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: it.ID})
	it = waitItem(t, f.st, it.ID, "doing")
	f.svc.Tool(ctx, sc, "burn_done", burn.ToolInput{Item: it.ID, Summary: "đã sửa, test qua"})
	it = waitDelivered(t, f.st, it.ID)
	if b, _ = f.st.Burn().Session(ctx, f.project.ID); it.Branch != b.RunBranch || !strings.HasPrefix(it.Branch, "burn/") {
		t.Fatalf("item = %+v, run %s", it, b.RunBranch)
	}
	show := exec.Command("git", "show", "--stat", it.Branch)
	show.Dir = f.dir
	if got, _ := show.CombinedOutput(); !strings.Contains(string(got), "made-by-agent.txt") || !strings.Contains(string(got), "burn: Sửa lỗi thanh toán") {
		t.Fatalf("branch %s: %s", it.Branch, got)
	}
	// the next piece starts from the run and lands on the same branch
	os.WriteFile(f.bin, []byte(`#!/bin/sh
cat > /dev/null
test -f made-by-agent.txt && echo "thứ hai" > second.txt
sleep 1
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"xong lượt","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Việc thứ hai", Kind: "upgrade"})
	items, _ = f.st.Burn().Items(ctx, b.ID)
	next := items[len(items)-1]
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: next.ID})
	waitItem(t, f.st, next.ID, "doing")
	f.svc.Tool(ctx, sc, "burn_done", burn.ToolInput{Item: next.ID, Summary: "thêm second"})
	if next = waitDelivered(t, f.st, next.ID); next.Branch != it.Branch {
		t.Fatalf("second piece on %q, want %q", next.Branch, it.Branch)
	}
	log := exec.Command("git", "log", "--format=%s", "main.."+it.Branch)
	log.Dir = f.dir
	if got, _ := log.CombinedOutput(); string(got) != "burn: Việc thứ hai\nburn: Sửa lỗi thanh toán\n" {
		t.Fatalf("run branch log:\n%s", got)
	}
	if out, _ := exec.Command("git", "-C", f.dir, "branch", "--list", "burn/*").CombinedOutput(); strings.Count(string(out), "burn/") != 1 {
		t.Fatalf("one branch per run, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "made-by-agent.txt")); err == nil {
		t.Fatal("the project's own folder was changed")
	}
	// it was done in a hidden chat of its own (ADR-116), kept after it is done
	wc := it.WorkConversationID
	if c, err := f.st.Chat().GetConversation(ctx, wc); wc == "" || wc == b.ConversationID || err != nil || c.Purpose != chat.BurnWorkPurpose {
		t.Fatalf("work chat %q = %+v, %v", wc, c, err)
	}
	if s, err := f.st.Burn().SessionByConversation(ctx, wc); err != nil || s.ID != b.ID {
		t.Fatalf("its Burn = %+v, %v", s, err)
	}
	// there the burn_* tools touch that piece only
	other := next
	wsc := actions.Scope{ProjectID: f.project.ID, ConversationID: wc}
	if _, err := f.svc.Tool(ctx, wsc, "burn_fail", burn.ToolInput{Item: other.ID, Reason: "x"}); err == nil {
		t.Fatal("a piece's chat reported another piece")
	}
	if _, err := f.svc.Tool(ctx, wsc, "burn_pick", burn.ToolInput{Item: other.ID}); err == nil {
		t.Fatal("a piece's chat picked work")
	}
	if err := f.svc.Stop(ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	// stopped, the run's worktree stays on its branch, to review there
	b, _ = f.st.Burn().Session(ctx, f.project.ID)
	tree := f.svc.RunTree(b)
	head := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	head.Dir = tree
	if got, err := head.CombinedOutput(); tree == "" || err != nil || strings.TrimSpace(string(got)) != b.RunBranch {
		t.Fatalf("run worktree %q on %s (%v), want %s", tree, got, err, b.RunBranch)
	}
}

// Stopped mid-piece, the piece waits (its worktree kept); started again it
// goes on first.
func TestBurnStopPausesAndGoesOn(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", State: "stopped"})
	b, _ := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	it, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "làm dở", Kind: "unfinished", Status: "queued"})
	it = waitItem(t, f.st, it.ID, "doing")
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(it.Worktree, "made-by-agent.txt")); err == nil {
			break // its turn runs in its worktree
		}
	}
	if err := f.svc.Stop(ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	it = waitItem(t, f.st, it.ID, "paused")
	if it.Worktree == "" {
		t.Fatal("no worktree kept")
	}
	if _, err := os.Stat(it.Worktree); err != nil {
		t.Fatalf("worktree gone: %v", err)
	}
	if _, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io"); err != nil {
		t.Fatal(err)
	}
	waitItem(t, f.st, it.ID, "doing")
	f.svc.Stop(ctx, f.project.ID)
}

// Pieces run side by side up to the Burn's cap (ADR-117), each in its own
// worktree; the rest wait their turn.
func TestBurnWorksPiecesInParallel(t *testing.T) {
	f := setup(t)
	os.WriteFile(f.bin, []byte(`#!/bin/sh
cat > /dev/null
echo x > made-by-agent.txt
sleep 3
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"xong lượt","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", MaxParallel: 2, ResultMode: "branch", State: "stopped"})
	b, _ := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	defer f.svc.Stop(context.Background(), f.project.ID)
	for _, title := range []string{"việc một", "việc hai", "việc ba"} {
		f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: title, Kind: "upgrade", Status: "queued"})
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(30 * time.Millisecond) {
		items, _ := f.st.Burn().Items(ctx, b.ID)
		n := map[string]int{}
		trees := map[string]bool{}
		for _, it := range items {
			n[it.Status]++
			if it.Status == "doing" {
				trees[it.Worktree] = true
			}
		}
		if n["doing"] == 2 {
			if n["queued"] != 1 || len(trees) != 2 {
				t.Fatalf("statuses %v, worktrees %v", n, trees)
			}
			return
		}
		if n["doing"] > 2 {
			t.Fatalf("past the cap: %v", n)
		}
		if time.Now().After(deadline) {
			t.Fatalf("never two at once: %v", n)
		}
	}
}

// Drained, a Burn finishes the piece in progress, starts no other, then stops
// on its own; resumed meanwhile, it goes on as before.
func TestBurnDrainFinishesThenStops(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	sent := make(chan string, 4)
	f.svc.SetNotify(func(_ context.Context, channelID, chatID, text string) error {
		sent <- channelID + "/" + chatID + ": " + text
		return nil
	})
	ch, _ := f.st.Channels().Create(ctx, storage.Channel{ProjectID: f.project.ID, Kind: "discord", Name: "bot"})
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", MaxParallel: 1, ResultMode: "branch", State: "stopped",
		NotifyChannelID: ch.ID, NotifyChatID: "123"})
	b, _ := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	defer f.svc.Stop(context.Background(), f.project.ID)
	first, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "đang làm", Kind: "upgrade", Status: "queued"})
	waitItem(t, f.st, first.ID, "doing")
	second, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "chưa làm", Kind: "upgrade", Status: "queued"})
	if err := f.svc.Drain(ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Resume(ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	if cur, _ := f.st.Burn().Session(ctx, f.project.ID); cur.State != "running" {
		t.Fatalf("resumed: %s", cur.State)
	}
	f.svc.Drain(ctx, f.project.ID)
	f.svc.Tool(ctx, actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}, "burn_done", burn.ToolInput{Item: first.ID, Summary: "xong"})
	waitItem(t, f.st, first.ID, "done")
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(30 * time.Millisecond) {
		cur, _ := f.st.Burn().Session(ctx, f.project.ID)
		if cur.State == "stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("drained Burn still %s", cur.State)
		}
	}
	if it, _ := f.st.Burn().Item(ctx, second.ID); it.Status != "queued" || it.Worktree != "" {
		t.Fatalf("a new piece was started while draining: %+v", it)
	}
	// its summary: the last message of its chat, and to the bot's chat (ADR-120)
	msgs, _ := f.st.Chat().ListMessages(ctx, b.ConversationID)
	if last := msgs[len(msgs)-1]; last.Author != "Burn" || !strings.Contains(last.Content, "đã làm nốt việc dở") || !strings.Contains(last.Content, "đang làm") {
		t.Fatalf("last message: %+v", last)
	}
	// and its log on the way: started, the piece begun and done
	var log strings.Builder
	for _, m := range msgs {
		log.WriteString(m.Content + "\n")
	}
	for _, want := range []string{"Burn bắt đầu", "**Bắt đầu** đang làm", "Làm nốt việc", "Chạy tiếp", "**Xong** đang làm"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log has no %q:\n%s", want, log.String())
		}
	}
	select {
	case got := <-sent:
		if !strings.HasPrefix(got, ch.ID+"/123: ") || !strings.Contains(got, "Xong (1)") {
			t.Fatalf("sent %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the summary did not go to the bot")
	}
	if err := f.svc.Resume(ctx, f.project.ID); err == nil {
		t.Fatal("a stopped Burn resumed")
	}
}

// The real case (2026-10-05): its agent changed (the old one paused), a Burn
// started again talks with the new one, in a chat of its own.
func TestBurnFollowsItsAgent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	agents, _ := f.engine.Agents(ctx, f.project.ID)
	old := agents[0]
	other, err := f.st.Agents().Create(ctx, storage.Agent{ProjectID: old.ProjectID, Name: "Thay", ModelTier: old.ModelTier, Instructions: "x"})
	if err != nil {
		t.Fatal(err)
	}
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, AgentID: old.ID, ModelTier: "fast", ResultMode: "branch", State: "stopped"})
	b, err := f.svc.Begin(ctx, f.project.ID, "a")
	if err != nil {
		t.Fatal(err)
	}
	first := b.ConversationID
	f.svc.Stop(ctx, f.project.ID)
	b.AgentID, b.State = other.ID, "stopped"
	f.st.Burn().SaveSession(ctx, b)
	if b, err = f.svc.Begin(ctx, f.project.ID, "a"); err != nil {
		t.Fatal(err)
	}
	c, _ := f.st.Chat().GetConversation(ctx, b.ConversationID)
	if b.ConversationID == first || c.AgentID != other.ID {
		t.Fatalf("conversation %s (first %s) of %s, want %s", b.ConversationID, first, c.AgentID, other.ID)
	}
	if again, _ := f.svc.Begin(ctx, f.project.ID, "a"); again.ConversationID != b.ConversationID {
		t.Fatal("running: started again, a new chat")
	}
	f.svc.Stop(ctx, f.project.ID)
	if again, _ := f.svc.Begin(ctx, f.project.ID, "a"); again.ConversationID == b.ConversationID {
		t.Fatal("each start, a chat of its own")
	}
	f.svc.Stop(ctx, f.project.ID)
	f.st.Agents().SetEnabled(ctx, other.ID, false) // paused: refused at once, with what to do
	if _, err := f.svc.Begin(ctx, f.project.ID, "a"); err == nil || !strings.Contains(err.Error(), "đang tắt") {
		t.Fatalf("a paused agent started: %v", err)
	}
}

// Its time up, a Burn stops; the tools are its conversation's only.
func TestBurnEndsAndToolsAreItsOwn(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ResultMode: "branch", EndsAt: &past, State: "stopped"})
	if _, err := f.svc.Begin(ctx, f.project.ID, "a"); err == nil {
		t.Fatal("started past its stop time")
	}
	if _, err := f.svc.Tool(ctx, actions.Scope{ProjectID: f.project.ID, ConversationID: "cnv_other"}, "burn_add", burn.ToolInput{Title: "x"}); err == nil {
		t.Fatal("a burn tool worked outside its conversation")
	}
}

// The real case (2026-10-01): a Burn whose agent may only read in its mode
// still runs with full access, so it writes. It must write in its own
// worktree, never in the project's folder.
func TestBurnReadOnlyAgentStillWorksInItsWorktree(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agents, _ := f.st.Agents().List(ctx, f.project.ID)
	for _, a := range agents {
		a.Permissions.Level = perm.Read
		f.st.Agents().Update(ctx, a)
	}
	f.svc.Start(ctx)
	ends := time.Now().Add(time.Hour)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", EndsAt: &ends, State: "stopped"})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Việc chỉ đọc", Kind: "bug"})
	items, _ := f.st.Burn().Items(ctx, b.ID)
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: items[0].ID})
	it := waitItem(t, f.st, items[0].ID, "doing")
	f.svc.Tool(ctx, sc, "burn_done", burn.ToolInput{Item: it.ID, Summary: "xong"})
	it = waitItem(t, f.st, it.ID, "done")
	if _, err := os.Stat(filepath.Join(f.dir, "made-by-agent.txt")); err == nil {
		t.Fatal("a read-only agent with full access wrote in the project's own folder")
	}
	if strings.Contains(it.Summary, "không giao được") {
		t.Fatalf("not committed: %s", it.Summary)
	}
	f.svc.Stop(ctx, f.project.ID)
}

// Office starting sweeps worktrees whose chat is gone: a Burn piece's own is
// not one of those (its work waits there while paused).
func TestSweepKeepsBurnWorktrees(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	b, _ := f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ResultMode: "branch", State: "stopped"})
	it, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "x", Kind: "bug", Status: "paused"})
	for _, name := range []string{"burn-" + it.ID, "burn-scan-" + b.ID, "burn-bit_gone"} {
		if _, err := f.trees.Ensure(ctx, f.dir, f.project.ID, name, nil); err != nil {
			t.Fatal(err)
		}
	}
	f.engine.SweepWorktrees(ctx, 0)
	for name, kept := range map[string]bool{"burn-" + it.ID: true, "burn-scan-" + b.ID: true, "burn-bit_gone": false} {
		if got := f.trees.Exists(f.project.ID, name); got != kept {
			t.Errorf("%s: exists=%v, want %v", name, got, kept)
		}
	}
}

// Office's start sweeps worktrees idle for 14 days by their folder's time,
// which does not move when only files inside change: a piece in progress
// (paused, or doing) keeps its worktree however old; a piece that is over,
// a scan of a session that is gone, an orphan go.
func TestSweepOldKeepsWorkInProgress(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	b, _ := f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ResultMode: "branch", State: "stopped"})
	paused, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "dở", Kind: "bug", Status: "paused"})
	doing, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "đang làm", Kind: "bug", Status: "doing"})
	done, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "xong", Kind: "bug", Status: "done"})
	want := map[string]bool{"burn-" + paused.ID: true, "burn-" + doing.ID: true, "burn-" + done.ID: false,
		"burn-scan-bse_gone": false, "burn-bit_gone": false}
	month := time.Now().Add(-30 * 24 * time.Hour)
	for name := range want {
		dir, err := f.trees.Ensure(ctx, f.dir, f.project.ID, name, nil)
		if err != nil {
			t.Fatal(err)
		}
		os.Chtimes(dir, month, month)
	}
	f.engine.SweepWorktrees(ctx, 14*24*time.Hour)
	for name, kept := range want {
		if got := f.trees.Exists(f.project.ID, name); got != kept {
			t.Errorf("%s: exists=%v, want %v", name, got, kept)
		}
	}
}

// Office restarted while a piece ran: started again, the piece waits (its
// worktree and what it did there kept), then goes on first.
func TestBurnGoesOnAfterRestart(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, _ := f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", State: "stopped"})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io") // its conversation; no loop yet (not started)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "đang làm khi tắt", Kind: "bug", Status: "doing", Attempts: 1})
	dir, err := f.trees.Ensure(ctx, f.dir, f.project.ID, "burn-"+it.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "half-done.txt"), []byte("dở\n"), 0o644)
	it.Worktree = dir
	f.st.Burn().UpdateItem(ctx, it)
	// as office starts again: the sweep, then Burn
	f.engine.SweepWorktrees(ctx, 14*24*time.Hour)
	f.svc.Start(ctx)
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		if it, _ = f.st.Burn().Item(ctx, it.ID); it.Status == "doing" && it.Attempts == 2 {
			break // its turn again, after the restart
		}
	}
	if it.Status != "doing" || it.Attempts != 2 {
		t.Fatalf("after restart = %s, attempts %d", it.Status, it.Attempts)
	}
	if it.Worktree != dir {
		t.Fatalf("worktree = %s, want %s", it.Worktree, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "half-done.txt")); err != nil {
		t.Fatalf("what it did is gone: %v", err)
	}
	f.svc.Stop(ctx, f.project.ID)
}

// Without git there is no worktree: Burn would edit the project's own folder,
// so it does not start.
func TestBurnNeedsGit(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	plain, _ := f.st.Repos().Create(ctx, storage.Repo{Name: "plain", Path: t.TempDir()})
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: plain.ID, ResultMode: "branch", State: "stopped"})
	_, err := f.svc.Begin(ctx, plain.ID, "admin@x.io")
	if err == nil || !strings.Contains(err.Error(), "git") {
		t.Fatalf("Burn on a folder without git: %v", err)
	}
}

// reviewer answers the Burn's reviews (ADR-112): the problem turned down,
// the result agreed; a work turn writes a file, as setup's.
const reviewer = `#!/bin/sh
p=$(cat)
case "$p" in
*"issue review"*) r="VERDICT: DISAGREE. Không có thật";;
*"result review"*) r="VERDICT: AGREE. Đúng phạm vi";;
*) echo "do agent viết" > made-by-agent.txt; sleep 1; r="xong lượt";;
esac
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"'"$r"'","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`

// profile saves a review profile reviewing stages, by the Burn's agent.
func (f fx) profile(t *testing.T, stages ...string) string {
	t.Helper()
	p := storage.BurnReviewProfile{ProjectID: f.project.ID, Name: "test", Stages: map[string]storage.BurnReviewStage{}}
	for _, st := range stages {
		p.Stages[st] = storage.BurnReviewStage{}
	}
	p, err := f.st.Burn().SaveReviewProfile(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// The problem's review turns a chosen piece down: skipped with why, never done.
func TestBurnReviewTurnsAPieceDown(t *testing.T) {
	f := setup(t)
	os.WriteFile(f.bin, []byte(reviewer), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	ends := time.Now().Add(time.Hour)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", EndsAt: &ends, State: "stopped", ReviewProfileID: f.profile(t, "issue")})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Lỗi tưởng tượng", Kind: "bug"})
	items, _ := f.st.Burn().Items(ctx, b.ID)
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: items[0].ID})
	it := waitItem(t, f.st, items[0].ID, "skipped")
	if !strings.Contains(it.Summary, "Review vấn đề") || !strings.Contains(it.ReviewNote, "Không có thật") || it.Worktree != "" {
		t.Fatalf("item = %+v", it)
	}
	rc := it.ReviewConversations["issue"]
	if rc == "" || rc == b.ConversationID {
		t.Fatalf("the review has no chat of its own: %+v", it)
	}
	// hidden: of its own kind, not in the chat list
	if c, err := f.st.Chat().GetConversation(ctx, rc); err != nil || c.Purpose != chat.BurnReviewPurpose {
		t.Fatalf("review chat = %+v, %v", c, err)
	}
	if s, err := f.st.Burn().SessionByConversation(ctx, rc); err != nil || s.ID != b.ID {
		t.Fatalf("its Burn = %+v, %v", s, err)
	}
	if _, err := f.svc.Tool(ctx, actions.Scope{ProjectID: f.project.ID, ConversationID: rc}, "burn_done", burn.ToolInput{Item: it.ID, Summary: "x"}); err == nil {
		t.Fatal("the reviewer's chat used a burn_* tool")
	}
	f.svc.Stop(ctx, f.project.ID)
}

// The result's review: done waits for it, agreed it is committed.
func TestBurnReviewAgreesTheResult(t *testing.T) {
	f := setup(t)
	os.WriteFile(f.bin, []byte(reviewer), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	ends := time.Now().Add(time.Hour)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", EndsAt: &ends, State: "stopped", ReviewProfileID: f.profile(t, "result")})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Thêm file", Kind: "upgrade"})
	items, _ := f.st.Burn().Items(ctx, b.ID)
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: items[0].ID})
	it := waitItem(t, f.st, items[0].ID, "doing")
	f.svc.Tool(ctx, sc, "burn_done", burn.ToolInput{Item: it.ID, Summary: "đã thêm"})
	if got, _ := f.st.Burn().Item(ctx, it.ID); got.Status != "review" {
		t.Fatalf("done before its review: %s", got.Status)
	}
	it = waitDelivered(t, f.st, it.ID)
	if !strings.Contains(strings.Join(it.Reviewed, ","), "result") || !strings.Contains(it.ReviewNote, "Đúng phạm vi") {
		t.Fatalf("item = %+v", it)
	}
	show := exec.Command("git", "show", "--stat", it.Branch)
	show.Dir = f.dir
	if got, _ := show.CombinedOutput(); !strings.Contains(string(got), "made-by-agent.txt") {
		t.Fatalf("branch %s: %s", it.Branch, got)
	}
	f.svc.Stop(ctx, f.project.ID)
}

// burn_list gives what the coordination prompt leaves out (ADR-121).
func TestBurnListTool(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", State: "stopped"})
	b, _ := f.svc.Begin(ctx, f.project.ID, "a")
	f.svc.Stop(ctx, f.project.ID)
	f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "việc mở", Kind: "bug", Detail: "ở a.go:1", Status: "found"})
	f.st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "việc xong", Kind: "bug", Status: "done", Summary: "đã sửa"})
	f.st.Burn().SetScanned(ctx, b.ID, "08/10 09:00 internal/chat")
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	for what, want := range map[string]string{"open": "a.go:1", "closed": "đã sửa", "scanned": "internal/chat"} {
		out, err := f.svc.Tool(ctx, sc, "burn_list", burn.ToolInput{What: what})
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("burn_list(%s) = %q, %v", what, out, err)
		}
	}
	if out, _ := f.svc.Tool(ctx, sc, "burn_list", burn.ToolInput{What: "open"}); strings.Contains(out, "việc xong") {
		t.Errorf("open lists a closed piece: %s", out)
	}
}

// A reviewer that always crashes on the result (a system error, not a
// turned-down piece) must not leave the piece queued forever: past a few
// tries in a row it is failed (ADR-120).
func TestBurnReviewSystemErrorFailsAfterAFewTries(t *testing.T) {
	f := setup(t)
	os.WriteFile(f.bin, []byte(`#!/bin/sh
p=$(cat)
case "$p" in
*"result review"*)
  echo "fatal: reviewer crashed" >&2
  exit 1
  ;;
*) [ "$1" = auth ] || echo "do agent viết" > made-by-agent.txt; sleep 1; r="xong lượt";; # not the CLI login probe after a failed turn: it runs in the package folder
esac
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"'"$r"'","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	ends := time.Now().Add(time.Hour)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", EndsAt: &ends, State: "stopped", ReviewProfileID: f.profile(t, "result")})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Việc nào đó", Kind: "bug"})
	items, _ := f.st.Burn().Items(ctx, b.ID)
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: items[0].ID})
	waitItem(t, f.st, items[0].ID, "doing")
	f.svc.Tool(ctx, sc, "burn_done", burn.ToolInput{Item: items[0].ID, Summary: "xong"})
	it := waitItemFor(t, f.st, items[0].ID, "failed", 3*time.Minute)
	if !strings.Contains(it.Summary, "lỗi hệ thống liên tục") {
		t.Fatalf("item = %+v", it)
	}
	f.svc.Stop(ctx, f.project.ID)
}

// A reviewer that crashes once, then answers, is not a stuck loop: the piece
// goes on, and its error count is back to zero (ADR-120).
func TestBurnReviewSystemErrorOnceThenGoesOn(t *testing.T) {
	f := setup(t)
	tries := filepath.Join(f.dir, "review-tries")
	os.WriteFile(f.bin, []byte(`#!/bin/sh
p=$(cat)
case "$p" in
*"result review"*)
  n=$(cat `+tries+` 2>/dev/null || echo 0)
  n=$((n+1))
  echo "$n" > `+tries+`
  if [ "$n" -le 1 ]; then
    echo "fatal: reviewer crashed" >&2
    exit 1
  fi
  r="VERDICT: AGREE. Có thật"
  ;;
*) [ "$1" = auth ] || echo "do agent viết" > made-by-agent.txt; sleep 1; r="xong lượt";; # not the CLI login probe after a failed turn: it runs in the package folder
esac
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"'"$r"'","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	ends := time.Now().Add(time.Hour)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", ResultMode: "branch", EndsAt: &ends, State: "stopped", ReviewProfileID: f.profile(t, "result")})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Việc nào đó", Kind: "bug"})
	items, _ := f.st.Burn().Items(ctx, b.ID)
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: items[0].ID})
	waitItem(t, f.st, items[0].ID, "doing")
	f.svc.Tool(ctx, sc, "burn_done", burn.ToolInput{Item: items[0].ID, Summary: "xong"})
	it := waitItemFor(t, f.st, items[0].ID, "done", 2*time.Minute)
	if it.ReviewErrAttempts != 0 {
		t.Fatalf("item = %+v", it)
	}
	f.svc.Stop(ctx, f.project.ID)
}

// panicOnWorktreeSave panics once UpdateItem is called right after work()
// has saved a piece's worktree and work chat (burn.go: the save right before
// it runs the agent) — the spot a real panic in run/gate/finish would follow.
type panicOnWorktreeSave struct {
	storage.Store
	armed *int32
}

func (w panicOnWorktreeSave) Burn() storage.BurnRepo {
	return panicBurnRepo{w.Store.Burn(), w.armed}
}

type panicBurnRepo struct {
	storage.BurnRepo
	armed *int32
}

func (r panicBurnRepo) UpdateItem(ctx context.Context, it storage.BurnItem) error {
	if it.Status == "doing" && it.Worktree != "" && it.WorkConversationID != "" && atomic.CompareAndSwapInt32(r.armed, 0, 1) {
		_ = r.BurnRepo.UpdateItem(ctx, it) // the real save still happens first, as it would before any later panic
		panic("boom: simulated panic in work/run")
	}
	return r.BurnRepo.UpdateItem(ctx, it)
}

// A panic while a piece is worked on fails only that piece — its worktree
// and work chat stay as they were, and the office (and other projects'
// Burns) keep running.
func TestBurnStepPanicFailsThePieceNotTheOffice(t *testing.T) {
	var armed int32
	f := setup(t, func(st storage.Store) storage.Store { return panicOnWorktreeSave{st, &armed} })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	defer f.svc.Stop(ctx, f.project.ID)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", MaxParallel: 1, ResultMode: "branch", State: "stopped"})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	out, err := f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Việc sẽ panic", Kind: "bug", Detail: "x"})
	if err != nil || !strings.Contains(out, "bit_") {
		t.Fatal(out, err)
	}
	items, _ := f.st.Burn().Items(ctx, b.ID)
	it := items[0]
	f.svc.Tool(ctx, sc, "burn_pick", burn.ToolInput{Item: it.ID})
	it = waitItem(t, f.st, it.ID, "failed")
	if it.Worktree == "" || it.WorkConversationID == "" {
		t.Fatalf("item lost its worktree/work chat after the panic: %+v", it)
	}
	if !atomic.CompareAndSwapInt32(&armed, 1, 1) { // it did panic, not skipped
		t.Fatal("the panic never fired")
	}
	// the project's Burn loop is still alive: it can still take new work
	out, err = f.svc.Tool(ctx, sc, "burn_add", burn.ToolInput{Title: "Việc sau panic", Kind: "bug"})
	if err != nil || !strings.Contains(out, "bit_") {
		t.Fatal(out, err)
	}
}

// waitWorker waits for a worker of the Burn (ADR-126) at work, not one of seen.
func waitWorker(t *testing.T, st storage.Store, sessionID string, seen map[string]bool) storage.BurnItem {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		items, _ := st.Burn().Items(context.Background(), sessionID)
		for _, it := range items {
			if it.Kind == "" && it.Status == "doing" && it.WorkConversationID != "" && !seen[it.ID] {
				return it
			}
		}
	}
	t.Fatal("no worker started")
	return storage.BurnItem{}
}

// ADR-130: a scan only records pieces with their brief and ends; workers do
// each piece from found (highest priority first), each landing on the run's
// branch; the code map and the areas looked at are kept. Three scans in a
// row that record nothing, all done: the Burn finishes.
func TestBurnScanRecordsThenWorkersDo(t *testing.T) {
	f := setup(t)
	f.svc.SetFindWork(true)
	os.WriteFile(f.bin, []byte(`#!/bin/sh
cat > /dev/null
echo "do agent viết" > made-by-agent.txt
sleep 3
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"xong lượt","session_id":"s1","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", MaxParallel: 1, State: "stopped"})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	defer f.svc.Stop(context.Background(), f.project.ID)
	seen := map[string]bool{}
	scan := waitWorker(t, f.st, b.ID, seen)
	seen[scan.ID] = true
	if !strings.Contains(scan.Title, "quét") {
		t.Fatalf("scan = %+v", scan)
	}
	ssc := actions.Scope{ProjectID: f.project.ID, ConversationID: scan.WorkConversationID}
	if _, err := f.svc.Tool(ctx, ssc, "burn_done", burn.ToolInput{Item: scan.ID, Summary: "x"}); err == nil {
		t.Fatal("a scan reported work done")
	}
	f.svc.Tool(ctx, ssc, "burn_add", burn.ToolInput{Title: "Sửa chú thích", Kind: "upgrade", Priority: "low", Detail: "a.go:1"})
	if out, err := f.svc.Tool(ctx, ssc, "burn_add", burn.ToolInput{Title: "Thêm file", Kind: "upgrade", Priority: "high", Detail: "Where: a.go"}); err != nil || !strings.Contains(out, "bit_") {
		t.Fatal(out, err)
	}
	if again, _ := f.svc.Tool(ctx, ssc, "burn_add", burn.ToolInput{Title: "thêm  file", Kind: "upgrade"}); !strings.Contains(again, "Đã có") {
		t.Fatalf("the same piece twice: %s", again)
	}
	if _, err := f.svc.Tool(ctx, ssc, "burn_scan_done", burn.ToolInput{Item: scan.ID, Scanned: "internal/a", Map: "a.go: gói a"}); err != nil {
		t.Fatal(err)
	}
	if b, _ = f.st.Burn().Session(ctx, f.project.ID); b.CodeMap != "a.go: gói a" || !strings.Contains(b.Scanned, "internal/a") {
		t.Fatalf("map %q, scanned %q", b.CodeMap, b.Scanned)
	}
	// the high one first, each by a worker of its own, no scan meanwhile (one slot)
	for _, title := range []string{"Thêm file", "Sửa chú thích"} {
		var it storage.BurnItem
		for deadline := time.Now().Add(15 * time.Second); it.ID == "" && time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
			items, _ := f.st.Burn().Items(ctx, b.ID)
			for _, x := range items {
				if x.Status == "doing" && x.Kind != "" && x.WorkConversationID != "" {
					it = x
				}
			}
		}
		if it.Title != title {
			t.Fatalf("doing %+v, want %s", it, title)
		}
		f.svc.Tool(ctx, actions.Scope{ProjectID: f.project.ID, ConversationID: it.WorkConversationID}, "burn_done", burn.ToolInput{Item: it.ID, Summary: "xong"})
		waitDelivered(t, f.st, it.ID)
	}
	log := exec.Command("git", "log", "--format=%s", "main.."+b.RunBranch)
	log.Dir = f.dir
	if got, _ := log.CombinedOutput(); !strings.Contains(string(got), "burn: Thêm file") {
		t.Fatalf("run branch log:\n%s", got)
	}
	// scans that find nothing: three in a row, all done, it finishes
	for i := 0; i < 3; i++ {
		n := waitWorker(t, f.st, b.ID, seen)
		seen[n.ID] = true
		if _, err := f.svc.Tool(ctx, actions.Scope{ProjectID: f.project.ID, ConversationID: n.WorkConversationID}, "burn_scan_done", burn.ToolInput{Item: n.ID, Scanned: "x", Reason: "hết việc"}); err != nil {
			t.Fatal(err)
		}
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if cur, _ := f.st.Burn().Session(ctx, f.project.ID); cur.State == "stopped" {
			break
		}
		if time.Now().After(deadline) {
			cur, _ := f.st.Burn().Session(ctx, f.project.ID)
			t.Fatalf("still %s after three scans found nothing", cur.State)
		}
	}
}

// A piece a scan found meets the issue review before a worker does it; the
// reviewer turning it down skips it. A scan that ends its turn without
// burn_scan_done is over all the same.
func TestBurnScannedPieceMeetsTheIssueReview(t *testing.T) {
	f := setup(t)
	f.svc.SetFindWork(true)
	os.WriteFile(f.bin, []byte(strings.Replace(reviewer, "sleep 1", "sleep 3", 1)), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", MaxParallel: 1, State: "stopped", ReviewProfileID: f.profile(t, "issue")})
	b, err := f.svc.Begin(ctx, f.project.ID, "admin@x.io")
	if err != nil {
		t.Fatal(err)
	}
	defer f.svc.Stop(context.Background(), f.project.ID)
	scan := waitWorker(t, f.st, b.ID, map[string]bool{})
	ssc := actions.Scope{ProjectID: f.project.ID, ConversationID: scan.WorkConversationID}
	if _, err := f.svc.Tool(ctx, ssc, "burn_add", burn.ToolInput{Title: "Lỗi tưởng tượng", Kind: "bug", Detail: "a.go"}); err != nil {
		t.Fatal(err)
	}
	var id string
	items, _ := f.st.Burn().Items(ctx, b.ID)
	for _, x := range items {
		if x.Title == "Lỗi tưởng tượng" {
			id = x.ID
		}
	}
	it := waitItem(t, f.st, id, "skipped")
	if !strings.Contains(it.Summary, "Review vấn đề") || it.ReviewConversations["issue"] == "" {
		t.Fatalf("item = %+v", it)
	}
	if _, err := f.st.Burn().Item(ctx, scan.ID); err == nil {
		t.Fatal("the scan is still there after its turn")
	}
}

// A quest the person gives waits in found, with no run until it starts; a
// running Burn takes it at once, even with no worker looking for work, and
// it lands in the run it was done in.
func TestBurnDoesAQuest(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ends := time.Now().Add(time.Hour)
	b, _ := f.st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: f.project.ID, ModelTier: "fast", MaxParallel: 1, EndsAt: &ends, State: "stopped"})
	if _, err := f.svc.AddQuest(ctx, b, "  ", ""); err == nil {
		t.Fatal("a quest with no title")
	}
	q, err := f.svc.AddQuest(ctx, b, "Thêm nút  xuất CSV", "trang đơn hàng")
	if err != nil || q.Kind != burn.KindQuest || q.Status != "found" || q.RunBranch != "" || q.Title != "Thêm nút xuất CSV" {
		t.Fatalf("quest = %+v, %v", q, err)
	}
	f.svc.Start(ctx)
	if b, err = f.svc.Begin(ctx, f.project.ID, "admin@x.io"); err != nil {
		t.Fatal(err)
	}
	waitItem(t, f.st, q.ID, "doing")
	sc := actions.Scope{ProjectID: f.project.ID, ConversationID: b.ConversationID}
	f.svc.Tool(ctx, sc, "burn_done", burn.ToolInput{Item: q.ID, Summary: "đã thêm"})
	if q = waitDelivered(t, f.st, q.ID); q.RunBranch != b.RunBranch || q.Kind != burn.KindQuest {
		t.Fatalf("quest done = %+v, run %s", q, b.RunBranch)
	}
}
