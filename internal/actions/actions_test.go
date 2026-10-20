package actions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ops"
	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func TestAutoByPackage(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	test, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "test", Kind: "job", Command: "echo ok"})
	dev, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "dev", Kind: "service", Command: "sleep 30"})
	m := ops.NewManager(st, t.TempDir(), nil)
	defer m.Shutdown()
	svc := New(st, m)
	allProcs = []string{test.ID, dev.ID}

	sc := Scope{ProjectID: proj.ID, RunRef: "r1", Agent: "a", Level: perm.Propose}
	if a, _ := svc.Propose(ctx, sc, "run_process", "test", "kiểm tra"); a.Status != "pending" {
		t.Fatalf("propose level must wait: %s", a.Status)
	}
	sc.Level, sc.Access, sc.RunRef = perm.Check, at(perm.Check), "r2"
	sc.ConversationID, sc.JobID = "", "job_7"
	auto, _ := svc.Propose(ctx, sc, "run_process", "test", "kiểm tra")
	if auto.Status != "done" {
		t.Fatalf("check level runs an allowed job: %s %s", auto.Status, auto.Detail)
	}
	// ADR-043: what the agent did on its own is in the log, as the agent's
	rows, _ := st.Audit().List(ctx, storage.AuditFilter{JobID: "job_7"})
	if len(rows) != 1 || rows[0].Action != "action.approve" || rows[0].ActorKind != "agent" || rows[0].ActorName != "a" ||
		rows[0].ApprovedBy != "" || rows[0].ActionID != auto.ID || rows[0].ProjectID != proj.ID || rows[0].Detail["auto"] != true || !rows[0].OK {
		t.Fatalf("auto action audit: %+v", rows)
	}
	if a, _ := svc.Propose(ctx, sc, "restart_process", "dev", "treo"); a.Status != "pending" {
		t.Fatalf("restarting a service needs operate: %s", a.Status)
	}
	sc.Level, sc.Access, sc.RunRef = perm.Operate, at(perm.Operate), "r3"
	if a, _ := svc.Propose(ctx, sc, "run_process", "dev", "bật"); a.Status != "done" {
		t.Fatalf("operate runs an allowed service: %s %s", a.Status, a.Detail)
	}
	if _, err := svc.Propose(ctx, sc, "run_process", "nope", ""); err == nil {
		t.Fatal("unknown target must fail")
	}
}

func TestGitActions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	root := t.TempDir()
	gitRun := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	gitRun("init", "-q", "-b", "main")
	gitRun("config", "user.email", "t@x.io")
	gitRun("config", "user.name", "T")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("1\n"), 0o644)
	gitRun("add", ".")
	gitRun("commit", "-qm", "init")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("2\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".env"), []byte("S=1\n"), 0o644)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("b\n"), 0o644)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "g", Path: root})
	pol := perm.DefaultPolicy()
	pol.MaxLevel = perm.Operate
	perm.SavePolicy(ctx, st, proj.ID, pol)
	svc := New(st, nil)

	sc := Scope{ProjectID: proj.ID, RunRef: "g1", Agent: "a", Level: perm.Propose}
	a, err := svc.Propose(ctx, sc, "git_commit", "", "lưu", storage.ActionArgs{Message: "feat: a", Files: []string{"a.txt"}})
	if err != nil || a.Status != "pending" {
		t.Fatalf("propose must wait: %v %+v", err, a)
	}
	if _, err := svc.Propose(ctx, sc, "git_commit", "", "x", storage.ActionArgs{Message: "leak", Files: []string{".env"}}); err == nil {
		t.Fatal("denied file must not be committed")
	}
	sc.Level, sc.Access, sc.RunRef = perm.Edit, at(perm.Edit), "g2"
	a, err = svc.Propose(ctx, sc, "git_commit", "", "lưu", storage.ActionArgs{Message: "feat: b", Files: []string{"b.txt"}})
	if err != nil || a.Status != "done" {
		t.Fatalf("edit level commits on its own: %v %+v", err, a)
	}
	out, _ := exec.Command("git", "-C", root, "log", "--oneline", "-1", "--name-only").Output()
	if !strings.Contains(string(out), "feat: b") || !strings.Contains(string(out), "b.txt") || strings.Contains(string(out), "a.txt") {
		t.Fatalf("commit content: %s", out)
	}
	sc.Level, sc.Access, sc.RunRef = perm.Operate, at(perm.Operate), "g3"
	if a, _ := svc.Propose(ctx, sc, "git_push", "", "đẩy"); a.Status != "pending" {
		t.Fatalf("push must always wait: %s", a.Status)
	}
	if a, err := svc.Propose(ctx, sc, "git_branch", "", "nhánh", storage.ActionArgs{Branch: "feat/x"}); err != nil || a.Status != "done" {
		t.Fatalf("operate creates branches: %v %+v", err, a)
	}

	// a chat in its own worktree: git waits while its changes are not merged
	// into the project, then works on the project once they are
	conv, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: proj.ID})
	wt := Scope{ProjectID: proj.ID, ConversationID: conv.ID, RunRef: "g4", Agent: "a", Level: perm.Operate, Access: at(perm.Operate), Dir: t.TempDir()}
	p, _ := st.Chat().AddPatch(ctx, storage.Patch{ConversationID: conv.ID, Diff: "x", Files: []string{"c.txt"}, Status: "pending", Origin: "worktree"})
	if _, err := svc.Propose(ctx, wt, "git_branch", "", "nhánh", storage.ActionArgs{Branch: "feat/y"}); err == nil || !strings.Contains(err.Error(), "gộp") {
		t.Fatalf("git with unmerged changes = %v", err)
	}
	st.Chat().DecidePatch(ctx, p.ID, "applied", "", "human:a", time.Now())
	wt.RunRef = "g5"
	if a, err := svc.Propose(ctx, wt, "git_branch", "", "nhánh", storage.ActionArgs{Branch: "feat/y"}); err != nil || a.Status != "done" {
		t.Fatalf("git after the merge = %v %+v", err, a)
	}
}

