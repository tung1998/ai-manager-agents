package burn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// ADR-139: a builder Burn reads features.json; its checks run the passing
// features' acceptance again, one piece at a time, no scan while one is open.
func TestBuilderTemplate(t *testing.T) {
	b := storage.BurnSession{Template: "builder", MaxParallel: 3}
	if !ValidTemplate("builder") || parallel(b) != 1 || parallel(storage.BurnSession{MaxParallel: 3}) != 3 {
		t.Fatal("builder: valid, one at a time")
	}
	dir := t.TempDir()
	if got := verifyCommands(b, dir); len(got) != 0 {
		t.Fatalf("no features.json: %v", got)
	}
	os.WriteFile(filepath.Join(dir, featuresFile), []byte(`[
		{"id":"f1","acceptance":"true","passes":true},
		{"id":"f2","acceptance":"echo f2 hỏng; false","passes":true},
		{"id":"f3","acceptance":"test -f nope","passes":false},
		{"id":"f4","acceptance":"true","passes":true}
	]`), 0o644)
	if got := verifyCommands(b, dir); strings.Join(got, ";") != "true;echo f2 hỏng; false" {
		t.Fatalf("passing features' acceptance, each once: %v", got)
	}
	if got := verifyCommands(storage.BurnSession{}, dir); len(got) != 0 {
		t.Fatalf("not a builder: features.json is not read: %v", got)
	}
	if !strings.Contains(verify(context.Background(), b, dir), "f2 hỏng") {
		t.Fatal("a passing feature that fails sends the piece back")
	}
	os.WriteFile(filepath.Join(dir, featuresFile), []byte(`{"features":[{"id":"f1","acceptance":"make a","passes":true}]}`), 0o644)
	if got := verifyCommands(storage.BurnSession{Template: "builder", Verify: "make build"}, dir); strings.Join(got, ";") != "make build;make a" {
		t.Fatalf("the Burn's own, then the acceptance (wrapped list): %v", got)
	}

	if !builderBusy(b, []storage.BurnItem{{Kind: "unfinished", Status: "review"}}) || builderBusy(b, []storage.BurnItem{{Kind: "unfinished", Status: "done"}, {Status: "doing"}}) {
		t.Fatal("builderBusy: an open piece (not a scan, not done)")
	}
	if builderBusy(storage.BurnSession{}, []storage.BurnItem{{Kind: "bug", Status: "doing"}}) {
		t.Fatal("other templates scan beside the work")
	}

	w := storage.BurnItem{ID: "w"}
	if p := scanPrompt(b, w, nil, false); !strings.Contains(p, "Khởi tạo dự án") || strings.Contains(p, "ROADMAP FIRST") {
		t.Errorf("builder scan:\n%s", p)
	}
	if p := workPrompt(b, storage.BurnItem{ID: "x", Kind: "unfinished"}, false, false, false); !strings.Contains(p, "ONLY the one feature") {
		t.Errorf("builder work:\n%s", p)
	}
}

// A piece no check ran on says so in its summary, once (ADR-139).
func TestUncheckedSaysSo(t *testing.T) {
	s := &Service{}
	it := storage.BurnItem{Status: "done", Summary: "xong", Worktree: t.TempDir()}
	if !s.checked(context.Background(), storage.BurnSession{}, &it) || !strings.HasSuffix(it.Summary, unverified) {
		t.Fatalf("summary %q", it.Summary)
	}
	s.checked(context.Background(), storage.BurnSession{}, &it)
	if strings.Count(it.Summary, unverified) != 1 {
		t.Fatalf("marked twice: %q", it.Summary)
	}
	it = storage.BurnItem{Status: "done", Summary: "xong", Worktree: t.TempDir()}
	if !s.checked(context.Background(), storage.BurnSession{Verify: "true"}, &it) || it.Summary != "xong" {
		t.Fatalf("checked: %q", it.Summary)
	}
}

// Quests go one at a time, oldest first; found pieces still fill the other
// slots. A quest turned down teaches the scan nothing.
func TestQuestsInOrder(t *testing.T) {
	q1 := storage.BurnItem{ID: "q1", Kind: KindQuest, Status: "found"}
	q2 := storage.BurnItem{ID: "q2", Kind: KindQuest, Status: "found"}
	bug := storage.BurnItem{ID: "b", Kind: "bug", Status: "found"}
	if it, _ := next([]storage.BurnItem{q1, q2, bug}, nil, false); it.ID != "q1" {
		t.Fatalf("first: %s", it.ID)
	}
	q1.Status = "doing"
	if it, _ := next([]storage.BurnItem{q1, q2, bug}, map[string]bool{"q1": true}, false); it.ID != "b" {
		t.Fatalf("q2 waits for q1, the bug goes: %s", it.ID)
	}
	q1.Status = "done"
	if it, _ := next([]storage.BurnItem{q1, q2, bug}, nil, false); it.ID != "q2" {
		t.Fatalf("q1 done, q2 next: %s", it.ID)
	}
	q1.Status = "review"
	if _, ok := next([]storage.BurnItem{q1, q2}, map[string]bool{"q1": true}, false); ok {
		t.Fatal("a quest waiting for its review holds the next")
	}
	got := setbacks([]storage.BurnItem{{Kind: KindQuest, Status: "skipped", Summary: "chưa có ADR"}, {Kind: "bug", Status: "skipped", Summary: "không có thật"}})
	if len(got) != 1 || !strings.Contains(got[0], "không có thật") {
		t.Fatalf("setbacks %v", got)
	}
}

// A quest's plan review is told the person chose it.
func TestQuestPlanReview(t *testing.T) {
	q := reviewPrompt(storage.BurnSession{}, storage.BurnItem{ID: "q", Kind: KindQuest, Title: "x"}, "plan")
	if !strings.Contains(q, "whether to do it is their call") {
		t.Errorf("quest plan review:\n%s", q)
	}
	if b := reviewPrompt(storage.BurnSession{}, storage.BurnItem{ID: "b", Kind: "bug", Title: "x"}, "plan"); strings.Contains(b, "their call") {
		t.Errorf("a scan's piece is reviewed as before:\n%s", b)
	}
}
