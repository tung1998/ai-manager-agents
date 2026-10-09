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
		if retry := f.s.reviewErrAttempt(ctx, it, "result", nil, boom); !retry {
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
	if retry := f.s.reviewErrAttempt(ctx, it, "result", nil, boom); retry {
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
	if retry := s.reviewErrAttempt(ctx, it, "result", nil, errors.New("x")); retry {
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
		if retry := f.s.reviewErrAttempt(ctx, f.it, "result", []string{""}, boom); !retry {
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
	if retry := f.s.reviewErrAttempt(ctx, f.it, "result", nil, errors.New("x")); !retry {
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

// What the workers looked at is kept (latest last) and given to the next.
func TestScannedIsKept(t *testing.T) {
	at := time.Date(2026, 10, 8, 9, 0, 0, 0, time.Local)
	kept := ""
	for i := range 40 {
		kept = addScanned(kept, fmt.Sprintf("vùng %d %s", i, strings.Repeat("x", 80)), at)
	}
	if n := len([]rune(kept)); n > maxScanned || !strings.Contains(kept, "vùng 39") || strings.Contains(kept, "vùng 0 ") {
		t.Fatalf("kept %d runes:\n%s", n, kept)
	}
	if p := soloPrompt(storage.BurnSession{Scanned: "08/10 09:00 internal/chat"}, storage.BurnItem{ID: "bit_w"}, nil, false, false, false); !strings.Contains(p, "internal/chat") {
		t.Fatal("the next worker is not told what was looked at")
	}
}

// A worker's prompt (ADR-126): find one piece, claim it, do it, report it;
// what the others took is not done again; the order and the focus steer.
func TestSoloPrompt(t *testing.T) {
	items := []storage.BurnItem{
		{ID: "bit_a", Kind: "bug", Status: "done", Title: "Sửa lỗi A"},
		{ID: "bit_b", Kind: "upgrade", Status: "doing", Title: "Nâng cấp B"},
		{ID: "bit_c", Status: "doing", Title: huntTitle}, // another worker still looking
	}
	p := soloPrompt(storage.BurnSession{RunBranch: "burn/x"}, storage.BurnItem{ID: "bit_w"}, items, false, false, false)
	for _, want := range []string{`burn_claim(item="bit_w"`, `burn_done(item="bit_w"`, `burn_none(item="bit_w"`, "[done] Sửa lỗi A", "[doing] Nâng cấp B", "burn/x", "ROADMAP FIRST",
		"ONE piece: fix them all together", `claim it with kind "idea" and build it`} {
		if !strings.Contains(p, want) {
			t.Errorf("worker prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, huntTitle) {
		t.Error("a worker still looking is not a piece taken")
	}
	road := soloPrompt(storage.BurnSession{Order: "roadmap"}, storage.BurnItem{ID: "w"}, nil, false, false, false)
	if i, j := strings.Index(road, "- Roadmap:"), strings.Index(road, "- Bugs:"); i < 0 || j < 0 || i > j {
		t.Errorf("roadmap should come before bugs:\n%s", road)
	}
	bugs := soloPrompt(storage.BurnSession{Order: "bugs"}, storage.BurnItem{ID: "w"}, nil, false, false, false)
	if i, j := strings.Index(bugs, "- Roadmap:"), strings.Index(bugs, "- Bugs:"); i < 0 || j < 0 || j > i || strings.Contains(bugs, "ROADMAP FIRST") {
		t.Errorf("bugs order should list bugs first:\n%s", bugs)
	}
	if r := soloPrompt(storage.BurnSession{}, storage.BurnItem{ID: "w"}, nil, true, true, false); !strings.Contains(r, "started on this before") || !strings.Contains(r, "reviewed first") {
		t.Errorf("a retry or a reviewed result is not said:\n%s", r)
	}
	// reviewed before it is done (a mechanism the person sets up): claim, then stop there
	pre := soloPrompt(storage.BurnSession{}, storage.BurnItem{ID: "w"}, nil, false, false, true)
	if !strings.Contains(pre, "END YOUR TURN") || strings.Contains(pre, "burn_done(") {
		t.Errorf("a pre-reviewed worker should claim and stop:\n%s", pre)
	}
}

func TestWorkPromptFeature(t *testing.T) {
	p := workPrompt(storage.BurnSession{}, storage.BurnItem{Kind: "unfinished", Title: "Thông báo sự cố: phần 1"}, false, false)
	if !strings.Contains(p, "mark the progress") {
		t.Errorf("a roadmap piece should update the roadmap docs:\n%s", p)
	}
	if i := workPrompt(storage.BurnSession{}, storage.BurnItem{Kind: "idea", Title: "MCP sampling"}, false, false); !strings.Contains(i, "A new idea: build it") {
		t.Errorf("an idea is not said to be built:\n%s", i)
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
		if got := verdict(text); got != want {
			t.Errorf("verdict(%q) = %s, want %s", text, got, want)
		}
	}
}

// The focus steers, not limits (ADR-120): first in what a worker looks at,
// with what to look at for a known focus; the work's checks.
func TestFocusSteers(t *testing.T) {
	b := storage.BurnSession{Focus: "tập trung vào bảo mật API"}
	p := soloPrompt(b, storage.BurnItem{ID: "w"}, nil, false, false, false)
	if !strings.Contains(p, "not a limit") || !strings.Contains(p, "injection") || !strings.Contains(p, "any other piece worth doing") {
		t.Errorf("worker prompt does not lead with the focus:\n%s", p)
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
	if focusLenses("build xong") != nil || strings.Contains(soloPrompt(storage.BurnSession{}, storage.BurnItem{ID: "w"}, nil, false, false, false), "focus") {
		t.Error("no focus, or none known: no lens")
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

// A review that failed for want of quota (the error says so, the connection
// reported nothing) is not a system error either.
func TestReviewErrAttemptDoesNotCountAQuotaError(t *testing.T) {
	f := newReviewErrFixture(t)
	ctx := context.Background()
	quota := errors.New("agy: Individual quota reached. Resets in 9m51s.")
	for n := 1; n <= maxReviewErrAttempts+2; n++ {
		if retry := f.s.reviewErrAttempt(ctx, f.it, "result", nil, quota); !retry {
			t.Fatalf("attempt %d: retry = false", n)
		}
	}
	if got, _ := f.st.Burn().Item(ctx, f.it.ID); got.Status != "queued" || got.ReviewErrAttempts != 0 {
		t.Fatalf("item = %+v, want untouched", got)
	}
}

// A workflow reviewer's role agent out of quota stalls every piece: the Burn
// waits for its reset instead of asking again and again (ADR-124).
func TestWaitLimitCountsTheReviewersAgents(t *testing.T) {
	f := newReviewErrFixture(t)
	ctx := context.Background()
	b, _ := f.st.Burn().SessionByID(ctx, f.it.SessionID)
	f.st.Providers().Create(ctx, storage.Provider{Name: "main", Kind: storage.ProviderClaudeCLI, IsDefault: true})
	other, _ := f.st.Providers().Create(ctx, storage.Provider{Name: "gemini", Kind: storage.ProviderClaudeCLI})
	role, err := f.st.Agents().Create(ctx, storage.Agent{ProjectID: b.ProjectID, Name: "B", ProviderID: other.ID, ModelTier: "fast", Instructions: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.Workflows().Create(ctx, storage.Workflow{ProjectID: b.ProjectID, Key: "two-views", Name: "Hai góc nhìn", Bindings: map[string]string{"b": role.ID}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	prof, err := f.st.Burn().SaveReviewProfile(ctx, storage.BurnReviewProfile{ProjectID: b.ProjectID, Name: "r", Stages: map[string]storage.BurnReviewStage{"result": {Workflow: "two-views"}}})
	if err != nil {
		t.Fatal(err)
	}
	b.ReviewProfileID, b.State = prof.ID, "running"
	b, _ = f.st.Burn().SaveSession(ctx, b)
	if f.s.waitLimit(ctx, b) {
		t.Fatal("nobody is out of quota yet")
	}
	reset := time.Now().Add(10 * time.Minute)
	f.st.Settings().Set(ctx, chat.LimitsKey(other.ID), chat.Limits{Status: "rejected", Windows: map[string]chat.LimitWindow{"quota": {Utilization: 1, ResetsAt: reset}}, UpdatedAt: time.Now()})
	if !f.s.waitLimit(ctx, b) {
		t.Fatal("the workflow's role agent is out of quota: the Burn must wait")
	}
	if cur, _ := f.st.Burn().SessionByID(ctx, b.ID); cur.State != "waiting_limit" || cur.WaitingUntil == nil || !cur.WaitingUntil.Equal(reset) {
		t.Fatalf("session = %s until %v, want waiting_limit until %v", cur.State, cur.WaitingUntil, reset)
	}
	f.st.Settings().Set(ctx, chat.LimitsKey(other.ID), chat.Limits{Status: "rejected", Windows: map[string]chat.LimitWindow{"quota": {Utilization: 1, ResetsAt: time.Now().Add(-time.Minute)}}, UpdatedAt: time.Now()})
	if _, hit := f.s.limitsHit(ctx, role.ID); hit {
		t.Fatal("a reset that has passed still counts")
	}
}

// A Burn's template (ADR-128) says what its workers look for and how a piece
// is checked; the frame is the same. General keeps the order; custom is the
// person's own prompt.
func TestTemplates(t *testing.T) {
	w := storage.BurnItem{ID: "w"}
	if g := soloPrompt(storage.BurnSession{}, w, nil, false, false, false); !strings.Contains(g, "ROADMAP FIRST") || strings.Contains(g, "its template") {
		t.Errorf("the general template keeps the order:\n%s", g)
	}
	ux := soloPrompt(storage.BurnSession{Template: "ux"}, w, nil, false, false, false)
	if !strings.Contains(ux, "390px") || strings.Contains(ux, "ROADMAP FIRST") || !strings.Contains(ux, `burn_claim(item="w"`) {
		t.Errorf("ux template:\n%s", ux)
	}
	if i := soloPrompt(storage.BurnSession{Template: "ideas"}, w, nil, false, false, false); !strings.Contains(i, "product's owner") {
		t.Errorf("ideas template:\n%s", i)
	}
	sec := storage.BurnSession{Template: "security", Focus: "bảo mật API"}
	if p := soloPrompt(sec, w, nil, false, false, false); !strings.Contains(p, "injection") || strings.Count(p, "injection") != 1 {
		t.Errorf("security template (its lens once, not again from the focus):\n%s", p)
	}
	if p := workPrompt(sec, storage.BurnItem{}, false, false); !strings.Contains(p, "attack") {
		t.Errorf("security template's checks are not in the work:\n%s", p)
	}
	if p := reviewPrompt(storage.BurnSession{Template: "performance"}, storage.BurnItem{}, "result"); !strings.Contains(p, "before and after") {
		t.Errorf("the result review lacks the template's checks:\n%s", p)
	}
	custom := storage.BurnSession{Template: "custom", HuntPrompt: "Tìm chỗ còn gõ cứng chuỗi tiếng Anh"}
	if p := soloPrompt(custom, w, nil, false, false, false); !strings.Contains(p, "Tìm chỗ còn gõ cứng chuỗi tiếng Anh") || strings.Contains(p, "ROADMAP FIRST") {
		t.Errorf("custom template:\n%s", p)
	}
	if !ValidTemplate("ux") || ValidTemplate("other") || ValidTemplate("") {
		t.Error("ValidTemplate")
	}
}