// at is the preset access of a package.
// at: an agent at level allowed to run/restart every process of the test
func at(level string) perm.Access {
	return perm.Access{Level: level, Caps: perm.Preset(level), Processes: allProcs}
}

var allProcs []string

func TestRunCommand(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "hello.txt"), []byte("xin chào\n"), 0o644)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "c", Path: root})
	svc := New(st, nil)
	acc := perm.Access{Level: perm.Check, Caps: []string{perm.CapPropose, perm.CapCommands}, Commands: []string{"cat *"}}
	sc := Scope{ProjectID: proj.ID, RunRef: "c1", Agent: "a", Level: perm.Check, Access: acc}

	a, err := svc.Propose(ctx, sc, "run_command", "cat hello.txt", "xem")
	if err != nil || a.Status != "done" || !strings.Contains(a.Detail, "xin chào") {
		t.Fatalf("allowed command runs: %v %+v", err, a)
	}
	sc.RunRef = "c2"
	if a, err := svc.Propose(ctx, sc, "run_command", "ls", "xem"); err != nil || a.Status != "pending" {
		t.Fatalf("other commands wait: %v %+v", err, a)
	}
	if _, err := svc.Propose(ctx, sc, "run_command", "cat hello.txt | wc", "x"); err == nil {
		t.Fatal("shell syntax must be rejected")
	}
	sc.RunRef = "c3"
	if a, _ := svc.Propose(ctx, sc, "run_command", "cat missing.txt", "x"); a.Status != "failed" {
		t.Fatalf("a failing command is failed: %+v", a)
	}

	// in a chat a pending command is reused, but not one for another folder
	conv, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: proj.ID})
	sc.ConversationID, sc.Dir = conv.ID, root
	first, _ := svc.Propose(ctx, sc, "run_command", "ls", "x")
	if again, _ := svc.Propose(ctx, sc, "run_command", "ls", "x"); again.ID != first.ID {
		t.Fatalf("same command, same folder: one card: %s %s", first.ID, again.ID)
	}
	sc.Dir = ""
	if other, _ := svc.Propose(ctx, sc, "run_command", "ls", "x"); other.ID == first.ID || other.Status != "pending" {
		t.Fatalf("same command in another folder is its own card: %+v", other)
	}

	// whoever decided gave up waiting (a tool call timed out): it still runs
	// and is recorded, not left pending
	gone, cancel := context.WithCancel(ctx)
	cancel()
	svc.Decide(gone, first.ID, true, "x")
	if got, _ := st.Actions().Get(ctx, first.ID); got.Status != "done" {
		t.Fatalf("a decided command ends recorded: %+v", got)
	}
}

