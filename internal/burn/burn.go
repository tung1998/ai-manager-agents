// Package burn runs a project's Burn (spec 2026-10-01-burn-design): its main
// agent, on its own and with full access, finds what is unfinished, what
// could be better and what is broken, and does it — one piece at a time,
// each in its own worktree — until it is stopped or its time is up. Stopped,
// the piece in progress waits (its worktree kept); started again, it goes on.
package burn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/worktree"
)

// Kinds of work a Burn looks for.
var Kinds = map[string]bool{"unfinished": true, "upgrade": true, "bug": true}

// Service runs the projects' Burns.
type Service struct {
	store storage.Store
	chat  *chat.Engine
	trees *worktree.Manager
	idle  time.Duration // nothing to do: how long before looking again

	mu    sync.Mutex
	root  context.Context
	loops map[string]context.CancelFunc // by project
}

// New builds a Service; Start runs the Burns that were running.
func New(st storage.Store, engine *chat.Engine, trees *worktree.Manager) *Service {
	return &Service{store: st, chat: engine, trees: trees, idle: 5 * time.Minute, loops: map[string]context.CancelFunc{}}
}

// SetIdle is how long a Burn with nothing to do waits before looking again.
func (s *Service) SetIdle(d time.Duration) { s.idle = d }

// Start goes on with the Burns that ran when office stopped: their piece in
// progress waits, then goes on first.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.root = ctx
	s.mu.Unlock()
	running, err := s.store.Burn().Running(ctx)
	if err != nil {
		return
	}
	for _, b := range running {
		s.pauseDoing(ctx, b.ID)
		s.spawn(b.ProjectID)
	}
}

// Begin starts a project's Burn (settings as saved); who is the admin who
// confirmed it.
func (s *Service) Begin(ctx context.Context, projectID, who string) (storage.BurnSession, error) {
	b, err := s.store.Burn().Session(ctx, projectID)
	if err != nil {
		return b, err
	}
	if b.EndsAt != nil && !b.EndsAt.After(time.Now()) {
		return b, errors.New("giờ tắt đã qua: hãy đặt giờ tắt mới")
	}
	if b.ConversationID == "" {
		conv, err := s.chat.StartConversationPurpose(ctx, projectID, b.AgentID, "burn")
		if err != nil {
			return b, err
		}
		conv.Title = "Burn"
		_ = s.store.Chat().UpdateConversation(ctx, conv)
		b.ConversationID = conv.ID
		if err := s.chat.SetMode(ctx, conv.ID, "operate"); err != nil {
			return b, err
		}
	}
	now := time.Now().UTC()
	b.State, b.StartedBy, b.StartedAt, b.WaitingUntil = "running", who, &now, nil
	if b, err = s.store.Burn().SaveSession(ctx, b); err != nil {
		return b, err
	}
	s.spawn(projectID)
	return b, nil
}

// Stop stops a project's Burn: the answer running now is cancelled, its
// piece of work waits.
func (s *Service) Stop(ctx context.Context, projectID string) error {
	b, err := s.store.Burn().Session(ctx, projectID)
	if err != nil {
		return err
	}
	b.State, b.WaitingUntil = "stopped", nil
	if _, err := s.store.Burn().SaveSession(ctx, b); err != nil {
		return err
	}
	s.mu.Lock()
	if cancel, ok := s.loops[projectID]; ok {
		cancel()
		delete(s.loops, projectID)
	}
	s.mu.Unlock()
	if b.ConversationID != "" {
		s.chat.StopAll(b.ConversationID)
	}
	s.pauseDoing(ctx, b.ID)
	return nil
}

func (s *Service) pauseDoing(ctx context.Context, sessionID string) {
	items, _ := s.store.Burn().Items(ctx, sessionID)
	for _, it := range items {
		if it.Status == "doing" {
			it.Status = "paused"
			_ = s.store.Burn().UpdateItem(ctx, it)
		}
	}
}

func (s *Service) spawn(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.loops[projectID]; ok || s.root == nil {
		return
	}
	ctx, cancel := context.WithCancel(s.root)
	s.loops[projectID] = cancel
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.loops, projectID)
			s.mu.Unlock()
			cancel()
		}()
		s.loop(ctx, projectID)
	}()
}

