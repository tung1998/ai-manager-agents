package burn

import (
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func TestVerdict(t *testing.T) {
	for text, want := range map[string]string{
		"KẾT LUẬN: ĐỒNG Ý\nổn":            "yes",
		"**KẾT LUẬN: KHÔNG ĐỒNG Ý**\nsai": "no",
		"Ket luan: dong y":                "yes",
		"VERDICT: AGREE\nfine":            "yes",
		"**VERDICT: DISAGREE**\nwrong":    "no",
		"Tôi nghĩ là được":                "unclear",
		// analysis first, conclusion last — the reviewer agent's usual style
		"Phân tích:\ndòng 1\ndòng 2\ndòng 3\ndòng 4\ndòng 5\nKẾT LUẬN: ĐỒNG Ý": "yes",
		// first line wins even if a later line quotes the opposite verdict
		"KẾT LUẬN: KHÔNG ĐỒNG Ý\nbài kia dùng `KẾT LUẬN: ĐỒNG Ý` ở dòng cuối": "no",
		// a mid-paragraph mention of agreement/disagreement is not a verdict line
		"KẾT LUẬN: ĐỒNG Ý\nmình không đồng ý với X nhưng tổng thể ổn": "yes",
		// no line starts with the KẾT LUẬN prefix at all
		"Đây là phân tích tự do, không có kết luận rõ ràng nào cả.":    "unclear",
		"Analysis:\nline 1\nline 2\nline 3\nline 4\nVERDICT: DISAGREE": "no",
		"I agree with the approach overall":                            "unclear",
	} {
		if got := verdictOf(text); got != want {
			t.Errorf("verdict(%q) = %s, want %s", text, got, want)
		}
	}
}

// A run's summary: what this run did, what is left (ADR-120).
func TestSummary(t *testing.T) {
	start := time.Now().Add(-90 * time.Minute)
	b := storage.BurnSession{StartedAt: &start, ResultMode: "branch", RunBranch: "burn/a"}
	items := []storage.BurnItem{
		{Title: "Sửa lỗi A", Status: "done", Branch: "burn/a", Summary: "đã sửa", CostUSD: 1.5, UpdatedAt: time.Now()},
		{Title: "Cũ", Status: "done", UpdatedAt: start.Add(-time.Hour)},
		{Title: "Làm B", Status: "paused", UpdatedAt: time.Now()},
		{Title: "C", Status: "failed", Summary: "không build được", UpdatedAt: time.Now()},
		{Title: "D", Status: "found"},
	}
	s := summary(b, items, "demo", "dừng hẳn", time.Now())
	for _, want := range []string{"Burn demo đã dừng", "1 giờ 30 phút", "Sửa lỗi A", "Kết quả: nhánh `burn/a`", "Làm B", "không build được", "$1.50", "1 việc tìm thấy"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Cũ") {
		t.Errorf("an earlier run's piece is in it:\n%s", s)
	}
}

// A result turned down goes back while rounds are left, then fails; the next
// review gets the whole earlier note and is told to judge against it.
func TestReviewRounds(t *testing.T) {
	for attempts, want := range map[int]string{1: "queued", 2: "queued", maxReviewRounds: "failed"} {
		if got, _ := reviewAgain(storage.BurnItem{Attempts: attempts}, "x"); got != want {
			t.Errorf("reviewAgain(attempts=%d) = %s, want %s", attempts, got, want)
		}
	}
	note := "VERDICT: DISAGREE\n1. " + strings.Repeat("sửa a ", 200) + "\n2. TAIL-POINT"
	b := storage.BurnSession{}
	p := reviewPrompt(b, storage.BurnItem{ID: "bit_1", Attempts: 2, ReviewNote: note}, "result", "")
	if !strings.Contains(p, "TAIL-POINT") || !strings.Contains(p, "round 2 of at most") {
		t.Errorf("round-2 result prompt misses the earlier note's end or the round rule:\n%s", p)
	}
	if p := reviewPrompt(b, storage.BurnItem{ID: "bit_1", Attempts: 1}, "result", ""); strings.Contains(p, "round 1") {
		t.Error("a first result review is not told about rounds")
	}
	if p := reviewPrompt(b, storage.BurnItem{ID: "bit_1"}, "plan", ""); !strings.Contains(p, "NOT a reason to disagree") {
		t.Error("the plan review must agree with a corrected approach rather than turn a real problem down")
	}
}
