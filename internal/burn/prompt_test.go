package burn

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// reviewErrFixture is a store with a project, a Burn session and one queued
// piece, for reviewErrAttempt's unit tests.
type reviewErrFixture struct {
	st storage.Store
	s  *Service
	it storage.BurnItem
}

func newReviewErrFixture(t *testing.T) reviewErrFixture {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	proj, err := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: proj.ID, State: "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Burn().AddItem(ctx, storage.BurnItem{SessionID: sess.ID, Title: "x", Kind: "bug", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	return reviewErrFixture{st: st, s: &Service{store: st}, it: it}
}

// A review's system error (reviewer agent gone, bad config, network — not
// the AI's limit) must not leave a piece looping forever: a few in a row
// fail it instead; a transient blip, followed by success, does not (ADR-120).
func TestReviewErrAttemptFailsAfterAFewTries(t *testing.T) {
	f := newReviewErrFixture(t)
	ctx := context.Background()
	boom := errors.New("fatal: reviewer crashed")

	it := f.it
	for n := 1; n <= maxReviewErrAttempts-1; n++ {
		if retry := f.s.reviewErrAttempt(ctx, it, "issue", "", boom); !retry {
			t.Fatalf("attempt %d: retry = false, want true (under the threshold)", n)
		}
		var err error
		it, err = f.st.Burn().Item(ctx, it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if it.Status != "queued" || it.ReviewErrAttempts != n {
			t.Fatalf("attempt %d: item = %+v", n, it)
		}
	}
	if retry := f.s.reviewErrAttempt(ctx, it, "issue", "", boom); retry {
		t.Fatal("past the threshold: retry = true, want false")
	}
	got, err := f.st.Burn().Item(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || !strings.Contains(got.Summary, "lỗi hệ thống liên tục") {
		t.Fatalf("item = %+v", got)
	}
}

// A context already cancelled (the Burn stopped meanwhile) is not worth
// saving or retrying: ADR-120 only guards against a stuck loop, not a stop.
func TestReviewErrAttemptStopsOnCancel(t *testing.T) {
	s := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	it := storage.BurnItem{ID: "nonexistent", Status: "queued"}
	if retry := s.reviewErrAttempt(ctx, it, "issue", "", errors.New("x")); retry {
		t.Fatal("cancelled context: retry = true, want false")
	}
}

// A reviewer whose connection is at its limit is not a system error: it must
// not be counted (nor ever fail the piece for it), since the loop already
// waits and asks again on its own (step, waitLimit).
func TestReviewErrAttemptDoesNotCountTheAIsLimit(t *testing.T) {
	f := newReviewErrFixture(t)
	ctx := context.Background()
	prov, err := f.st.Providers().Create(ctx, storage.Provider{Name: "p", Kind: storage.ProviderClaudeCLI, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	lim := chat.Limits{Status: "rejected", Windows: map[string]chat.LimitWindow{
		"five_hour": {Utilization: 1, ResetsAt: time.Now().Add(time.Hour)},
	}}
	if err := f.st.Settings().Set(ctx, chat.LimitsKey(prov.ID), lim); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("fatal: reviewer crashed")
	for n := 1; n <= maxReviewErrAttempts+2; n++ { // past the threshold, were it counted
		if retry := f.s.reviewErrAttempt(ctx, f.it, "issue", "", boom); !retry {
			t.Fatalf("attempt %d: retry = false, want true (the AI's limit, not a system error)", n)
		}
	}
	got, err := f.st.Burn().Item(ctx, f.it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "queued" || got.ReviewErrAttempts != 0 {
		t.Fatalf("item = %+v, want untouched", got)
	}
}

// A person acting on a piece while its review was failing (pausing it,
// skipping it) must not be undone by reviewErrAttempt's own save: it only
// touches the piece if its status is still what it was when review() ran.
func TestReviewErrAttemptKeepsAConcurrentChange(t *testing.T) {
	f := newReviewErrFixture(t)
	ctx := context.Background()
	if err := f.st.Burn().UpdateItem(ctx, func() storage.BurnItem { it := f.it; it.Status = "paused"; return it }()); err != nil {
		t.Fatal(err)
	}
	// reviewErrAttempt is handed the snapshot from before the status changed
	// (as gate/finish do: it was read, then review() ran for a while).
	if retry := f.s.reviewErrAttempt(ctx, f.it, "issue", "", errors.New("x")); !retry {
		t.Fatal("retry = false, want true (under the threshold)")
	}
	got, err := f.st.Burn().Item(ctx, f.it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paused" || got.ReviewErrAttempts != 0 {
		t.Fatalf("item = %+v, the concurrent change was overwritten", got)
	}
}

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
	if !strings.Contains(p, "EXACTLY ONE") || !strings.Contains(planPrompt(b, nil, 0, 3), "pick 3 pieces") {
		t.Error("a scan picks as many pieces as there are free slots")
	}
	if p2 := planPrompt(b, nil, 2, 1); !strings.Contains(p2, "2 scans in a row") {
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
	for _, want := range []string{"ROADMAP FIRST", "not approved yet", "short design", "part 1"} {
		if !strings.Contains(road, want) {
			t.Errorf("roadmap prompt lacks %q", want)
		}
	}
	if i, j := strings.Index(road, "- Roadmap:"), strings.Index(road, "- Bugs in detail"); i < 0 || j < 0 || i > j {
		t.Errorf("roadmap should come before bugs")
	}
	bugs := planPrompt(storage.BurnSession{Order: "bugs"}, nil, 0, 1)
	if i, j := strings.Index(bugs, "- Roadmap:"), strings.Index(bugs, "- Bugs in detail"); i < 0 || j < 0 || j > i {
		t.Errorf("bugs order should list bugs first")
	}
	if strings.Contains(bugs, "ROADMAP FIRST") {
		t.Error("bugs order should not push the roadmap first")
	}
	// an old row with no order behaves as roadmap
	if !strings.Contains(planPrompt(storage.BurnSession{}, nil, 0, 1), "ROADMAP FIRST") {
		t.Error("empty order should default to roadmap")
	}
}

func TestWorkPromptFeature(t *testing.T) {
	p := workPrompt(storage.BurnSession{}, storage.BurnItem{Kind: "unfinished", Title: "Thông báo sự cố: phần 1"}, false, false)
	if !strings.Contains(p, "mark the progress") {
		t.Errorf("a roadmap piece should update the roadmap docs:\n%s", p)
	}
}

func TestVerdict(t *testing.T) {
	for text, want := range map[string]string{
		"KẾT LUẬN: ĐỒNG Ý\nổn":            "yes",
		"**KẾT LUẬN: KHÔNG ĐỒNG Ý**\nsai": "no",
		"Ket luan: dong y":                "yes",
		"VERDICT: AGREE\nfine":            "yes",
		"**VERDICT: DISAGREE**\nwrong":    "no",
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
	if strings.Contains(p, "xong 4\n") || !strings.Contains(p, "xong 9") || !strings.Contains(p, "burn_list(what=\"closed\")") {
		t.Errorf("only the latest closed items should be listed:\n%s", p)
	}
}

// The focus steers, not limits (ADR-120): first in a scan and its picks, with
// what to look at for a known focus; the work's checks; no reviewer's no.
func TestFocusSteers(t *testing.T) {
	b := storage.BurnSession{Focus: "tập trung vào bảo mật API"}
	p := planPrompt(b, nil, 0, 1)
	if !strings.Contains(p, "not a limit") || !strings.Contains(p, "injection") || !strings.Contains(p, "come before the order") {
		t.Errorf("scan prompt does not lead with the focus:\n%s", p)
	}
	if !strings.Contains(p, "Never burn_skip a piece only because it is off the focus") {
		t.Error("an out-of-focus piece may be skipped (the real case, 2026-10-08)")
	}
	// the real case (2026-10-08): 3 slots, 2 running, the agent took them for full
	if p1 := planPrompt(storage.BurnSession{MaxParallel: 3}, nil, 0, 1); !strings.Contains(p1, "3 in all, 2 taken by pieces in progress, 1 FREE") || !strings.Contains(p1, "scan new areas") {
		t.Errorf("the free slot is not said outright:\n%s", p1)
	}
	if p3 := planPrompt(b, nil, 0, 3); !strings.Contains(p3, "pick 3 pieces") || !strings.Contains(p3, "scan more new areas") {
		t.Errorf("free slots are not filled:\n%s", p3)
	}
	if strings.Contains(p, "trạng thái đang tải") {
		t.Error("a security focus got the UI lens")
	}
	if w := workPrompt(storage.BurnSession{Focus: "UI/UX trang checkout"}, storage.BurnItem{}, false, false); !strings.Contains(w, "narrow screens") {
		t.Errorf("work prompt lacks the UI checks:\n%s", w)
	}
	if r := reviewPrompt(b, storage.BurnItem{}, "issue"); !strings.Contains(r, "not a reason to turn other work down") {
		t.Errorf("issue review turns down what is off the focus:\n%s", r)
	}
	if focusLenses("build xong") != nil || strings.Contains(planPrompt(storage.BurnSession{}, nil, 0, 1), "focus") {
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
