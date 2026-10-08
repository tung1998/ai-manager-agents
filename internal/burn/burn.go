// Package burn runs a project's Burn (spec 2026-10-01-burn-design): its main
// agent, on its own and with full access, finds what is unfinished, what
// could be better and what is broken, and does it — a few pieces at a time
// (ADR-117), each in its own worktree and chat — until it is stopped or its time is up. Stopped,
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
	"bitbucket.org/senprints/agent-office/internal/prompts"
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
	wakes map[string]chan struct{}      // by project: its loop looks again now

	notify Notify // a run's summary to a bot's chat (ADR-120); nil: none
}

// Notify posts text to a bot's chat (the channels manager).
type Notify func(ctx context.Context, channelID, chatID, text string) error

// SetNotify sends the runs' summaries to the bots' chats the Burns name.
func (s *Service) SetNotify(n Notify) { s.notify = n }

// New builds a Service; Start runs the Burns that were running.
func New(st storage.Store, engine *chat.Engine, trees *worktree.Manager) *Service {
	return &Service{store: st, chat: engine, trees: trees, idle: 5 * time.Minute, loops: map[string]context.CancelFunc{}, wakes: map[string]chan struct{}{}}
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
	// each piece runs in its own worktree: without git it would edit the folder itself
	if p, err := s.store.Repos().Get(ctx, projectID); err != nil || p.Path == "" || !worktree.IsRepo(ctx, p.Path) {
		return b, errors.New("Burn cần project là một git repo (mỗi việc chạy trong worktree riêng)")
	}
	if err := s.CheckAgent(ctx, b.AgentID); err != nil {
		return b, err
	}
	if err := s.CheckReviewers(ctx, b.ReviewProfileID); err != nil {
		return b, err
	}
	if !b.Active() {
		b.ConversationID = "" // each start, a chat of its own: the earlier ones stay as they were
	}
	if err := s.ensureConversation(ctx, &b); err != nil {
		return b, err
	}
	now := time.Now().UTC()
	b.State, b.StartedBy, b.StartedAt, b.WaitingUntil = "running", who, &now, nil
	if b, err = s.store.Burn().SaveSession(ctx, b); err != nil {
		return b, err
	}
	s.spawn(projectID)
	return b, nil
}

// CheckAgent refuses an agent that is paused: a Burn with it would only get
// the "agent is off" notice.
func (s *Service) CheckAgent(ctx context.Context, agentID string) error {
	if agentID == "" {
		return nil // the first lead that is on
	}
	ag, err := s.store.Agents().Get(ctx, agentID)
	if err != nil {
		return errors.New("không tìm thấy agent của Burn: hãy chọn agent khác")
	}
	if ag.Disabled {
		return fmt.Errorf("agent %s đang tắt: chọn agent khác cho Burn hoặc bật lại agent này", ag.Name)
	}
	return nil
}

// ensureConversation gives the Burn a chat with its agent: its own while the
// agent is the same, a new one once the agent was changed (the old one stays
// with the agent it had, which may be paused now) or the Burn started again.
func (s *Service) ensureConversation(ctx context.Context, b *storage.BurnSession) error {
	if b.ConversationID != "" {
		c, err := s.store.Chat().GetConversation(ctx, b.ConversationID)
		if err == nil && (b.AgentID == "" || c.AgentID == b.AgentID) && c.Cleaned == "" { // cleaned: it takes no more
			return nil
		}
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
	}
	conv, err := s.chat.StartConversationPurpose(ctx, b.ProjectID, b.AgentID, "burn")
	if err != nil {
		return err
	}
	conv.Title = "Burn " + time.Now().Format("02/01 15:04")
	_ = s.store.Chat().UpdateConversation(ctx, conv)
	// saved before SetMode: a failure there must not lose the conversation just made
	b.ConversationID = conv.ID
	if saved, err := s.store.Burn().SaveSession(ctx, *b); err != nil {
		return err
	} else {
		*b = saved
	}
	return s.chat.SetMode(ctx, conv.ID, "operate")
}

// Stop stops a project's Burn: the answer running now is cancelled, its
// piece of work waits.
func (s *Service) Stop(ctx context.Context, projectID string) error {
	return s.stop(ctx, projectID, "dừng hẳn")
}

