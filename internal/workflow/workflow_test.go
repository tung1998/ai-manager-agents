package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuiltinsParse(t *testing.T) {
	list, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 3 || list[0].Def.Key != "handoff" || list[1].Def.Key != "advisor" || list[2].Def.Key != "council" {
		t.Fatalf("builtins = %d, first %v", len(list), list[0].Def.Key)
	}
	for _, b := range list {
		if b.Def.Body == "" || len(b.Def.Roles) == 0 {
			t.Errorf("%s: empty body or roles", b.Def.Key)
		}
	}
}

const good = `---
key: thu
name: Thử
description: d
roles:
  - key: a
    access: analyze
  - key: b
    access: edit
    differ_from: [a, dieu-phoi]
parallel: [[a, b]]
brief: [outcome, done_when]
gates:
  - key: ok
    kind: check
vote: { roles: [a, b], quorum: 2, veto: [b] }
---
Làm đi.
`

func TestParseDefaults(t *testing.T) {
	d, err := Parse(good)
	if err != nil {
		t.Fatal(err)
	}
	if d.Limits.Rounds != DefaultRounds || d.Limits.Turns != DefaultTurns || d.Timeout() != time.Hour {
		t.Fatalf("limits = %+v", d.Limits)
	}
	if r, _ := d.Role("a"); r.Name != "a" {
		t.Fatalf("role name default = %q", r.Name)
	}
	if !d.SameBatch("a", "b") || d.SameBatch("a", "x") {
		t.Fatal("SameBatch")
	}
	if d.Body != "Làm đi." {
		t.Fatalf("body = %q", d.Body)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no header":    "chỉ có chữ",
		"unknown":      strings.Replace(good, "name: Thử", "name: Thử\nfoo: 1", 1),
		"bad access":   strings.Replace(good, "access: analyze", "access: root", 1),
		"self differ":  strings.Replace(good, "differ_from: [a, dieu-phoi]", "differ_from: [b]", 1),
		"bad parallel": strings.Replace(good, "[[a, b]]", "[[a, z]]", 1),
		"bad brief":    strings.Replace(good, "[outcome, done_when]", "[outcome, foo]", 1),
		"check kind":   strings.Replace(good, "kind: check", "kind: maybe", 1),
		"quorum":       strings.Replace(good, "quorum: 2", "quorum: 3", 1),
		"no body":      strings.TrimSuffix(good, "Làm đi.\n"),
		"coordinator":  strings.Replace(good, "- key: a\n", "- key: dieu-phoi\n", 1),
	}
	for name, src := range cases {
		if _, err := Parse(src); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRenderBrief(t *testing.T) {
	d, _ := Parse(good)
	r, _ := d.Role("a")
	if _, err := d.RenderBrief(r, map[string]string{"outcome": "x"}); err == nil || !strings.Contains(err.Error(), "done_when") {
		t.Fatalf("missing part err = %v", err)
	}
	if _, err := d.RenderBrief(r, map[string]string{"outcome": "x", "done_when": "y", "bogus": "z"}); err == nil {
		t.Fatal("unknown part accepted")
	}
	out, err := d.RenderBrief(r, map[string]string{"outcome": "Đăng nhập được", "done_when": "test xanh"})
	if err != nil || !strings.Contains(out, "Kết quả cần đạt") || !strings.Contains(out, "Chỉ phân tích") {
		t.Fatalf("brief = %q, %v", out, err)
	}
}

func TestLibrary(t *testing.T) {
	l := Library{Dir: filepath.Join(t.TempDir(), "workflows")}
	n, err := l.Seed()
	if err != nil || n < 3 {
		t.Fatalf("Seed = %d, %v", n, err)
	}
	if err := l.Delete("advisor"); err != nil {
		t.Fatal(err)
	}
	if n, _ := l.Seed(); n != 0 {
		t.Fatalf("a deleted builtin came back: %d", n)
	}
	if _, err := l.Get("advisor"); err != ErrNotFound {
		t.Fatalf("Get deleted = %v", err)
	}
	if _, err := l.Reset("advisor"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Save("khac", good); err == nil {
		t.Fatal("key mismatch accepted")
	}
	if _, err := l.Save("thu", good); err != nil {
		t.Fatal(err)
	}
	it, err := l.Get("handoff")
	if err != nil || !it.Builtin || it.Modified {
		t.Fatalf("handoff = %+v, %v", it, err)
	}
	_ = os.WriteFile(filepath.Join(l.Dir, "hong.md"), []byte("---\nkey: hong\n---\n"), 0o644)
	list, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	var broken bool
	for _, x := range list {
		if x.Def.Key == "hong" && x.Error != "" {
			broken = true
		}
	}
	if !broken {
		t.Fatal("a broken file is not listed with its error")
	}
}

// a library seeded under the old Vietnamese keys moves to the English ones,
// keeping what was changed
func TestSeedRenamesOldKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(BuiltinSource("advisor"), "key: advisor", "key: co-van", 1) + "\nghi chú riêng\n"
	if err := os.WriteFile(filepath.Join(dir, "co-van.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".seeded"), []byte("co-van\ngiao-lai\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := Library{Dir: dir}
	if _, err := l.Seed(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "co-van.md")); !os.IsNotExist(err) {
		t.Fatal("old file kept")
	}
	it, err := l.Get("advisor")
	if err != nil || it.Error != "" || !strings.Contains(it.Source, "ghi chú riêng") || !it.Modified {
		t.Fatalf("advisor = %+v, %v", it, err)
	}
	// giao-lai was seeded once and deleted on purpose: handoff stays deleted
	if _, err := l.Get("handoff"); err != ErrNotFound {
		t.Fatalf("handoff came back: %v", err)
	}
}

func TestParseSubWorkflowRole(t *testing.T) {
	d, err := Parse("---\nkey: cha\nname: Cha\nroles:\n  - key: con\n    workflow: advisor\n  - key: ban\n---\nx\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.Roles[0].Access != AccessEdit || d.Roles[1].Access != AccessAnalyze || d.Limits.Depth != DefaultDepth {
		t.Fatalf("defaults = %+v %+v", d.Roles, d.Limits)
	}
	if _, err := Parse("---\nkey: cha\nname: Cha\nroles:\n  - key: con\n    workflow: Bad Key\nlimits:\n  depth: 9\n---\nx\n"); err == nil || !strings.Contains(err.Error(), "workflow") || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("bad sub-workflow: %v", err)
	}
}
