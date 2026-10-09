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
// each its own piece (burn/work.md). One scan at a time; none while MaxFound
// pieces wait. maxNones scans in a row that recorded nothing and the Burn
// scans no more this run: it does what is left, then stops.

// huntTitle is a scan's title.
const huntTitle = "Đang quét…"

// MaxFound: pieces waiting to be done (found or queued, quests included)
// past which the Burn does not scan.
const MaxFound = 10

// maxNones: scans in a row that recorded nothing before the Burn scans no
// more this run.
const maxNones = 3

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

// waiting: a piece recorded and not started yet.
func waiting(it storage.BurnItem) bool {
	return !hunting(it) && (it.Status == "found" || it.Status == "queued")
}

// room is how many pieces a scan may still record.
func room(items []storage.BurnItem) int {
	n := 0
	for _, it := range items {
		if waiting(it) {
			n++
		}
	}
	return max(MaxFound-n, 0)
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
		ID, Focus, Order, Hunt, Map, Lessons string
		FocusLooks, Taken, Scanned, Setbacks []string
		Room                                 int
		Again                                bool
	}{it.ID, b.Focus, b.Order, hunt(b), b.CodeMap, b.Lessons, looks(b), taken, scanned, setbacks(items), max(room(items), 1), again})
}

// priorities: what a scan may say of a piece, as the order it is done in.
var priorities = map[string]int{"high": 2, "normal": 1, "low": 0}

// record adds a piece found (status found, in the run): none twice, none
// past MaxFound waiting. from is the scan recording it ("" = the Burn's
// chat or a worker's), counted for burn_scan_done.
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
	if room(items) == 0 {
		return "", fmt.Errorf("đã có %d việc chờ làm: không ghi thêm, kết thúc bằng burn_scan_done", MaxFound)
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
	if err := s.store.Burn().DeleteItem(ctx, it.ID); err != nil {
		return "", err
	}
	n := s.endScan(ctx, b, it.ID)
	return fmt.Sprintf("Đã kết thúc lượt quét (%d việc). Dừng ở đây.", n), nil
}

// endScan counts what scan recorded: nothing, one more scan in a row with
// nothing; something, the count starts again. It says so in the Burn's chat.
func (s *Service) endScan(ctx context.Context, b storage.BurnSession, scan string) int {
	s.mu.Lock()
	n := s.found[scan]
	delete(s.found, scan)
	if n > 0 {
		delete(s.nones, b.RunBranch)
	} else {
		s.nones[b.RunBranch]++
	}
	s.mu.Unlock()
	if n > 0 {
		s.say(ctx, b, fmt.Sprintf("**Quét xong**: %d việc mới trong Tìm thấy", n))
	} else {
		s.say(ctx, b, "Lượt quét không tìm thấy việc đáng làm.")
	}
	s.wake(b.ProjectID)
	return n
}

// scansOver: maxNones scans in a row found nothing this run.
func (s *Service) scansOver(b storage.BurnSession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nones[b.RunBranch] >= maxNones
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