// stop is Stop saying why, in the run's summary (ADR-120).
func (s *Service) stop(ctx context.Context, projectID, why string) error {
	b, err := s.store.Burn().Session(ctx, projectID)
	if err != nil {
		return err
	}
	ran := b.Active()
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
	if items, err := s.store.Burn().Items(ctx, b.ID); err == nil {
		for _, it := range items {
			if it.Status == "doing" && it.WorkConversationID != "" {
				s.chat.StopAll(it.WorkConversationID)
			}
		}
	}
	s.pauseDoing(ctx, b.ID)
	if ran {
		s.report(context.WithoutCancel(ctx), b, why)
	}
	return nil
}

// Drain lets a project's Burn finish what it has in progress (the pieces
// being done, paused or waiting for their result's review), then it stops: no
// scan, no new piece. The scan running now is cancelled.
func (s *Service) Drain(ctx context.Context, projectID string) error {
	b, err := s.store.Burn().Session(ctx, projectID)
	if err != nil {
		return err
	}
	if !b.Active() {
		return errors.New("Burn không chạy")
	}
	if b.State == "draining" {
		return nil
	}
	b.State = "draining" // a limit's wait (WaitingUntil) still holds
	if _, err := s.store.Burn().SaveSession(ctx, b); err != nil {
		return err
	}
	if b.ConversationID != "" {
		s.chat.StopAll(b.ConversationID)
	}
	s.spawn(projectID) // its loop ends it once nothing is left
	s.wake(projectID)
	return nil
}

// Resume takes a draining Burn back to work as before.
func (s *Service) Resume(ctx context.Context, projectID string) error {
	b, err := s.store.Burn().Session(ctx, projectID)
	if err != nil {
		return err
	}
	if b.State != "draining" {
		return errors.New("Burn không ở trạng thái đang hoàn thành nốt")
	}
	b.State = "running"
	if b.WaitingUntil != nil && b.WaitingUntil.After(time.Now()) {
		b.State = "waiting_limit"
	}
	if _, err := s.store.Burn().SaveSession(ctx, b); err != nil {
		return err
	}
	s.spawn(projectID)
	s.wake(projectID)
	return nil
}

// wake has a project's loop look again now (it may be waiting a long while).
func (s *Service) wake(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.wakes[projectID] <- struct{}{}:
	default:
	}
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
	wake := make(chan struct{}, 1)
	s.loops[projectID], s.wakes[projectID] = cancel, wake
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.loops, projectID)
			if s.wakes[projectID] == wake {
				delete(s.wakes, projectID)
			}
			s.mu.Unlock()
			cancel()
		}()
		s.loop(ctx, projectID, wake)
	}()
}

