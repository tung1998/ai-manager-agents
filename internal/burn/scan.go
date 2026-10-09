package burn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A Burn's scan (ADR-130) is a piece not claimed (no kind): in its own chat
// and worktree it follows a set process (orient by the code map, find,
// challenge, record) and only records pieces (burn_add, status found) with a
// brief a worker starts from; it changes no code. It ends with
// burn_scan_done, which keeps the areas it looked at and its code map and
// drops the scan. Workers then take the pieces from found, quests first,
// each its own piece (burn/work.md). One scan at a time, each recording up
// to scanBatch pieces, with no cap on the pieces waiting (ADR-141).
//
// The coverage plan (ADR-141) is the scans' memory of what is left: a
// checklist of every area the template covers, the first scan writing it,
// each next one taking the unchecked items in order and checking them off.
// Scans are over once every item is checked and a scan finds nothing more,
// or after maxNones scans in a row that moved nothing on (no piece recorded,
// no item checked): the Burn does what is left, then stops.

// huntTitle is a scan's title.
const huntTitle = "Đang quét…"

// scanBatch: pieces one scan records at most; the next scan goes on.
const scanBatch = 10

// maxNones: scans in a row that moved nothing on before the Burn scans no
// more this run.
const maxNones = 3

// maxCoverage caps the coverage plan kept (ADR-141).
const maxCoverage = 16000

// maxCodeMap caps the code map kept (ADR-130).
const maxCodeMap = 8000

// What a scan's prompt carries: the latest pieces recorded, the latest
// areas looked at.
const (
	maxTakenInScan   = 60
	maxScannedInScan = 10
)

// hunting: a scan (a piece with no kind).
func hunting(it storage.BurnItem) bool { return it.Kind == "" }

// coverageCount reads a coverage plan: its items ("- [ ]" or "- [x]"
// lines) and how many are checked.
func coverageCount(plan string) (checked, total int) {
	for _, line := range strings.Split(plan, "\n") {
		l := strings.TrimLeft(strings.TrimSpace(line), "-*+ ")
		switch {
		case strings.HasPrefix(l, "[ ]"):
			total++
		case strings.HasPrefix(l, "[x]"), strings.HasPrefix(l, "[X]"):
			checked++
			total++
		}
	}
	return checked, total
}

// scanPrompt is a scan's turn (burn/scan.md).
func scanPrompt(b storage.BurnSession, it storage.BurnItem, items []storage.BurnItem, again bool) string {
	var taken []string
	for _, x := range items {
		if x.ID == it.ID || hunting(x) {
			continue
		}
		taken = append(taken, fmt.Sprintf("[%s] %s", x.Status, oneLine(x.Title, 120)))
	}
	if len(taken) > maxTakenInScan {
		taken = taken[len(taken)-maxTakenInScan:]
	}
	var scanned []string
	if b.Scanned != "" {
		scanned = strings.Split(strings.TrimSpace(b.Scanned), "\n")
	}
	if len(scanned) > maxScannedInScan {
		scanned = scanned[len(scanned)-maxScannedInScan:]
	}
	return prompts.Render("burn/scan", struct {
		ID, Focus, Order, Hunt, Map, Lessons, Coverage string
		FocusLooks, Taken, Scanned, Setbacks           []string
		Batch, Left                                    int
		Again                                          bool
	}{it.ID, b.Focus, b.Order, hunt(b), b.CodeMap, b.Lessons, b.Coverage, looks(b), taken, scanned, setbacks(items), scanBatch, left(b.Coverage), again})
}

// left: the items of a coverage plan not checked yet.
func left(plan string) int {
	checked, total := coverageCount(plan)
	return total - checked
}

// priorities: what a scan may say of a piece, as the order it is done in.
var priorities = map[string]int{"high": 2, "normal": 1, "low": 0}

// record adds a piece found (status found, in the run): none twice, a scan
// none past scanBatch. from is the scan recording it ("" = the Burn's chat
// or a worker's), counted for burn_scan_done.
func (s *Service) record(ctx context.Context, b storage.BurnSession, from string, in ToolInput) (string, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return "", errors.New("hãy ghi tiêu đề việc")
	}
	kind := strings.TrimSpace(in.Kind)
	if !Kinds[kind] {
		kind = "upgrade"
	}
	items, _ := s.store.Burn().Items(ctx, b.ID)
	for _, it := range items { // the same piece twice: once
		if !hunting(it) && sameTitle(it.Title, title) {
			return fmt.Sprintf("Đã có việc này: %s (%s).", it.ID, it.Status), nil
		}
	}
	if from != "" {
		s.mu.Lock()
		n := s.found[from]
		s.mu.Unlock()
		if n >= scanBatch {
			return "", fmt.Errorf("lượt quét này đã ghi đủ %d việc: cập nhật coverage rồi kết thúc bằng burn_scan_done, lượt sau làm tiếp", scanBatch)
		}
	}
	it, err := s.store.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: oneLine(title, 160), Kind: kind, Detail: strings.TrimSpace(in.Detail),
		Priority: priorities[strings.TrimSpace(in.Priority)], RunBranch: b.RunBranch})
	if err != nil {
		return "", err
	}
	if from != "" {
		s.mu.Lock()
		s.found[from]++
		s.mu.Unlock()
	}
	s.wake(b.ProjectID) // a free slot does it
	return "Đã ghi việc " + it.ID + ".", nil
}