// TestRunCommandRisky: a risky pattern saved to an agent's commands before
// putPolicy rejected them (or by any other path) must still never auto-run.
func TestRunCommandRisky(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "r", Path: t.TempDir()})
	svc := New(st, nil)
	acc := perm.Access{Level: perm.Operate, Caps: []string{perm.CapPropose, perm.CapCommands},
		Commands: []string{"git push *", "go version *", "git -C *", "git --no-pager *", "git -c *"}}
	sc := Scope{ProjectID: proj.ID, RunRef: "r1", Agent: "a", Level: perm.Operate, Access: acc}

	if a, err := svc.Propose(ctx, sc, "run_command", "git push origin main --force", "x"); err != nil || a.Status != "pending" {
		t.Fatalf("a risky pattern must wait for a person, even with CapCommands: %v %+v", err, a)
	}
	// git's global options before the subcommand don't hide a push or reset
	for i, line := range []string{"git -C . push origin main", "git --no-pager push", "git -c x=y reset --hard"} {
		sc.RunRef = fmt.Sprintf("g%d", i)
		if a, err := svc.Propose(ctx, sc, "run_command", line, "x"); err != nil || a.Status != "pending" {
			t.Fatalf("%q must wait for a person: %v %+v", line, err, a)
		}
	}
	sc.RunRef = "r2"
	if a, err := svc.Propose(ctx, sc, "run_command", "go version", "x"); err != nil || a.Status != "done" {
		t.Fatalf("a non-risky allowed command still runs on its own: %v %+v", err, a)
	}
}

type fakeSender struct{}

func (fakeSender) Relay(ctx context.Context, a storage.Action) (string, string, error) {
	return "", a.TargetID, nil
}
func (fakeSender) Relayed(ctx context.Context, conversationID string) bool    { return false }
func (fakeSender) RelayLimit(ctx context.Context, conversationID string) bool { return false }

// TestCheckSendCrossProject: checkSend must reject a target chat that belongs
// to a different project than the one the run is in, even when the target is
// otherwise a valid, un-cleaned conversation (IDOR: a run in project A could
// otherwise inject a message into a chat of project B).
func TestCheckSendCrossProject(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p1, _ := st.Repos().Create(ctx, storage.Repo{Name: "p1", Path: t.TempDir()})
	p2, _ := st.Repos().Create(ctx, storage.Repo{Name: "p2", Path: t.TempDir()})

	own, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p1.ID, Title: "own"})
	other, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p2.ID, Title: "other"})
	job, err := st.Jobs().Create(ctx, storage.Job{ProjectID: p1.ID, Kind: "chat_turn", Origin: "user", Status: "done", CreatedBy: "human:u1", ConversationID: own.ID})
	if err != nil {
		t.Fatal(err)
	}

	svc := New(st, nil)
	svc.SetSender(fakeSender{})
	sc := Scope{ProjectID: p1.ID, ConversationID: own.ID, JobID: job.ID}
	a := &storage.Action{ProjectID: p1.ID, Target: other.ID, Args: storage.ActionArgs{Message: "xin chào"}}
	if err := svc.checkSend(ctx, sc, a); err == nil {
		t.Fatalf("send_message must not reach a chat of another project, got nil error, action=%+v", a)
	}
}

