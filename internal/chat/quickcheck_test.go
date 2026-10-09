package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// checkSays runs the hook on file (relative to dir) and returns what it said.
func checkSays(t *testing.T, dir, file string) string {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"cwd": dir, "tool_name": "Edit", "tool_input": map[string]any{"file_path": file}})
	var out bytes.Buffer
	QuickCheck(bytes.NewReader(in), &out)
	return out.String()
}

func goModule(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.21\n"), 0o644)
	return dir
}

// A Go file badly formatted, with a syntax error or a vet finding is sent
// back to the agent as a block; a good one passes.
func TestQuickCheckGo(t *testing.T) {
	dir := goModule(t)
	write := func(name, src string) { os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644) }

	write("a.go", "package x\n\nfunc A() int { return 1 }\n")
	if s := checkSays(t, dir, "a.go"); s != "" {
		t.Fatalf("good file blocked: %s", s)
	}
	write("a.go", "package x\nfunc A()   int { return 1 }\n")
	s := checkSays(t, dir, "a.go")
	if !strings.Contains(s, `"decision":"block"`) || !strings.Contains(s, "gofmt -w a.go") {
		t.Fatalf("unformatted file: %s", s)
	}
	write("a.go", "package x\n\nfunc A() int { return\n")
	if s := checkSays(t, dir, "a.go"); !strings.Contains(s, `"block"`) || !strings.Contains(s, "a.go:") {
		t.Fatalf("syntax error: %s", s)
	}
	write("a.go", "package x\n\nimport \"fmt\"\n\nfunc A() { fmt.Printf(\"%d\", \"s\") }\n")
	if s := checkSays(t, dir, "a.go"); !strings.Contains(s, "a.go:5") {
		t.Fatalf("vet finding: %s", s)
	}
	write("a.go", "package x\n\nfunc A() int { return 1 }\n")
	write("b.go", "package x\n\nfunc B() int { return undefinedName }\n") // another file's trouble: not this edit's
	if s := checkSays(t, dir, "a.go"); s != "" {
		t.Fatalf("another file's error reported: %s", s)
	}
	if s := checkSays(t, dir, "b.go"); !strings.Contains(s, "undefinedName") {
		t.Fatalf("type error: %s", s)
	}
}

func TestQuickCheckDataFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) { os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644) }
	write("ok.json", `{"a": [1, 2]}`)
	write("bad.json", "{\n  \"a\": 1,\n}\n")
	write("ok.yaml", "a: 1\n---\nb: [1, 2]\n")
	write("bad.yml", "a: [1, 2\n")
	write("x.ts", "const a: number = 'nope'\n")
	for _, f := range []string{"ok.json", "ok.yaml", "x.ts", "missing.go"} {
		if s := checkSays(t, dir, f); s != "" {
			t.Errorf("%s blocked: %s", f, s)
		}
	}
	if s := checkSays(t, dir, "bad.json"); !strings.Contains(s, "JSON sai ở dòng 3") {
		t.Errorf("bad.json: %s", s)
	}
	if s := checkSays(t, dir, "bad.yml"); !strings.Contains(s, "YAML sai") {
		t.Errorf("bad.yml: %s", s)
	}
}

// A file outside the working folder is not looked at.
func TestQuickCheckOutside(t *testing.T) {
	out := t.TempDir()
	bad := filepath.Join(out, "bad.json")
	os.WriteFile(bad, []byte("{"), 0o644)
	if s := checkSays(t, t.TempDir(), bad); s != "" {
		t.Fatalf("outside file checked: %s", s)
	}
	if s := checkSays(t, out, bad); s == "" {
		t.Fatal("inside file not checked")
	}
}

// Out of time: nothing is said, the agent is never held up by the check.
func TestQuickCheckTimeout(t *testing.T) {
	dir := goModule(t)
	f := filepath.Join(dir, "a.go")
	os.WriteFile(f, []byte("package x\nfunc A()   {}\n"), 0o644)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if s := checkFile(ctx, f); s != "" {
		t.Fatalf("timed out check reported: %s", s)
	}
}

// The hook is in the run's settings when the project wants it, never otherwise.
func TestClaudeArgsQuickCheck(t *testing.T) {
	var r claudeRunner
	oldG, oldC := GuardCommand, CheckCommand
	defer func() { GuardCommand, CheckCommand = oldG, oldC }()
	GuardCommand, CheckCommand = "'/x/office' hook guard", "'/x/office' hook check"
	settings := func(req RunRequest) string {
		a := r.args(req, false)
		if i := slices.Index(a, "--settings"); i >= 0 {
			return a[i+1]
		}
		return ""
	}
	s := settings(RunRequest{Write: true, QuickCheck: true})
	if !strings.Contains(s, `"PostToolUse"`) || !strings.Contains(s, "hook check") || !strings.Contains(s, "hook guard") {
		t.Fatalf("quick check missing: %s", s)
	}
	if s := settings(RunRequest{Write: true}); strings.Contains(s, "PostToolUse") || !strings.Contains(s, "hook guard") {
		t.Fatalf("quick check off, still there: %s", s)
	}
	if s := settings(RunRequest{QuickCheck: true}); strings.Contains(s, "PostToolUse") {
		t.Fatalf("a read-only run got the check: %s", s)
	}
	CheckCommand = ""
	if s := settings(RunRequest{Write: true, QuickCheck: true}); strings.Contains(s, "PostToolUse") {
		t.Fatalf("no command, still a hook: %s", s)
	}
}
