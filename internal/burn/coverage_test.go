package burn

import (
	"context"
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