// A project that links another (perm.Policy.LinkedProjects) may send to its
// chats or open a new one there; one it does not link stays out of reach,
// and sending there runs on its own only with chat.cross.
func TestCheckSendLinkedProject(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p1, _ := st.Repos().Create(ctx, storage.Repo{Name: "mockup", Path: t.TempDir()})
	p2, _ := st.Repos().Create(ctx, storage.Repo{Name: "Admin v3", Path: t.TempDir()})
	p3, _ := st.Repos().Create(ctx, storage.Repo{Name: "p3", Path: t.TempDir()})
	if err := perm.SavePolicyIf(ctx, st, p1.ID, "", perm.Policy{LinkedProjects: []string{p2.ID}}); err != nil {
		t.Fatal(err)
	}
	own, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p1.ID, Title: "own"})
	linked, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p2.ID, Title: "admin"})
	unlinked, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p3.ID, Title: "p3"})
	job, _ := st.Jobs().Create(ctx, storage.Job{ProjectID: p1.ID, Kind: "chat_turn", Origin: "user", Status: "done", CreatedBy: "human:u1", ConversationID: own.ID})
	svc := New(st, nil)
	svc.SetSender(fakeSender{})
	sc := Scope{ProjectID: p1.ID, ConversationID: own.ID, JobID: job.ID}

	a := &storage.Action{ProjectID: p1.ID, Target: linked.ID, Args: storage.ActionArgs{Message: "sửa trang admin"}}
	if err := svc.checkSend(ctx, sc, a); err != nil || a.TargetID != linked.ID || a.Args.Project != p2.ID {
		t.Fatalf("linked chat: %v %+v", err, a)
	}
	a = &storage.Action{ProjectID: p1.ID, Args: storage.ActionArgs{Message: "sửa trang admin", Project: "admin V3"}}
	if err := svc.checkSend(ctx, sc, a); err != nil || a.TargetID != "" || a.Args.Project != p2.ID || !strings.Contains(a.Target, "Admin v3") {
		t.Fatalf("new chat by name: %v %+v", err, a)
	}
	a = &storage.Action{ProjectID: p1.ID, Target: unlinked.ID, Args: storage.ActionArgs{Message: "x"}}
	if err := svc.checkSend(ctx, sc, a); err == nil {
		t.Fatal("a chat of a project not linked was accepted")
	}
	a = &storage.Action{ProjectID: p1.ID, Args: storage.ActionArgs{Message: "x", Project: p3.ID}}
	if err := svc.checkSend(ctx, sc, a); err == nil {
		t.Fatal("a new chat in a project not linked was accepted")
	}
	a = &storage.Action{ProjectID: p1.ID, Target: unlinked.ID, Args: storage.ActionArgs{Message: "x", Project: p2.ID}}
	if err := svc.checkSend(ctx, sc, a); err == nil {
		t.Fatal("a chat not of the project named was accepted")
	}

	cross := storage.Action{Kind: "send_message", ProjectID: p1.ID, Args: storage.ActionArgs{Project: p2.ID}}
	same := storage.Action{Kind: "send_message", ProjectID: p1.ID}
	sendOnly := perm.Access{Caps: []string{perm.CapChatSend}}
	if svc.autoAllowed(ctx, cross, sendOnly) || !svc.autoAllowed(ctx, same, sendOnly) {
		t.Fatal("chat.send alone: only the project's own chats run on their own")
	}
	if !svc.autoAllowed(ctx, cross, perm.Access{Caps: []string{perm.CapChatCross}}) {
		t.Fatal("chat.cross: a linked project's chat runs on its own")
	}
}

// newChatSender opens a new chat for a send with no target (Engine.Relay).
type newChatSender struct{ fakeSender }

func (newChatSender) Relay(ctx context.Context, a storage.Action) (string, string, error) {
	return "đã gửi", "cnv_opened", nil
}

// A new chat opened in a linked project is kept on the action (its target),
// so the chat that sent it finds it again (LinkedPending, report back).
func TestSendNewChatKeepsTarget(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p1, _ := st.Repos().Create(ctx, storage.Repo{Name: "mockup", Path: t.TempDir()})
	p2, _ := st.Repos().Create(ctx, storage.Repo{Name: "admin", Path: t.TempDir()})
	perm.SavePolicyIf(ctx, st, p1.ID, "", perm.Policy{LinkedProjects: []string{p2.ID}})
	own, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p1.ID})
	job, _ := st.Jobs().Create(ctx, storage.Job{ProjectID: p1.ID, Kind: "chat_turn", Origin: "user", Status: "running", CreatedBy: "human:u1", ConversationID: own.ID})
	svc := New(st, nil)
	svc.SetSender(newChatSender{})
	sc := Scope{ProjectID: p1.ID, ConversationID: own.ID, JobID: job.ID, Agent: "Lead", Level: perm.Operate, Access: perm.Access{Level: perm.Operate, Caps: []string{perm.CapChatCross}}}
	a, err := svc.Propose(ctx, sc, "send_message", "", "giao việc", storage.ActionArgs{Message: "làm trang mockup", Project: "admin"})
	if err != nil || a.Status != "done" || a.TargetID != "cnv_opened" {
		t.Fatalf("propose = %v %+v", err, a)
	}
	if got, _ := st.Actions().Get(ctx, a.ID); got.TargetID != "cnv_opened" {
		t.Fatalf("stored target = %q", got.TargetID)
	}
	// a copy read before the chat was known never clears it
	stale := a
	stale.TargetID = ""
	st.Actions().Update(ctx, stale)
	if got, _ := st.Actions().Get(ctx, a.ID); got.TargetID != "cnv_opened" {
		t.Fatalf("target cleared by a stale copy: %q", got.TargetID)
	}
}

