package burn

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func TestIdleBacksOff(t *testing.T) {
	s := &Service{idle: 5 * time.Minute}
	for n, want := range map[int]time.Duration{1: 5 * time.Minute, 2: 10 * time.Minute, 3: 20 * time.Minute, 4: 40 * time.Minute, 5: time.Hour, 9: time.Hour} {
		if got := s.idleAfter(n); got != want {
			t.Errorf("idleAfter(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestPlanPromptDigsDeeper(t *testing.T) {
	b := storage.BurnSession{MaxParallel: 2}
	p := planPrompt(b, nil, 0, 1)
	for _, want := range []string{"subagent", scannedMark} {
		if !strings.Contains(p, want) {
			t.Errorf("first scan prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "liên tiếp") {
		t.Error("first scan should not mention empty scans")
	}
	if !strings.Contains(p, "ĐÚNG MỘT") || !strings.Contains(planPrompt(b, nil, 0, 3), "chọn đủ 3 việc") {
		t.Error("a scan picks as many pieces as there are free slots")
	}
	if p2 := planPrompt(b, nil, 2, 1); !strings.Contains(p2, "2 lần quét liên tiếp") {
		t.Errorf("repeat scan should say how many came back empty:\n%s", p2)
	}
}

// What the scans looked at is kept off the chat (ADR-116): read from the
// answer, the latest kept, given to the next scan.
func TestScannedIsKept(t *testing.T) {
	if got := scannedOf("Chọn việc X.\n**VÙNG ĐÃ XEM:** internal/burn, dashboard/burn"); got != "internal/burn, dashboard/burn" {
		t.Fatalf("scannedOf = %q", got)
	}
	if got := scannedOf("không có dòng đó"); got != "" {
		t.Fatalf("scannedOf = %q", got)
	}
	at := time.Date(2026, 10, 8, 9, 0, 0, 0, time.Local)
	kept := ""
	for i := range 40 {
		kept = addScanned(kept, fmt.Sprintf("vùng %d %s", i, strings.Repeat("x", 80)), at)
	}
	if n := len([]rune(kept)); n > maxScanned || !strings.Contains(kept, "vùng 39") || strings.Contains(kept, "vùng 0 ") {
		t.Fatalf("kept %d runes:\n%s", n, kept)
	}
	if p := planPrompt(storage.BurnSession{Scanned: "08/10 09:00 internal/chat"}, nil, 0, 1); !strings.Contains(p, "internal/chat") {
		t.Fatal("the next scan is not told what was scanned")
	}
}

func TestPlanPromptOrder(t *testing.T) {
	road := planPrompt(storage.BurnSession{Order: "roadmap"}, nil, 0, 1)
	for _, want := range []string{"LỘ TRÌNH TRƯỚC", "chưa được duyệt", "thiết kế ngắn", "phần 1"} {
		if !strings.Contains(road, want) {
			t.Errorf("roadmap prompt lacks %q", want)
		}
	}
	if i, j := strings.Index(road, "Lộ trình"), strings.Index(road, "Lỗi chi tiết"); i < 0 || j < 0 || i > j {
		t.Errorf("roadmap should come before bugs")
	}
	bugs := planPrompt(storage.BurnSession{Order: "bugs"}, nil, 0, 1)
	if i, j := strings.Index(bugs, "Lộ trình"), strings.Index(bugs, "Lỗi chi tiết"); i < 0 || j < 0 || j > i {
		t.Errorf("bugs order should list bugs first")
	}
	if strings.Contains(bugs, "LỘ TRÌNH TRƯỚC") {
		t.Error("bugs order should not push the roadmap first")
	}
	// an old row with no order behaves as roadmap
	if !strings.Contains(planPrompt(storage.BurnSession{}, nil, 0, 1), "LỘ TRÌNH TRƯỚC") {
		t.Error("empty order should default to roadmap")
	}
}

func TestWorkPromptFeature(t *testing.T) {
	p := workPrompt(storage.BurnSession{}, storage.BurnItem{Kind: "unfinished", Title: "Thông báo sự cố: phần 1"}, false, false)
	if !strings.Contains(p, "đánh dấu tiến độ") {
		t.Errorf("a roadmap piece should update the roadmap docs:\n%s", p)
	}
}

func TestVerdict(t *testing.T) {
	for text, want := range map[string]string{
		"KẾT LUẬN: ĐỒNG Ý\nổn":            "yes",
		"**KẾT LUẬN: KHÔNG ĐỒNG Ý**\nsai": "no",
		"Ket luan: dong y":                "yes",
		"Tôi nghĩ là được":                "unclear",
	} {
		if got := verdict(text); got != want {
			t.Errorf("verdict(%q) = %s, want %s", text, got, want)
		}
	}
}

func TestPlanPromptTrimsClosed(t *testing.T) {
	var items []storage.BurnItem
	for i := 0; i < maxClosedInPlan+5; i++ {
		items = append(items, storage.BurnItem{ID: fmt.Sprintf("bit_done%d", i), Status: "done", Title: fmt.Sprintf("xong %d", i), Summary: "tóm tắt dài"})
	}
	items = append(items, storage.BurnItem{ID: "bit_open", Status: "found", Title: "mở", Summary: "chi tiết mở"})
	p := planPrompt(storage.BurnSession{}, items, 0, 1)
	if !strings.Contains(p, "bit_open") || !strings.Contains(p, "chi tiết mở") {
		t.Error("open item should be listed in full")
	}
	if strings.Contains(p, "bit_done") || strings.Contains(p, "tóm tắt dài") {
		t.Error("closed items should be title only")
	}
	if strings.Contains(p, "xong 0\n") || !strings.Contains(p, "xong 19") || !strings.Contains(p, "5 việc cũ hơn") {
		t.Errorf("only the latest closed items should be listed:\n%s", p)
	}
}

// The focus steers each step (ADR-120): a scan above its order, with what to
// look at for a known focus; the work's checks; a reviewer's no.
func TestFocusSteers(t *testing.T) {
	b := storage.BurnSession{Focus: "tập trung vào bảo mật API"}
	p := planPrompt(b, nil, 0, 1)
	if !strings.Contains(p, "TRỌNG TÂM CỦA NGƯỜI DÙNG") || !strings.Contains(p, "injection") || !strings.Contains(p, "phạm vi TRỌNG TÂM") {
		t.Errorf("scan prompt does not lead with the focus:\n%s", p)
	}
	if !strings.Contains(p, "ĐỂ NGUYÊN") {
		t.Error("an out-of-focus piece may be skipped (the real case, 2026-10-08)")
	}
	if p3 := planPrompt(b, nil, 0, 3); !strings.Contains(p3, "chọn đủ 3 việc") || !strings.Contains(p3, "quét thêm vùng mới") {
		t.Errorf("free slots are not filled:\n%s", p3)
	}
	if strings.Contains(p, "trạng thái đang tải") {
		t.Error("a security focus got the UI lens")
	}
	if w := workPrompt(storage.BurnSession{Focus: "UI/UX trang checkout"}, storage.BurnItem{}, false, false); !strings.Contains(w, "màn hẹp") {
		t.Errorf("work prompt lacks the UI checks:\n%s", w)
	}
	if r := reviewPrompt(b, storage.BurnItem{}, "issue"); !strings.Contains(r, "không phục vụ trọng tâm") {
		t.Errorf("issue review ignores the focus:\n%s", r)
	}
	if focusLenses("build xong") != nil || focusPlan("") != "" {
		t.Error("no focus, or none known: no lens")
	}
}

// A run's summary: what this run did, what is left (ADR-120).
func TestSummary(t *testing.T) {
	start := time.Now().Add(-90 * time.Minute)
	b := storage.BurnSession{StartedAt: &start, ResultMode: "branch"}
	items := []storage.BurnItem{
		{Title: "Sửa lỗi A", Status: "done", Branch: "burn/a", Summary: "đã sửa", CostUSD: 1.5, UpdatedAt: time.Now()},
		{Title: "Cũ", Status: "done", UpdatedAt: start.Add(-time.Hour)},
		{Title: "Làm B", Status: "paused", UpdatedAt: time.Now()},
		{Title: "C", Status: "failed", Summary: "không build được", UpdatedAt: time.Now()},
		{Title: "D", Status: "found"},
	}
	s := summary(b, items, "demo", "dừng hẳn", time.Now())
	for _, want := range []string{"Burn demo đã dừng", "1 giờ 30 phút", "Sửa lỗi A", "burn/a", "Làm B", "không build được", "$1.50", "1 việc tìm thấy"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Cũ") {
		t.Errorf("an earlier run's piece is in it:\n%s", s)
	}
}