// loop: the paused and chosen pieces first, up to its cap at once (ADR-117),
// each in its own worktree and chat; a slot free and nothing to go on with,
// the main agent looks for work and chooses; with nothing to do, it waits a
// while. Draining, it only finishes what is in progress, then stops.
// wake: a worker is done, or the Burn was drained or resumed.
func (s *Service) loop(ctx context.Context, projectID string, wake chan struct{}) {
	var (
		mu   sync.Mutex
		busy = map[string]bool{} // the pieces a worker has
		wg   sync.WaitGroup
	)
	defer wg.Wait()
	empty := 0 // scans in a row that found nothing
	for ctx.Err() == nil {
		b, err := s.store.Burn().Session(ctx, projectID)
		if err != nil || !b.Active() {
			return
		}
		now := time.Now()
		if b.EndsAt != nil && !b.EndsAt.After(now) {
			_ = s.stop(context.WithoutCancel(ctx), projectID, "tới giờ tắt") // its time is up
			return
		}
		if b.WaitingUntil != nil || b.State == "waiting_limit" {
			if b.WaitingUntil != nil && b.WaitingUntil.After(now) {
				wait(ctx, wake, min(time.Until(*b.WaitingUntil), time.Minute))
				continue
			}
			if b.State == "waiting_limit" {
				b.State = "running"
			}
			b.WaitingUntil = nil
			b, _ = s.store.Burn().SaveSession(ctx, b)
		}
		draining := b.State == "draining"
		if conv := b.ConversationID; s.ensureConversation(ctx, &b) == nil && b.ConversationID != conv { // its agent was changed while it ran
			b, _ = s.store.Burn().SaveSession(ctx, b)
		}
		items, _ := s.store.Burn().Items(ctx, b.ID)
		mu.Lock()
		free := max(b.MaxParallel, 1) - len(busy)
		for ; free > 0; free-- {
			it, ok := next(items, busy, draining)
			if !ok {
				break
			}
			busy[it.ID] = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.step(ctx, b, it)
				mu.Lock()
				delete(busy, it.ID)
				mu.Unlock()
				select {
				case wake <- struct{}{}:
				default:
				}
			}()
		}
		working := len(busy)
		mu.Unlock()
		if draining {
			if working == 0 && s.drained(ctx, b.ID) {
				return // all it had is finished
			}
			wait(ctx, wake, time.Minute)
			continue
		}
		if free <= 0 { // every slot taken: wait for one
			wait(ctx, wake, time.Minute)
			continue
		}
		before := len(items)
		if err := s.plan(ctx, b, items, empty, free); err != nil && ctx.Err() == nil {
			if cur, err := s.store.Burn().SessionByID(ctx, b.ID); err == nil && cur.State != "running" {
				continue // drained or stopped meanwhile: the scan was cancelled
			}
			slog.Warn("burn: plan", "project", projectID, "err", err)
			if !s.waitLimit(ctx, b) {
				wait(ctx, wake, time.Minute)
			}
			continue
		}
		after, _ := s.store.Burn().Items(ctx, b.ID)
		mu.Lock()
		_, chosen := next(after, busy, false)
		mu.Unlock()
		switch {
		case chosen || len(after) != before:
			empty = 0
		default: // nothing new, nothing chosen: wait longer each time (a piece done wakes it)
			empty++
			wait(ctx, wake, s.idleAfter(empty))
		}
	}
}

// drained ends a draining Burn with nothing left in progress; false when it
// was resumed meanwhile.
func (s *Service) drained(ctx context.Context, sessionID string) bool {
	b, err := s.store.Burn().SessionByID(ctx, sessionID)
	if err != nil || b.State != "draining" {
		return err != nil
	}
	b.State, b.WaitingUntil = "stopped", nil
	_, _ = s.store.Burn().SaveSession(ctx, b)
	s.report(context.WithoutCancel(ctx), b, "đã làm nốt việc dở")
	return true
}

// step takes a piece one step on: its review once done, the reviews before it
// is done, then its work.
func (s *Service) step(ctx context.Context, b storage.BurnSession, it storage.BurnItem) {
	if it.Status == "review" { // done: its result waits for the reviewer (ADR-112)
		if s.finish(ctx, b, it) && !s.waitLimit(ctx, b) {
			sleep(ctx, time.Minute) // the review could not run: ask again in a while
		}
		return
	}
	if proceed, failed := s.gate(ctx, b, it); !proceed { // reviewed before it is done
		if failed && !s.waitLimit(ctx, b) {
			sleep(ctx, time.Minute)
		}
		return
	}
	if failed := s.work(ctx, b, it); failed {
		s.waitLimit(ctx, b) // the connection's limit: it waits for the reset
	}
}

// idleAfter is the wait after n empty scans in a row: idle, then doubling, at most an hour.
func (s *Service) idleAfter(n int) time.Duration {
	d := s.idle
	for i := 1; i < n && d < time.Hour; i++ {
		d *= 2
	}
	return min(d, time.Hour)
}

// next: a piece done waiting for its review, a paused one (it goes on),
// else the one chosen first — none a worker has already; draining, only the
// pieces in progress.
func next(items []storage.BurnItem, busy map[string]bool, draining bool) (storage.BurnItem, bool) {
	order := []string{"review", "paused", "doing", "queued"}
	if draining {
		order = order[:3]
	}
	for _, st := range order {
		for _, it := range items {
			if it.Status == st && !busy[it.ID] {
				return it, true
			}
		}
	}
	return storage.BurnItem{}, false
}

// wait is sleep that ends early when woken.
func wait(ctx context.Context, wake <-chan struct{}, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-wake:
	case <-time.After(d):
	}
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
	text      string // the answer
}

