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

// A Burn's worker (ADR-126) is a piece not claimed yet (no kind): in its own
// chat and worktree it finds one piece worth doing, claims it (burn_claim),
// does it and reports it, from A to Z. Nothing worth doing: burn_none, and
// the piece is dropped; maxNones of them in a row and the Burn finishes.

// huntTitle is a worker's title until it claims a piece.
const huntTitle = "Đang tìm việc…"

// maxNones: workers in a row that found nothing worth doing before the Burn
// finishes what is in progress and stops.
const maxNones = 3

// What a worker's prompt carries of the others: the latest pieces taken, the
// latest areas looked at.
const (
	maxTakenInSolo   = 60
	maxScannedInSolo = 10
)

// hunting: a worker that has not claimed its piece yet.
func hunting(it storage.BurnItem) bool { return it.Kind == "" }

// soloPrompt is a worker's turn (burn/solo.md).
// preReviewed: the piece is reviewed before it is done, so the worker
// claims it and ends its turn there.
func soloPrompt(b storage.BurnSession, it storage.BurnItem, items []storage.BurnItem, again, reviewed, preReviewed bool) string {
	var taken []string
	for _, x := range items {
		if x.ID == it.ID || hunting(x) {
			continue
		}
		taken = append(taken, fmt.Sprintf("[%s] %s", x.Status, oneLine(x.Title, 120)))
	}
	if len(taken) > maxTakenInSolo {
		taken = taken[len(taken)-maxTakenInSolo:]
	}
	var scanned []string
	if b.Scanned != "" {
		scanned = strings.Split(strings.TrimSpace(b.Scanned), "\n")
	}
	if len(scanned) > maxScannedInSolo {
		scanned = scanned[len(scanned)-maxScannedInSolo:]
	}
	return prompts.Render("burn/solo", struct {
		ID, Focus, Order, Branch, Hunt string
		FocusLooks, Taken              []string
		Scanned                        []string
		Again, Reviewed, Pre           bool
	}{it.ID, b.Focus, b.Order, b.RunBranch, hunt(b), looks(b), taken, scanned, again, reviewed, preReviewed})
}

// claim names a worker's piece: what it found, unless another piece is the
// same (done, dropped or being done).
func (s *Service) claim(ctx context.Context, b storage.BurnSession, it storage.BurnItem, in ToolInput) (string, error) {
	if !hunting(it) {
		return "", fmt.Errorf("việc %s đã nhận rồi: %s", it.ID, it.Title)
	}
	title := oneLine(strings.TrimSpace(in.Title), 160)
	if title == "" {
		return "", errors.New("hãy ghi tiêu đề việc")
	}
	kind := strings.TrimSpace(in.Kind)
	if !Kinds[kind] {
		kind = "upgrade"
	}
	items, _ := s.store.Burn().Items(ctx, b.ID)
	for _, x := range items {
		if x.ID != it.ID && !hunting(x) && sameTitle(x.Title, title) {
			return "", fmt.Errorf("việc này đã có (%s, %s): hãy tìm việc khác", x.ID, x.Status)
		}
	}
	s.keepScanned(ctx, b, in.Scanned)
	it.Title, it.Kind, it.Detail = title, kind, strings.TrimSpace(in.Detail)
	if err := s.store.Burn().UpdateItemFrom(ctx, it, it.Status); err != nil {
		return "", err
	}
	if c, err := s.store.Chat().GetConversation(ctx, it.WorkConversationID); err == nil {
		c.Title = oneLine("Burn: "+title, 80)
		_ = s.store.Chat().UpdateConversation(ctx, c)
	}
	s.say(ctx, b, "**Nhận việc** "+title)
	s.mu.Lock()
	delete(s.nones, b.RunBranch) // it found something
	s.mu.Unlock()
	return "Đã nhận việc: " + title + ". Làm nó rồi báo burn_done.", nil
}

// none: a worker found nothing worth doing; its piece is dropped (work, once
// the turn ends, lets go of its worktree and counts it).
func (s *Service) none(ctx context.Context, b storage.BurnSession, it storage.BurnItem, in ToolInput) (string, error) {
	if !hunting(it) {
		return "", fmt.Errorf("việc %s đã nhận (%s): báo burn_done hoặc burn_fail", it.ID, it.Title)
	}
	s.keepScanned(ctx, b, in.Scanned)
	if err := s.store.Burn().DeleteItem(ctx, it.ID); err != nil {
		return "", err
	}
	return "Đã ghi: không có việc đáng làm. Kết thúc lượt.", nil
}

// noneFound counts a worker that found nothing: maxNones in a row, the Burn
// finishes what is in progress and stops (no waiting while idle).
func (s *Service) noneFound(ctx context.Context, b storage.BurnSession) {
	s.mu.Lock()
	s.nones[b.RunBranch]++
	n := s.nones[b.RunBranch]
	s.mu.Unlock()
	if n >= maxNones {
		_ = s.Drain(context.WithoutCancel(ctx), b.ProjectID)
	}
}

// keepScanned adds what a worker looked at to the Burn's areas scanned.
func (s *Service) keepScanned(ctx context.Context, b storage.BurnSession, area string) {
	area = oneLine(strings.TrimSpace(area), 400)
	if area == "" {
		return
	}
	s.mu.Lock() // workers side by side: one at a time, none lost
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