// loop: the paused and chosen pieces first, each in its own worktree; with
// none, the main agent looks for work and chooses; with nothing to do, it
// waits a while.
func (s *Service) loop(ctx context.Context, projectID string) {
	for ctx.Err() == nil {
		b, err := s.store.Burn().Session(ctx, projectID)
		if err != nil || (b.State != "running" && b.State != "waiting_limit") {
			return
		}
		now := time.Now()
		if b.EndsAt != nil && !b.EndsAt.After(now) {
			_ = s.Stop(context.WithoutCancel(ctx), projectID) // its time is up
			return
		}
		if b.State == "waiting_limit" {
			if b.WaitingUntil != nil && b.WaitingUntil.After(now) {
				sleep(ctx, min(time.Until(*b.WaitingUntil), time.Minute))
				continue
			}
			b.State, b.WaitingUntil = "running", nil
			b, _ = s.store.Burn().SaveSession(ctx, b)
		}
		items, _ := s.store.Burn().Items(ctx, b.ID)
		if it, ok := next(items); ok {
			if failed := s.work(ctx, b, it); failed && s.waitLimit(ctx, b) {
				continue // the connection's limit: it waits for the reset
			}
			continue
		}
		before := len(items)
		if err := s.plan(ctx, b, items); err != nil && ctx.Err() == nil {
			slog.Warn("burn: plan", "project", projectID, "err", err)
			if !s.waitLimit(ctx, b) {
				sleep(ctx, time.Minute)
			}
			continue
		}
		after, _ := s.store.Burn().Items(ctx, b.ID)
		if _, ok := next(after); !ok && len(after) == before {
			sleep(ctx, s.idle) // nothing new, nothing chosen
		}
	}
}

