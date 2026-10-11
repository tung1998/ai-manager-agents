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
	for i := range maxChecks + 5 {
		fmt.Fprintf(&before, "- [ ] /screen%d\n", i)
		box := "[ ]"
		if i < maxChecks {
			box = "[x]"
		}
		fmt.Fprintf(&after, "- %s /screen%d — nothing (screen%d.vue read)\n", box, i, i)
	}
	if why := planProblem(before.String(), after.String()); why != "" {
		t.Fatalf("maxChecks looked at, each with its file, is fine: %s", why)
	}
	if why := planProblem(before.String(), after.String()+"- [x] /extra — nothing (extra.vue)\n"); why == "" {
		t.Fatal("one more checked than a scan can look at")
	}
	// ADR-145: an item checked this scan names what was read; one checked
	// before keeps its result as it is.
	if why := planProblem("- [ ] internal/llm", "- [x] internal/llm — nothing: no commits since"); why == "" {
		t.Fatal("checked without the file read")
	}
	if why := planProblem("- [ ] internal/llm", "- [x] internal/llm — nothing: single-turn by design (internal/llm/cli.go:40)"); why != "" {
		t.Fatalf("checked with the file read: %s", why)
	}
	if why := planProblem("- [ ] /projects", "- [x] /projects — 1 piece (shot 03-projects.png)"); why != "" {
		t.Fatalf("checked with the screenshot: %s", why)
	}
	if why := planProblem("- [x] internal/llm — solid\n- [ ] /a", "- [x] internal/llm — solid\n- [ ] /a"); why != "" {
		t.Fatalf("checked in an earlier scan: %s", why)
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

// A scan that rewrites the plan never loses what this run already looked
// at: items checked before stay checked, dropped ones come back.
func TestMergeCoverageKeepsChecked(t *testing.T) {
	before := "- [x] /a — nothing (a.go)\n- [x] /b — 1 piece (b.go)\n- [ ] /c\n- [ ] /d"
	after := "## Flows\n- [ ] /a\n- [ ] /c\n- [x] /d — nothing (d.go)"
	got := mergeCoverage(before, after)
	checked, total := coverageCount(got)
	if checked != 3 || total != 4 {
		t.Fatalf("checked %d/%d, want 3/4:\n%s", checked, total, got)
	}
	if !strings.Contains(got, "/a — nothing (a.go)") || !strings.Contains(got, "/b — 1 piece (b.go)") {
		t.Fatalf("earlier lines not kept:\n%s", got)
	}
	if planProblem(before, got) != "" {
		t.Fatalf("merged plan refused: %s", planProblem(before, got))
	}
}

// A plan written for another template is not reused by a run of this one.
func TestCoverageTaggedByTemplate(t *testing.T) {
	plan := tagged("- [ ] internal/x", "general")
	if planTemplate(plan) != "general" {
		t.Fatalf("tag: %q", plan)
	}
	if again := tagged(plan, "ideas"); planTemplate(again) != "ideas" || strings.Count(again, tagPrefix) != 1 {
		t.Fatalf("retag: %q", again)
	}
	if _, total := coverageCount(plan); total != 1 {
		t.Fatal("the tag line counted as an item")
	}
}

// A scan sends only the lines it changed: the rest of the plan stays, a new
// area goes at the end.
func TestMergeCoveragePatch(t *testing.T) {
	before := "## Flows\n- [x] /a — nothing (a.go)\n- [ ] /b\n- [ ] /c"
	got := mergeCoverage(before, "- [x] /b — 1 piece (b.go)\n- [ ] /new")
	want := "## Flows\n- [x] /a — nothing (a.go)\n- [x] /b — 1 piece (b.go)\n- [ ] /c\n- [ ] /new"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if first := mergeCoverage("", "- [ ] /x\n- [ ] /y"); first != "- [ ] /x\n- [ ] /y" {
		t.Fatalf("first plan: %q", first)
	}
}

// ADR-147: a plan past maxCoverage keeps every item, its checked items'
// results shortened; a scan's new items at the end are not cut off, so it
// counts as moving the plan on.
func TestCoverageFitsWithoutCuttingItems(t *testing.T) {
	var plan strings.Builder
	for i := range 120 {
		fmt.Fprintf(&plan, "- [x] internal/area%03d — %s (internal/area%03d/x.go:1-200)\n", i, strings.Repeat("không lỗi mới, đã đọc kỹ ", 6), i)
	}
	plan.WriteString("- [ ] internal/last — chưa đọc")
	got, fits := fitCoverage(plan.String())
	if !fits || len([]rune(got)) > maxCoverage {
		t.Fatalf("plan not fitted: %d runes", len([]rune(got)))
	}
	if checked, total := coverageCount(got); checked != 120 || total != 121 {
		t.Fatalf("items lost: %d/%d", checked, total)
	}
	if !strings.HasSuffix(got, "- [ ] internal/last — chưa đọc") {
		t.Fatal("the unchecked item was shortened or cut")
	}
	if short := "- [x] /a — ok (a.go)"; func() string { s, _ := fitCoverage(short); return s }() != short {
		t.Fatal("a plan within the cap was changed")
	}

	s, ag := limitTestService(t)
	ctx := context.Background()
	b, err := s.store.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: ag.ProjectID, AgentID: ag.ID, State: "running", RunBranch: "burn/fit"})
	if err != nil {
		t.Fatal(err)
	}
	s.keepCoverage(ctx, b, plan.String())
	cur, _ := s.store.Burn().SessionByID(ctx, b.ID)
	if _, total := coverageCount(cur.Coverage); total != 121 {
		t.Fatalf("kept plan lost items: %d", total)
	}
}

// ADR-147: an item naming no real area can be checked off without a file.
func TestCoverageNotApplicable(t *testing.T) {
	if why := planProblem("- [ ] internal/channels/tools.go", "- [x] internal/channels/tools.go — không tồn tại"); why != "" {
		t.Fatalf("refused: %s", why)
	}
	if why := planProblem("- [ ] /a", "- [x] /a — trùng mục /b"); why != "" {
		t.Fatalf("refused: %s", why)
	}
}
