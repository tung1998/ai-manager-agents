package burn

import (
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func TestPathsAndOverlap(t *testing.T) {
	it := storage.BurnItem{Title: "capture.mjs misses screens", Detail: "Where: docs/guide/capture.mjs:417, dashboard/app/app.config.ts:3. e.g. v1.5"}
	got := paths(it)
	if len(got) != 3 || got[0] != "capture.mjs" || got[1] != "docs/guide/capture.mjs" || got[2] != "dashboard/app/app.config.ts" {
		t.Fatalf("paths = %v", got)
	}
	if !overlap([]string{"docs/guide/capture.mjs"}, []string{"capture.mjs"}) {
		t.Fatal("a short name is the same file")
	}
	if overlap([]string{"a/xcapture.mjs"}, []string{"capture.mjs"}) || overlap([]string{"a.go"}, []string{"b.go"}) {
		t.Fatal("different files overlap")
	}
}

func TestNextWaitsForPieceOnSameFile(t *testing.T) {
	busy := storage.BurnItem{ID: "a", Kind: "bug", Status: "doing", Detail: "Where: docs/guide/capture.mjs:10"}
	same := storage.BurnItem{ID: "b", Kind: "bug", Status: "queued", Detail: "Where: capture.mjs:20"}
	other := storage.BurnItem{ID: "c", Kind: "bug", Status: "queued", Detail: "Where: dashboard/app/components/FilesPanel.vue:3"}
	items := []storage.BurnItem{busy, same, other}
	if it, _ := next(items, map[string]bool{"a": true}, false); it.ID != "c" {
		t.Fatalf("next = %s, want c (b waits for a)", it.ID)
	}
	if it, _ := next(items, nil, false); it.ID != "a" {
		t.Fatalf("next = %s, want a", it.ID)
	}
	// begun already (paused, doing, review): it goes on whatever runs beside it
	again := storage.BurnItem{ID: "d", Kind: "bug", Status: "paused", Detail: "Where: capture.mjs:1"}
	if it, _ := next([]storage.BurnItem{busy, again}, map[string]bool{"a": true}, false); it.ID != "d" {
		t.Fatalf("next = %s, want d", it.ID)
	}
}

func TestRelatedIgnoresAmbiguousBasename(t *testing.T) {
	b := storage.BurnSession{RunBranch: "burn/x"}
	items := []storage.BurnItem{
		{ID: "1", Kind: "bug", Status: "done", RunBranch: "burn/x", Title: "api channels fix", Files: []string{"internal/api/channels.go"}, ReviewNote: "AGREE"},
		{ID: "2", Kind: "bug", Status: "queued", RunBranch: "burn/x", Title: "storage channels bug", Files: []string{"internal/storage/sqlite/channels.go"}},
		{ID: "3", Kind: "bug", Status: "queued", RunBranch: "burn/x", Title: "unrelated bug", Detail: "Where: channels.go:5"},
	}
	r := related(b, items[2], items)
	if len(r) != 0 {
		t.Fatalf("related = %v, want none: \"channels.go\" is ambiguous between internal/api and internal/storage/sqlite", r)
	}
}

func TestRelatedAndChanged(t *testing.T) {
	b := storage.BurnSession{RunBranch: "burn/x"}
	items := []storage.BurnItem{
		{ID: "1", Kind: "bug", Status: "done", RunBranch: "burn/x", Title: "wrap soft()", Files: []string{"docs/guide/capture.mjs"}, ReviewNote: "AGREE keep soft()"},
		{ID: "2", Kind: "bug", Status: "skipped", RunBranch: "burn/old", Title: "old run", Files: []string{"docs/guide/capture.mjs"}},
		{ID: "3", Kind: "bug", Status: "done", RunBranch: "burn/x", Title: "other file", Files: []string{"a.go"}},
		{ID: "4", Kind: "bug", Status: "queued", RunBranch: "burn/x", Title: "fail loudly", Detail: "Where: capture.mjs:9"},
	}
	r := related(b, items[3], items)
	if len(r) != 1 || !strings.Contains(r[0], "wrap soft()") || !strings.Contains(r[0], "keep soft()") {
		t.Fatalf("related = %v", r)
	}
	p := reviewPrompt(b, items[3], "plan", items...)
	if !strings.Contains(p, "wrap soft()") || !strings.Contains(p, "burn/x") {
		t.Fatalf("plan review prompt lacks the run or related pieces:\n%s", p)
	}
	if p := reviewPrompt(b, items[3], "result", items...); strings.Contains(p, "never conclude from main alone") {
		t.Fatal("a result review reads the piece's own worktree, not the run's")
	}
	c := changed(b, items)
	if len(c) != 2 || c[0] != "docs/guide/capture.mjs: wrap soft()" || c[1] != "a.go: other file" {
		t.Fatalf("changed = %v", c)
	}
	if s := scanPrompt(b, storage.BurnItem{ID: "w"}, items, false); !strings.Contains(s, "docs/guide/capture.mjs: wrap soft()") {
		t.Fatal("scan prompt lacks the files the run changed")
	}
}
