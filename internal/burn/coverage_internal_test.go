package burn

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/worktree"
)

// overlongFixture is a coverage plan whose items are all different (no two
// lines alike), the way real debt piles up (near-duplicate phrasing, not
// byte-identical lines): it is already past maxCoverage before this scan
// sends anything.
func overlongFixture(n int) string {
	var lines []string
	for i := 0; i < n; i++ {
		lines = append(lines, "- [ ] vùng số "+strconv.Itoa(i)+": một dòng khá dài để chiếm chỗ cho bằng được, mỗi dòng viết khác nhau chút ít")
	}
	return strings.Join(lines, "\n")
}

func coverageTest(t *testing.T) (*Service, func(coverage string) storage.BurnItem) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: tmp})
	s := New(st, nil, worktree.New(filepath.Join(tmp, "trees")))
	newItem := func(coverage string) storage.BurnItem {
		b, err := st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: p.ID, RunBranch: "burn/test", State: "stopped", Coverage: coverage})
		if err != nil {
			t.Fatal(err)
		}
		it, err := st.Burn().AddItem(ctx, storage.BurnItem{SessionID: b.ID, Title: "quét", RunBranch: b.RunBranch})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	return s, newItem
}

// (a) a plan already over maxCoverage, all items distinct (real near-duplicate
// debt, not byte-identical lines): marking one existing item [x], with full
// evidence, must still be accepted, and no item may be lost — ADR-141's
// "never drop an item" holds even while the plan is over the cap.
func TestScanDoneAcceptsMarkingAnItemInOverlongPlan(t *testing.T) {
	ctx := context.Background()
	s, newItem := coverageTest(t)
	before := overlongFixture(200)
	if n := len([]rune(before)); n <= maxCoverage {
		t.Fatalf("fixture too short to exceed the cap: %d", n)
	}
	it := newItem(before)
	b, _ := s.store.Burn().SessionByID(ctx, it.SessionID)

	sent := "- [x] vùng số 0: một dòng khá dài để chiếm chỗ cho bằng được, mỗi dòng viết khác nhau chút ít — đọc a.go:1, không còn gì"
	if _, err := s.scanDone(ctx, b, it, ToolInput{Coverage: sent, Scanned: "a"}); err != nil {
		t.Fatalf("marking an existing item must be accepted even while over cap: %v", err)
	}
	cur, err := s.store.Burn().SessionByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	checked, total := coverageCount(cur.Coverage)
	if total != 200 {
		t.Fatalf("an item was lost: total = %d, want 200", total)
	}
	if checked != 1 {
		t.Fatalf("the marked item did not stick: checked = %d, want 1", checked)
	}
}

// (b) marking items as duplicates of ones already covered ("trùng mục …")
// is the one way to really shrink an over-cap plan: the saved plan must end
// up strictly shorter.
func TestScanDoneDuplicateMarksShrinkOverlongPlan(t *testing.T) {
	ctx := context.Background()
	s, newItem := coverageTest(t)
	before := overlongFixture(200)
	it := newItem(before)
	b, _ := s.store.Burn().SessionByID(ctx, it.SessionID)

	var sent []string
	for i := 1; i <= 5; i++ {
		sent = append(sent, "- [x] vùng số "+strconv.Itoa(i)+": một dòng khá dài để chiếm chỗ cho bằng được, mỗi dòng viết khác nhau chút ít — trùng mục vùng số 0")
	}
	if _, err := s.scanDone(ctx, b, it, ToolInput{Coverage: strings.Join(sent, "\n"), Scanned: "a"}); err != nil {
		t.Fatalf("marking real duplicates must be accepted: %v", err)
	}
	cur, err := s.store.Burn().SessionByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(cur.Coverage)); got >= len([]rune(before)) {
		t.Fatalf("duplicate marks did not shrink the plan: %d >= %d", got, len([]rune(before)))
	}
}

// (c) adding a brand-new area while the plan is already over cap must still
// be refused: the escape hatch is for not making debt worse, not for
// growing it.
func TestScanDoneRefusesNewAreaInOverlongPlan(t *testing.T) {
	ctx := context.Background()
	s, newItem := coverageTest(t)
	before := overlongFixture(200)
	it := newItem(before)
	b, _ := s.store.Burn().SessionByID(ctx, it.SessionID)

	sent := "- [ ] vùng hoàn toàn mới: chưa từng có trong kế hoạch trước đây"
	if _, err := s.scanDone(ctx, b, it, ToolInput{Coverage: sent, Scanned: "a"}); err == nil {
		t.Fatal("a new area added to an already over-cap plan must be refused")
	}
}

// (d) a plan under the cap is unaffected: adding enough new content to push
// it over maxCoverage in one scan is still refused.
func TestScanDoneStillCapsAPlanUnderTheLimit(t *testing.T) {
	ctx := context.Background()
	s, newItem := coverageTest(t)
	before := overlongFixture(5) // well under maxCoverage
	if n := len([]rune(before)); n >= maxCoverage {
		t.Fatalf("fixture too long for this case: %d", n)
	}
	it := newItem(before)
	b, _ := s.store.Burn().SessionByID(ctx, it.SessionID)

	sent := "- [ ] vùng mới: " + strings.Repeat("x", maxCoverage)
	if _, err := s.scanDone(ctx, b, it, ToolInput{Coverage: sent, Scanned: "a"}); err == nil {
		t.Fatal("a plan pushed over the cap from under it must still be refused")
	}
}