// run sends text in one of the Burn's conversations and waits for the answer
// (a person chatting there now: it waits its turn).
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
			case e.Type == "done" && e.Message != nil:
				res.text = e.Message.Content
				if e.Message.CostUSD != nil {
					res.cost += *e.Message.CostUSD
				}
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
	again := it.Status == "paused" || it.Status == "doing" || it.Worktree != "" // a retry goes on from what is there
	tree := "burn-" + it.ID
	it.Status, it.Attempts = "doing", it.Attempts+1
	if it.Branch == "" {
		it.Branch = branchName(it)
	}
	// its worktree first: a run without one would write in the project's own folder
	p, perr := s.store.Repos().Get(ctx, b.ProjectID)
	if perr == nil && s.trees != nil {
		it.Worktree, perr = s.trees.Ensure(ctx, p.Path, b.ProjectID, tree, nil)
	} else if perr == nil {
		perr = errors.New("office không có worktree")
	}
	if perr != nil {
		it.Status, it.Summary = "failed", "không tạo được worktree: "+perr.Error()
		_ = s.store.Burn().UpdateItem(ctx, it)
		return false
	}
	// its own chat (ADR-116): what other pieces did does not crowd its context
	if perr = s.ensureWorkConversation(ctx, b, &it); perr != nil {
		it.Status, it.Summary = s.failedOrAgain(it, "không tạo được chat làm việc: "+perr.Error())
		_ = s.store.Burn().UpdateItem(ctx, it)
		return false
	}
	_ = s.store.Burn().UpdateItem(ctx, it)
	// held back for its review (ADR-112): no diff until the reviewer agrees
	held := s.reviews(ctx, b, "result")
	res, err := s.run(runCtx(ctx, b, tree, b.ResultMode != "patch" || held), it.WorkConversationID, workPrompt(b, it, again, held))
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
	case cur.Status == "review": // the reviewer next, in the loop
	case cur.Status == "done" && held: // review turned on meanwhile
		cur.Status = "review"
	case cur.Status == "done":
		s.deliver(context.WithoutCancel(ctx), b, &cur)
	case cur.Status == "doing": // it said nothing of how it went
		cur.Status, cur.Summary = s.failedOrAgain(cur, "agent không báo kết quả (burn_done/burn_fail)")
	}
	_ = s.store.Burn().UpdateItem(context.WithoutCancel(ctx), cur)
	return failed
}

