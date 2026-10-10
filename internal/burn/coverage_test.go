package burn

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// ADR-141: scans go on while the coverage plan moves on; they are over once
// every item is checked and a scan finds nothing more, or after maxNones
// scans in a row that moved nothing on. The plan is kept whole, not by the
// session's save.
func TestScansGoOnUntilCovered(t *testing.T) {
	s, ag := limitTestService(t)
	ctx := context.Background()
	b, err := s.store.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: ag.ProjectID, AgentID: ag.ID, State: "running", RunBranch: "burn/x"})
	if err != nil {
		t.Fatal(err)
	}
	scan := func(plan string, found int) {
		t.Helper()
		cur, _ := s.store.Burn().SessionByID(ctx, b.ID)
		s.startScan(cur, "scan")
		s.mu.Lock()
		s.found["scan"] = found
		s.mu.Unlock()
		s.keepCoverage(ctx, cur, plan)
		s.endScan(ctx, cur, "scan")
	}
	// nothing found, but each scan checks items off: never over
	scan("- [x] /a\n- [ ] /b\n- [ ] /c\n- [ ] /d", 0)
	scan("- [x] /a\n- [x] /b\n- [ ] /c\n- [ ] /d", 0)
	scan("- [x] /a\n- [x] /b\n- [x] /c\n- [ ] /d", 0)
	if s.scansOver(b) {
		t.Fatal("over while the plan moves on")
	}
	if _, err := s.store.Burn().SaveSession(ctx, b); err != nil { // the loop saving a session read before
		t.Fatal(err)
	}
	if cur, _ := s.store.Burn().SessionByID(ctx, b.ID); left(cur.Coverage) != 1 {
		t.Fatalf("the plan was lost: %q", cur.Coverage)
	}
	// the last item yields pieces: not over yet, the next scan looks again
	scan("- [x] /a\n- [x] /b\n- [x] /c\n- [x] /d", 4)
	if s.scansOver(b) {
		t.Fatal("over on a scan that found pieces")
	}
	scan("- [x] /a\n- [x] /b\n- [x] /c\n- [x] /d", 0)
	if !s.scansOver(b) {
		t.Fatal("all covered, nothing more found: over")
	}

	// a scan stuck (no item checked, nothing found) maxNones times: over
	s.full, s.nones = map[string]bool{}, map[string]int{}
	for range maxNones - 1 {
		scan("- [x] /a\n- [ ] /b", 0)
	}
	if s.scansOver(b) {
		t.Fatal("over too early")
	}
	scan("- [x] /a\n- [ ] /b", 0)
	if !s.scansOver(b) {
		t.Fatal("stuck scans go on for ever")
	}
}

// A scan records scanBatch pieces at most; the next scan goes on. The
// Burn's own chat is not capped, nor the pieces waiting.
func TestScanBatch(t *testing.T) {
	s, ag := limitTestService(t)
	ctx := context.Background()
	b, _ := s.store.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: ag.ProjectID, AgentID: ag.ID, State: "running"})
	for i := range scanBatch {
		if _, err := s.record(ctx, b, "scan", ToolInput{Title: "việc " + string(rune('a'+i)), Kind: "bug"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.record(ctx, b, "scan", ToolInput{Title: "một việc nữa", Kind: "bug"}); err == nil {
		t.Fatal("a scan recorded past its batch")
	}
	if _, err := s.record(ctx, b, "scan2", ToolInput{Title: "lượt sau", Kind: "bug"}); err != nil {
		t.Fatalf("pieces waiting capped the next scan: %v", err)
	}
	if _, err := s.record(ctx, b, "", ToolInput{Title: "từ chat", Kind: "bug"}); err != nil {
		t.Fatal(err)
	}
}

// A scan's plan is refused when it ticks items from memory (ADR-141): more
// checked than one scan can look at, areas bundled on one line, items not
// looked at dropped. A new run sets every item back.
func TestPlanProblem(t *testing.T) {
	var before, after strings.Builder
	for i := range 20 {
		fmt.Fprintf(&before, "- [ ] /screen%d\n", i)
		box := "[ ]"
		if i < maxChecks {
			box = "[x]"
		}
		fmt.Fprintf(&after, "- %s /screen%d — nothing\n", box, i)
	}
	if why := planProblem(before.String(), after.String()); why != "" {
		t.Fatalf("maxChecks looked at is fine: %s", why)
	}
	if why := planProblem(before.String(), after.String()+"- [x] /extra — đã xem lần trước\n"); why == "" {
		t.Fatal("one more checked than a scan can look at")
	}
	if why := planProblem("", "- [x] 01-login, 02-overview, 03-projects — đã xem ở các lần quét trước"); why == "" {
		t.Fatal("areas bundled on one line")
	}
	if why := planProblem("", "- [ ] /a — notes, with, commas"); why != "" {
		t.Fatalf("commas in the note are fine: %s", why)
	}
	if why := planProblem(before.String(), "- [x] /screen0\n- [ ] /screen1"); why == "" {
		t.Fatal("items not looked at were dropped")
	}
	if got := uncheck("Screens:\n- [x] /a — 2 pieces\n  * [X] /b\n- [ ] /c"); got != "Screens:\n- [ ] /a — 2 pieces\n  * [ ] /b\n- [ ] /c" {
		t.Fatalf("uncheck = %q", got)
	}
}

// A piece tried again and again shows once in a scan's prompt, with its
// count: nine failed tries of one feature took most of the-compound's
// prompt (09/10).
func TestTriesShowOnce(t *testing.T) {
	if pieceKey("wire-ending (retry 2026-10-09i): Nối ending") != "wire-ending" || pieceKey("Sửa (lần 2) bảng Jobs") != "sửa bảng jobs" {
		t.Fatal("pieceKey")
	}
	var items []storage.BurnItem
	for i := range 9 {
		items = append(items, storage.BurnItem{ID: fmt.Sprint("f", i), Kind: "unfinished", Status: "failed", Title: fmt.Sprintf("wire-ending (retry %d): Nối ending", i), Summary: "corrupt patch"})
	}
	items = append(items, storage.BurnItem{ID: "d", Kind: "bug", Status: "done", Title: "Bảng Jobs cắt chữ"})
	p := scanPrompt(storage.BurnSession{}, storage.BurnItem{ID: "w"}, items, false)
	if strings.Count(p, "wire-ending") != 2 || !strings.Contains(p, "[failed ×9] wire-ending (retry 8)") || !strings.Contains(p, "[done] Bảng Jobs cắt chữ") {
		t.Fatalf("each piece once, its latest try, in the taken list and the setbacks:\n%s", p)
	}
}
