package chat

import (
	"slices"
	"testing"
)

func TestObjectionOf(t *testing.T) {
	for in, want := range map[string]string{
		"PHẢN BIỆN: bản giao sửa nhầm file\nbằng chứng": "bản giao sửa nhầm file",
		"**Phản biện** – thiếu ràng buộc":               "thiếu ràng buộc",
		"PHAN BIEN:": "xem câu trả lời",
		"Đã làm xong, không có phản biện.": "",
		"": "",
	} {
		if got := objectionOf(in); got != want {
			t.Errorf("objectionOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDriftOf(t *testing.T) {
	if what, drift := driftOf("GIÁM SÁT: LỆCH: đang sửa ngoài phạm vi\nchi tiết"); !drift || what != "đang sửa ngoài phạm vi" {
		t.Fatalf("drift = %q %v", what, drift)
	}
	if _, drift := driftOf("**GIÁM SÁT: ỔN**\nđúng hướng"); drift {
		t.Fatal("ok read as drift")
	}
	if _, drift := driftOf("không rõ"); drift {
		t.Fatal("unclear read as drift")
	}
}

// a workflow's role works itself: with full access, no subagents of its own
func TestClaudeArgsNoSubagents(t *testing.T) {
	var r claudeRunner
	a := r.args(RunRequest{FullAccess: true, NoSubagents: true}, false)
	if i := slices.Index(a, "--disallowedTools"); i < 0 || a[i+1] != "Agent Task" {
		t.Fatalf("args = %v", a)
	}
	if a := r.args(RunRequest{FullAccess: true}, false); slices.Contains(a, "--disallowedTools") {
		t.Fatalf("full access without it = %v", a)
	}
}