// scanDone ends a scan: what it looked at and its code map are kept, the
// scan dropped (work, once the turn ends, lets go of its worktree).
func (s *Service) scanDone(ctx context.Context, b storage.BurnSession, it storage.BurnItem, in ToolInput) (string, error) {
	if !hunting(it) {
		return "", fmt.Errorf("việc %s không phải lượt quét: báo burn_done hoặc burn_fail", it.ID)
	}
	s.keepScanned(ctx, b, in.Scanned)
	if m := strings.TrimSpace(in.Map); m != "" {
		if r := []rune(m); len(r) > maxCodeMap {
			m = string(r[:maxCodeMap])
		}
		_ = s.store.Burn().SetCodeMap(context.WithoutCancel(ctx), b.ID, m)
	}
	s.keepLessons(ctx, b, in.Lessons)
	s.keepCoverage(ctx, b, in.Coverage)
	if err := s.store.Burn().DeleteItem(ctx, it.ID); err != nil {
		return "", err
	}
	n := s.endScan(ctx, b, it.ID)
	return fmt.Sprintf("Đã kết thúc lượt quét (%d việc). Dừng ở đây.", n), nil
}

// startScan notes how much of the coverage plan was checked when scan
// started, to tell whether it moved it on.
func (s *Service) startScan(b storage.BurnSession, scan string) {
	checked, _ := coverageCount(b.Coverage)
	s.mu.Lock()
	s.marked[scan] = checked
	s.mu.Unlock()
}

// endScan counts what scan did: a piece recorded or a coverage item checked
// moves the run on (the count of scans in a row with neither starts again);
// every item checked and nothing found, scans are over (ADR-141). It says
// so in the Burn's chat.
func (s *Service) endScan(ctx context.Context, b storage.BurnSession, scan string) int {
	plan := b.Coverage
	if cur, err := s.store.Burn().SessionByID(context.WithoutCancel(ctx), b.ID); err == nil {
		plan = cur.Coverage
	}
	checked, total := coverageCount(plan)
	s.mu.Lock()
	n := s.found[scan]
	before, ok := s.marked[scan]
	if !ok { // a scan from before a restart
		before = checked
	}
	delete(s.found, scan)
	delete(s.marked, scan)
	moved := n > 0 || checked > before
	if moved {
		delete(s.nones, b.RunBranch)
	} else {
		s.nones[b.RunBranch]++
	}
	stuck := s.nones[b.RunBranch] >= maxNones
	done := total > 0 && checked == total && n == 0
	if done {
		s.full[b.RunBranch] = true
	}
	s.mu.Unlock()
	cov := ""
	if total > 0 {
		cov = fmt.Sprintf(" · đã phủ %d/%d mục", checked, total)
	}
	switch {
	case n > 0:
		s.say(ctx, b, fmt.Sprintf("**Quét xong**: %d việc mới trong Tìm thấy%s", n, cov))
	case done:
		s.say(ctx, b, fmt.Sprintf("**Đã phủ hết** %d mục: không còn việc đáng làm, ngưng quét.", total))
	case stuck && total > checked:
		s.say(ctx, b, fmt.Sprintf("%d lượt quét liền không tiến triển, còn %d/%d mục chưa phủ: ngưng quét (bấm Tiếp tục để quét lại).", maxNones, total-checked, total))
	case moved:
		s.say(ctx, b, "Lượt quét không ghi việc mới"+cov+".")
	default:
		s.say(ctx, b, "Lượt quét không tìm thấy việc đáng làm"+cov+".")
	}
	s.wake(b.ProjectID)
	return n
}

// scansOver: every coverage item checked with nothing more found, or
// maxNones scans in a row moved nothing on, this run.
func (s *Service) scansOver(b storage.BurnSession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.full[b.RunBranch] || s.nones[b.RunBranch] >= maxNones
}

// keepCoverage saves a scan's coverage plan, whole (ADR-141).
func (s *Service) keepCoverage(ctx context.Context, b storage.BurnSession, plan string) {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return
	}
	if r := []rune(plan); len(r) > maxCoverage {
		plan = string(r[:maxCoverage])
	}
	_ = s.store.Burn().SetCoverage(context.WithoutCancel(ctx), b.ID, plan)
}

// keepScanned adds what a scan looked at to the Burn's areas scanned.
func (s *Service) keepScanned(ctx context.Context, b storage.BurnSession, area string) {
	area = oneLine(strings.TrimSpace(area), 400)
	if area == "" {
		return
	}
	s.mu.Lock() // one at a time, none lost
	defer s.mu.Unlock()
	cur, err := s.store.Burn().SessionByID(ctx, b.ID)
	if err != nil {
		return
	}
	_ = s.store.Burn().SetScanned(context.WithoutCancel(ctx), b.ID, addScanned(cur.Scanned, area, time.Now()))
}

// sameTitle: two titles that say the same, spacing and case aside.
func sameTitle(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}
