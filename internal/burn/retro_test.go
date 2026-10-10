package burn

import (
	"context"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// ADR-143: a piece failed maxTries times is not recorded again; the same
// reason systemicFails times in a row is the process's fault; a run's look
// back gets its numbers and keeps the lessons it writes.
func TestBurnWatchesItsRun(t *testing.T) {
	conflict := "Chưa gộp được vào lần chạy (xung đột với việc khác đã gộp: error: corrupt patch at line 141): làm lại trên bản mới nhất."
	if failClass(conflict) != "chưa gộp được vào lần chạy" || failClass("Kiểm chứng tự động chưa qua: go vet") != "kiểm chứng tự động chưa qua" {
		t.Fatalf("failClass = %q", failClass(conflict))
	}

	s, ag := limitTestService(t)
	ctx := context.Background()
	b, _ := s.store.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: ag.ProjectID, AgentID: ag.ID, State: "running", RunBranch: "burn/x"})
	for _, title := range []string{"wire-ending: Nối ending", "wire-ending (retry b): Nối ending"} {
		it, _ := s.store.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: title, Kind: "unfinished", RunBranch: "burn/x"})
		it.Status, it.Summary = "failed", conflict
		s.store.Burn().UpdateItem(ctx, it)
	}
	if _, err := s.record(ctx, b, "scan", ToolInput{Title: "wire-ending (retry c): Nối ending", Kind: "unfinished"}); err == nil || !strings.Contains(err.Error(), "2 lần") {
		t.Fatalf("a piece failed twice was recorded again: %v", err)
	}
	if _, err := s.record(ctx, b, "", ToolInput{Title: "wire-ending (người dùng): Nối ending", Kind: "unfinished"}); err != nil {
		t.Fatalf("the person may record it again: %v", err)
	}

	items, _ := s.store.Burn().Items(ctx, b.ID)
	if _, ok := systemic(items, "burn/x"); ok {
		t.Fatal("two failures are not yet the process")
	}
	it, _ := s.store.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "khác", Kind: "bug", RunBranch: "burn/x"})
	it.Status, it.Summary = "failed", strings.Replace(conflict, "line 141", "line 9", 1)
	s.store.Burn().UpdateItem(ctx, it)
	items, _ = s.store.Burn().Items(ctx, b.ID)
	if why, ok := systemic(items, "burn/x"); !ok || !strings.Contains(why, "corrupt patch") {
		t.Fatalf("three failures in a row for one reason: %q %v", why, ok)
	}
	if _, ok := systemic(items, "burn/other"); ok {
		t.Fatal("another run's failures count")
	}

	b.Coverage = "- [x] /a\n- [ ] /b"
	p := retroPrompt(b, items, time.Now().Add(-time.Hour))
	for _, want := range []string{"3 failed", `"chưa gộp được vào lần chạy" 3`, "(×", "Coverage plan: 1/2", "```lessons"} {
		if !strings.Contains(p, want) {
			t.Errorf("the look back lacks %q:\n%s", want, p)
		}
	}
	m := lessonsBlock.FindStringSubmatch("Phân tích…\n```lessons\n- Gộp lỗi do diff bị cắt: đã sửa.\n```\n")
	if m == nil || m[1] != "- Gộp lỗi do diff bị cắt: đã sửa.\n" {
		t.Fatalf("lessons block: %q", m)
	}
}