// What may run on its own (a check) runs at once even while a proposal of its
// chat waits for a person: it changes nothing, so the order is kept.
func TestAutoRunsWhileAProposalWaits(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	test, _ := st.Processes().Create(ctx, storage.Process{ProjectID: proj.ID, Name: "test", Kind: "job", Command: "echo ok"})
	m := ops.NewManager(st, t.TempDir(), nil)
	defer m.Shutdown()
	svc := New(st, m)
	allProcs = []string{test.ID}
	conv, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: proj.ID})
	_, _ = st.Actions().Create(ctx, storage.Action{ProjectID: proj.ID, ConversationID: conv.ID, Kind: "run_command", Target: "env X=1 pnpm typecheck"})
	sc := Scope{ProjectID: proj.ID, ConversationID: conv.ID, RunRef: "r1", Agent: "a", Level: perm.Check, Access: at(perm.Check)}
	if a, _ := svc.Propose(ctx, sc, "run_process", "test", "kiểm tra"); a.Status != "done" {
		t.Fatalf("a check runs on its own: %s", a.Status)
	}
}

// A linked project in another office (ADR-167) is named <office>/<project>
// or by a link to one of its chats; the send carries the office, and runs on
// its own only with chat.cross.
func TestCheckSendPeerProject(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p1, _ := st.Repos().Create(ctx, storage.Repo{Name: "mockup", Path: t.TempDir()})
	st.Settings().Set(ctx, "peers", []peer.Peer{{ID: "peer_b", Name: "office-b", URL: "https://b.example",
		Remote: []peer.RemoteProject{{ID: "rep_shop", Name: "shop"}, {ID: "rep_other", Name: "other"}}}})
	perm.SavePolicyIf(ctx, st, p1.ID, "", perm.Policy{LinkedProjects: []string{"peer:peer_b/rep_shop"}})
	own, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p1.ID})
	job, _ := st.Jobs().Create(ctx, storage.Job{ProjectID: p1.ID, Kind: "chat_turn", Origin: "user", Status: "done", CreatedBy: "human:u1", ConversationID: own.ID})
	svc := New(st, nil)
	svc.SetSender(fakeSender{})
	svc.SetPeers(peer.New(st, nil))
	sc := Scope{ProjectID: p1.ID, ConversationID: own.ID, JobID: job.ID}

	a := &storage.Action{ProjectID: p1.ID, Args: storage.ActionArgs{Message: "làm trang shop", Project: "office-b/shop"}}
	if err := svc.checkSend(ctx, sc, a); err != nil || a.Args.Peer != "peer_b" || a.Args.Project != "rep_shop" || a.TargetID != "" {
		t.Fatalf("new chat there: %v %+v", err, a)
	}
	a = &storage.Action{ProjectID: p1.ID, Target: "https://b.example/projects/rep_shop?tab=chat&c=cnv_9", Args: storage.ActionArgs{Message: "tiếp"}}
	if err := svc.checkSend(ctx, sc, a); err != nil || a.Args.Peer != "peer_b" || a.TargetID != "cnv_9" {
		t.Fatalf("its chat by link: %v %+v", err, a)
	}
	a = &storage.Action{ProjectID: p1.ID, Target: "https://b.example/projects/rep_shop?tab=chat", Args: storage.ActionArgs{Message: "x"}}
	if err := svc.checkSend(ctx, sc, a); err != nil || a.Args.Peer != "peer_b" || a.TargetID != "" {
		t.Fatalf("its project by link (a new chat): %v %+v", err, a)
	}
	a = &storage.Action{ProjectID: p1.ID, Args: storage.ActionArgs{Message: "x", Project: "office-b/other"}}
	if err := svc.checkSend(ctx, sc, a); err == nil {
		t.Fatal("a project of the office not linked was accepted")
	}
	a = &storage.Action{ProjectID: p1.ID, Target: "https://b.example/projects/rep_other?tab=chat&c=cnv_1", Args: storage.ActionArgs{Message: "x"}}
	if err := svc.checkSend(ctx, sc, a); err == nil {
		t.Fatal("a chat of a project not linked was accepted")
	}
	cross := storage.Action{Kind: "send_message", ProjectID: p1.ID, Args: storage.ActionArgs{Project: "rep_shop", Peer: "peer_b"}}
	if svc.autoAllowed(ctx, cross, perm.Access{Caps: []string{perm.CapChatSend}}) || !svc.autoAllowed(ctx, cross, perm.Access{Caps: []string{perm.CapChatCross}}) {
		t.Fatal("another office's project runs on its own only with chat.cross")
	}
}