// ensureWorkConversation gives a piece a hidden chat of its own with the
// Burn's agent (ADR-116), kept after it is done (a retry goes on there, the
// person reads it from the Burn); a new one once the agent was changed.
func (s *Service) ensureWorkConversation(ctx context.Context, b storage.BurnSession, it *storage.BurnItem) error {
	if id := it.WorkConversationID; id != "" {
		c, err := s.store.Chat().GetConversation(ctx, id)
		if err == nil && (b.AgentID == "" || c.AgentID == b.AgentID) && c.Cleaned == "" {
			return nil
		}
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
	}
	conv, err := s.chat.StartConversationPurpose(ctx, b.ProjectID, b.AgentID, chat.BurnWorkPurpose)
	if err != nil {
		return err
	}
	conv.Title = oneLine("Burn: "+it.Title, 80)
	_ = s.store.Chat().UpdateConversation(ctx, conv)
	it.WorkConversationID = conv.ID
	return s.chat.SetMode(ctx, conv.ID, "operate")
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

// plan: the main agent looks for work and chooses what is next, up to free
// pieces (in a scratch worktree: what it tries there is thrown away).
func (s *Service) plan(ctx context.Context, b storage.BurnSession, items []storage.BurnItem, empty, free int) error {
	res, err := s.run(runCtx(ctx, b, "burn-scan-"+b.ID, true), b.ConversationID, planPrompt(b, items, empty, free))
	if err == nil && res.failed != "" {
		err = errors.New(res.failed)
	}
	if area := scannedOf(res.text); area != "" { // kept off the chat: it is compacted, or the agent changed
		_ = s.store.Burn().SetScanned(context.WithoutCancel(ctx), b.ID, addScanned(b.Scanned, area, time.Now()))
	}
	return err
}

// scannedMark starts the line of a scan's answer that says what it looked at
// (scannedMarks: also the one older answers used).
const scannedMark = "AREAS SCANNED:"

var scannedMarks = []string{scannedMark, "VÙNG ĐÃ XEM:"}

// maxScanned caps what is kept of the areas scanned (the latest stay).
const maxScanned = 2000

// scannedOf is what a scan said it looked at.
func scannedOf(text string) string {
	for _, line := range strings.Split(text, "\n") {
		l := []rune(strings.TrimLeft(strings.TrimSpace(line), "*-_# "))
		for _, mark := range scannedMarks {
			m := len([]rune(mark))
			if len(l) > m && strings.EqualFold(string(l[:m]), mark) {
				return oneLine(strings.Trim(string(l[m:]), "*_ "), 400)
			}
		}
	}
	return ""
}

// addScanned adds a scan's areas to what was kept, dropping the oldest lines
// past maxScanned.
func addScanned(kept, area string, at time.Time) string {
	lines := append(strings.Split(strings.TrimSpace(kept), "\n"), at.Local().Format("02/01 15:04")+" "+area)
	for len(lines) > 1 && (lines[0] == "" || len([]rune(strings.Join(lines, "\n"))) > maxScanned) {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// What the plan prompt carries inline (ADR-121): the open pieces in full, the
// latest closed ones and scanned areas only; the rest is a burn_list call away.
const (
	maxClosedInPlan  = 5
	maxScannedInPlan = 5
)

// planPrompt is the coordination turn (burn/plan.md): what is open, closed
// and scanned, and how many pieces to pick.
func planPrompt(b storage.BurnSession, items []storage.BurnItem, empty, free int) string {
	type piece struct{ ID, Kind, Status, Title, Summary string }
	// said outright: seeing pieces "doing", an agent took the slots for full (2026-10-08)
	total := max(b.MaxParallel, 1, free)
	var open, closed []piece
	for _, it := range items {
		switch it.Status {
		case "done", "skipped", "failed":
			closed = append(closed, piece{Status: it.Status, Title: oneLine(it.Title, 100)})
			continue
		}
		p := piece{ID: it.ID, Kind: it.Kind, Status: it.Status, Title: it.Title}
		if it.Summary != "" {
			p.Summary = oneLine(it.Summary, 120)
		}
		open = append(open, p)
	}
	closedCount := len(closed)
	if closedCount > maxClosedInPlan {
		closed = closed[closedCount-maxClosedInPlan:]
	}
	var scanned []string
	if b.Scanned != "" {
		scanned = strings.Split(strings.TrimSpace(b.Scanned), "\n")
	}
	scannedMore := len(scanned) > maxScannedInPlan
	if scannedMore {
		scanned = scanned[len(scanned)-maxScannedInPlan:]
	}
	return prompts.Render("burn/plan", struct {
		Focus              string
		FocusLooks         []string
		Total, Taken, Free int
		Open, Closed       []piece
		ClosedCount        int
		ClosedMore         bool
		Scanned            []string
		ScannedMore        bool
		Empty              int
		Order, ScannedMark string
	}{b.Focus, focusLooks(b.Focus), total, total - free, free, open, closed, closedCount, closedCount > maxClosedInPlan,
		scanned, scannedMore, empty, b.Order, scannedMark})
}

// workPrompt is a piece's work turn (burn/work.md).
func workPrompt(b storage.BurnSession, it storage.BurnItem, again, reviewed bool) string {
	return prompts.Render("burn/work", struct {
		ID, Kind, Title, Detail, Focus, ReviewNote, Branch string
		FocusChecks                                        []string
		Again, Reviewed, Patch                             bool
	}{it.ID, it.Kind, it.Title, it.Detail, b.Focus, it.ReviewNote, it.Branch, focusChecks(b.Focus), again, reviewed, b.ResultMode == "patch"})
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
	// as it is now: pieces run side by side, b may be a while old
	if cur, err := s.store.Burn().SessionByID(ctx, b.ID); err == nil {
		if !cur.Active() {
			return true
		}
		b = cur
	}
	if b.EndsAt != nil && !until.Before(*b.EndsAt) {
		_ = s.stop(context.WithoutCancel(ctx), b.ProjectID, "chạm giới hạn kết nối AI, reset sau giờ tắt")
		return true
	}
	if b.State != "draining" { // draining: it still finishes, after the reset
		b.State = "waiting_limit"
	}
	b.WaitingUntil = &until
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