// next: a paused piece (it goes on), else the one chosen first.
func next(items []storage.BurnItem) (storage.BurnItem, bool) {
	for _, st := range []string{"paused", "doing", "queued"} {
		for _, it := range items {
			if it.Status == st {
				return it, true
			}
		}
	}
	return storage.BurnItem{}, false
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// runCtx is how a Burn's turns run: the machine, its model, no time limit.
func runCtx(ctx context.Context, b storage.BurnSession, tree string, noPatch bool) context.Context {
	ctx = actor.With(ctx, "burn:"+b.StartedBy)
	ctx = chat.WithFullAccess(chat.WithModelTier(chat.WithTurnTimeout(ctx, 0), b.ModelTier))
	return chat.WithTree(ctx, tree, noPatch)
}

// turnResult is what a turn did.
type turnResult struct {
	subagents int
	cost      float64
	failed    string
}

// run sends text in the Burn's conversation and waits for the answer (a
// person chatting there now: it waits its turn).
func (s *Service) run(ctx context.Context, conversationID, text string) (turnResult, error) {
	var res turnResult
	var turn *chat.Turn
	for {
		t, _, err := s.chat.Send(ctx, conversationID, text, nil)
		if errors.Is(err, chat.ErrBusy) {
			sleep(ctx, 10*time.Second)
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			continue
		}
		if err != nil {
			return res, err
		}
		turn = t
		break
	}
	for seq := 0; ; {
		evs, done, wake := turn.Since(seq)
		seq += len(evs)
		for _, e := range evs {
			switch {
			case e.Type == "tool" && e.Tool != nil && (e.Tool.Name == "Task" || e.Tool.Name == "Agent"):
				res.subagents++
			case e.Type == "error":
				res.failed = e.Text
			case e.Type == "done" && e.Message != nil && e.Message.CostUSD != nil:
				res.cost += *e.Message.CostUSD
			}
		}
		if done {
			return res, nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			turn.Cancel()
			return res, ctx.Err()
		}
	}
}

// work runs one piece in its own worktree; reported done, it is committed
// to its branch (branch mode) — a diff to approve otherwise.
func (s *Service) work(ctx context.Context, b storage.BurnSession, it storage.BurnItem) (failed bool) {
	again := it.Status == "paused" || it.Status == "doing"
	tree := "burn-" + it.ID
	it.Status, it.Attempts = "doing", it.Attempts+1
	if it.Branch == "" {
		it.Branch = branchName(it)
	}
	if s.trees != nil {
		it.Worktree = s.trees.Path(b.ProjectID, tree)
	}
	_ = s.store.Burn().UpdateItem(ctx, it)
	res, err := s.run(runCtx(ctx, b, tree, b.ResultMode != "patch"), b.ConversationID, workPrompt(b, it, again))
	cur, gerr := s.store.Burn().Item(context.WithoutCancel(ctx), it.ID)
	if gerr != nil {
		return false
	}
	cur.Subagents += res.subagents
	cur.CostUSD += res.cost
	switch {
	case ctx.Err() != nil: // stopped: it waits
		if cur.Status == "doing" {
			cur.Status = "paused"
		}
	case err != nil || res.failed != "":
		failed = true
		if _, hit := s.limitHit(ctx, b.AgentID); hit { // not its fault: it goes on after the reset
			cur.Status, cur.Attempts = "paused", max(cur.Attempts-1, 0)
			break
		}
		cur.Status, cur.Summary = s.failedOrAgain(cur, firstNonEmpty(res.failed, errText(err)))
	case cur.Status == "done":
		if b.ResultMode != "patch" {
			if cerr := commit(context.WithoutCancel(ctx), cur.Worktree, cur.Branch, cur.Title, cur.Summary); cerr != nil {
				cur.Summary = strings.TrimSpace(cur.Summary + "\n(không commit được: " + cerr.Error() + ")")
			}
		}
	case cur.Status == "doing": // it said nothing of how it went
		cur.Status, cur.Summary = s.failedOrAgain(cur, "agent không báo kết quả (burn_done/burn_fail)")
	}
	_ = s.store.Burn().UpdateItem(context.WithoutCancel(ctx), cur)
	return failed
}

// failedOrAgain: a second try, then failed.
func (s *Service) failedOrAgain(it storage.BurnItem, why string) (string, string) {
	if it.Attempts < 2 {
		return "queued", why
	}
	return "failed", why
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// plan: the main agent looks for work and chooses what is next (in a
// scratch worktree: what it tries there is thrown away).
func (s *Service) plan(ctx context.Context, b storage.BurnSession, items []storage.BurnItem) error {
	res, err := s.run(runCtx(ctx, b, "burn-scan-"+b.ID, true), b.ConversationID, planPrompt(b, items))
	if err == nil && res.failed != "" {
		err = errors.New(res.failed)
	}
	return err
}

func planPrompt(b storage.BurnSession, items []storage.BurnItem) string {
	var sb strings.Builder
	sb.WriteString("[Burn] Lượt điều phối. Bạn đang chạy Burn cho project này: tự tìm và làm việc, với toàn quyền.\n")
	if b.Focus != "" {
		fmt.Fprintf(&sb, "Trọng tâm người dùng dặn: %s\n", b.Focus)
	}
	sb.WriteString("\nViệc đã có:\n")
	if len(items) == 0 {
		sb.WriteString("(chưa có)\n")
	}
	for _, it := range items {
		fmt.Fprintf(&sb, "- %s [%s, %s] %s", it.ID, it.Kind, it.Status, it.Title)
		if it.Summary != "" {
			fmt.Fprintf(&sb, " — %s", oneLine(it.Summary, 160))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(`
Việc của lượt này:
1. Nếu còn ít việc "found", QUÉT project tìm thêm: việc dang dở (TODO/FIXME, nhánh làm dở, test/build đang fail, đề xuất còn treo trong các chat — dùng search_history), chỗ nâng cấp đáng làm, lỗi chi tiết (đọc code, chạy test, xem log). Ghi từng việc bằng burn_add (tiêu đề ngắn, kind unfinished|upgrade|bug, chi tiết đủ để làm). Không ghi trùng việc đã có.
2. Chọn ĐÚNG MỘT việc đáng làm nhất bằng burn_pick; việc không đáng làm thì burn_skip kèm lý do.
3. Không sửa code ở lượt này (worktree của lượt này bị bỏ). Không còn gì đáng làm thì nói ngắn "hết việc".
Trả lời ngắn: tìm được gì, chọn việc nào và vì sao.`)
	return sb.String()
}

func workPrompt(b storage.BurnSession, it storage.BurnItem, again bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Burn] Làm việc %s (%s): %s\n", it.ID, it.Kind, it.Title)
	if it.Detail != "" {
		fmt.Fprintf(&sb, "Chi tiết: %s\n", it.Detail)
	}
	if again {
		sb.WriteString("Đây là việc đang làm dở: xem git status / git diff trong worktree này để biết đã làm tới đâu, rồi làm tiếp.\n")
	}
	if b.Focus != "" {
		fmt.Fprintf(&sb, "Trọng tâm người dùng dặn: %s\n", b.Focus)
	}
	fmt.Fprintf(&sb, "\nBạn làm trong worktree riêng của việc này, có toàn quyền. Được dùng tối đa %d subagent (công cụ Agent/Task) cho phần chạy song song; ", b.MaxSubagents)
	if b.MaxSubagents == 0 {
		sb.WriteString("lần này không dùng subagent; ")
	}
	sb.WriteString("không push, không merge. Sửa xong thì chạy build/test liên quan cho tới khi đạt.\n")
	if b.ResultMode == "patch" {
		sb.WriteString("Thay đổi trong worktree sẽ thành một diff chờ người dùng duyệt.\n")
	} else {
		sb.WriteString("Office sẽ commit mọi thay đổi trong worktree lên nhánh " + it.Branch + " khi bạn báo xong.\n")
	}
	fmt.Fprintf(&sb, "Kết thúc bằng burn_done(item=%q, summary=tóm tắt đã làm gì và kiểm chứng ra sao) hoặc burn_fail(item=%q, reason=…) nếu không làm được.", it.ID, it.ID)
	return sb.String()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// branchName: burn/<short id>-<title, plain>.
func branchName(it storage.BurnItem) string {
	id := strings.TrimPrefix(it.ID, "bit_")
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	if slug := slugOf(it.Title, 40); slug != "" {
		return "burn/" + id + "-" + slug
	}
	return "burn/" + id
}

func slugOf(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "đ", "d"), "Đ", "d")
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= n {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// commit puts what the piece changed on its branch, in its worktree (a
// branch of the project's repository: the person merges it, office never).
func commit(ctx context.Context, dir, branch, title, summary string) error {
	if dir == "" {
		return errors.New("không có worktree")
	}
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if cur, _ := git("rev-parse", "--abbrev-ref", "HEAD"); cur != branch {
		if _, err := git("switch", branch); err != nil {
			if out, err := git("switch", "-c", branch); err != nil {
				return fmt.Errorf("tạo nhánh %s: %s", branch, out)
			}
		}
	}
	if _, err := git("add", "-A"); err != nil {
		return err
	}
	if out, _ := git("status", "--porcelain"); out == "" {
		return nil // nothing (still) uncommitted: the agent may have committed itself
	}
	msg := "burn: " + title
	if summary != "" {
		msg += "\n\n" + summary
	}
	if _, err := git("commit", "-m", msg); err != nil {
		if out, err := git("-c", "user.name=Agent Office", "-c", "user.email=office@localhost", "commit", "-m", msg); err != nil {
			return fmt.Errorf("commit: %s", out)
		}
	}
	return nil
}

// provider is the AI connection the agent answers with: its own, else the
// office's default.
func (s *Service) provider(ctx context.Context, agentID string) string {
	if ag, err := s.store.Agents().Get(ctx, agentID); err == nil && ag.ProviderID != "" {
		return ag.ProviderID
	}
	if ps, err := s.store.Providers().List(ctx); err == nil {
		for _, p := range ps {
			if p.IsDefault {
				return p.ID
			}
		}
	}
	return ""
}

func (s *Service) limits(ctx context.Context, agentID string) (chat.Limits, bool) {
	var l chat.Limits
	id := s.provider(ctx, agentID)
	if id == "" {
		return l, false
	}
	ok, _ := s.store.Settings().Get(ctx, chat.LimitsKey(id), &l)
	return l, ok
}

// WeeklyReset is when the agent's AI connection's weekly limit resets next
// (the suggested time a Burn stops), if it said.
func (s *Service) WeeklyReset(ctx context.Context, agentID string) (time.Time, bool) {
	l, ok := s.limits(ctx, agentID)
	if !ok {
		return time.Time{}, false
	}
	w, ok := l.Windows["seven_day"]
	if !ok || !w.ResetsAt.After(time.Now()) {
		return time.Time{}, false
	}
	return w.ResetsAt, true
}

// limitHit: the AI connection said no more for now — until when.
func (s *Service) limitHit(ctx context.Context, agentID string) (time.Time, bool) {
	l, ok := s.limits(ctx, agentID)
	if !ok || l.Status != "rejected" {
		return time.Time{}, false
	}
	var until time.Time
	for _, w := range l.Windows {
		if w.Utilization >= 1 && w.ResetsAt.After(until) {
			until = w.ResetsAt
		}
	}
	if until.IsZero() {
		until = time.Now().Add(30 * time.Minute)
	}
	return until, true
}

// waitLimit puts the Burn to wait for its connection's reset (past its stop
// time: it stops).
func (s *Service) waitLimit(ctx context.Context, b storage.BurnSession) bool {
	until, hit := s.limitHit(ctx, b.AgentID)
	if !hit {
		return false
	}
	if b.EndsAt != nil && !until.Before(*b.EndsAt) {
		_ = s.Stop(context.WithoutCancel(ctx), b.ProjectID)
		return true
	}
	b.State, b.WaitingUntil = "waiting_limit", &until
	_, _ = s.store.Burn().SaveSession(ctx, b)
	return true
}

// DropWorktree removes a piece's worktree (its branch stays: merged, or not).
func (s *Service) DropWorktree(ctx context.Context, it storage.BurnItem) error {
	b, err := s.store.Burn().SessionByID(ctx, it.SessionID)
	if err != nil {
		return err
	}
	p, err := s.store.Repos().Get(ctx, b.ProjectID)
	if err != nil {
		return err
	}
	if s.trees == nil {
		return errors.New("không có worktree")
	}
	return s.trees.Remove(ctx, p.Path, b.ProjectID, "burn-"+it.ID)
}
