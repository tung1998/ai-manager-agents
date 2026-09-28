package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), identity...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupRepo(t *testing.T) string {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	write(t, filepath.Join(repo, ".gitignore"), "node_modules/\n.env\n")
	write(t, filepath.Join(repo, "a.txt"), "one\n")
	run(t, repo, "add", "-A")
	run(t, repo, "commit", "-q", "-m", "init")
	// the person's uncommitted work, a dependency folder and a secret
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\n")
	write(t, filepath.Join(repo, "new.txt"), "draft\n")
	write(t, filepath.Join(repo, "web", "node_modules", "pkg", "index.js"), "x")
	write(t, filepath.Join(repo, ".env"), "KEY=1\n")
	return repo
}

func TestWorktreeLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := setupRepo(t)
	statusBefore := run(t, repo, "status", "--porcelain")
	m := New(filepath.Join(t.TempDir(), "worktrees"))
	dir, err := m.Ensure(ctx, repo, "p1", "chat-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// starts from what the person sees, without touching their repo
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\ntwo\n" {
		t.Fatalf("a.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal("untracked file missing in worktree")
	}
	if l, err := os.Readlink(filepath.Join(dir, "web", "node_modules")); err != nil || l != filepath.Join(repo, "web", "node_modules") {
		t.Fatalf("node_modules link = %q, %v", l, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".env")); string(b) != "KEY=1\n" {
		t.Fatal(".env not copied")
	}
	if got := run(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Fatalf("repo status changed:\n%s\nwas\n%s", got, statusBefore)
	}
	if again, err := m.Ensure(ctx, repo, "p1", "chat-1", nil); err != nil || again != dir {
		t.Fatalf("Ensure again = %q, %v", again, err)
	}

	// nothing changed yet; links and copied secrets are not changes
	if diff, files, err := Changes(ctx, dir, nil); err != nil || diff != "" || files != nil {
		t.Fatalf("clean changes = %q %v %v", diff, files, err)
	}

	// the agent edits and adds files
	write(t, filepath.Join(dir, "a.txt"), "one\ntwo\nthree\n")
	write(t, filepath.Join(dir, "b.txt"), "bee\n")
	write(t, filepath.Join(dir, "out.txt"), "outside\n")
	diff, files, err := Changes(ctx, dir, []string{"a.txt", "b.txt"})
	if err != nil || strings.Join(files, ",") != "a.txt,b.txt" || !strings.Contains(diff, "+three") {
		t.Fatalf("changes = %v %v\n%s", files, err, diff)
	}
	if err := Restore(ctx, dir, []string{"out.txt"}); err != nil {
		t.Fatal(err)
	}
	if all, _ := Changed(ctx, dir); strings.Join(all, ",") != "a.txt,b.txt" {
		t.Fatalf("after restore = %v", all)
	}

	// merging into the project, then accepting: nothing pending any more
	cmd := exec.Command("git", "apply", "--binary", "-")
	cmd.Dir, cmd.Stdin = repo, strings.NewReader(diff)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply to repo: %v %s", err, out)
	}
	if err := Accept(ctx, dir, diff); err != nil {
		t.Fatal(err)
	}
	if all, _ := Changed(ctx, dir); len(all) != 0 {
		t.Fatalf("pending after accept = %v", all)
	}

	// a rejected change is taken back out
	write(t, filepath.Join(dir, "b.txt"), "bee\nwrong\n")
	d2, _, _ := Changes(ctx, dir, nil)
	if err := Discard(ctx, dir, d2); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "bee\n" {
		t.Fatalf("b.txt after discard = %q", b)
	}

	if err := m.Remove(ctx, repo, "p1", "chat-1"); err != nil {
		t.Fatal(err)
	}
	if m.Exists("p1", "chat-1") || strings.Contains(run(t, repo, "worktree", "list"), dir) {
		t.Fatal("worktree still there")
	}
}

func TestNotGit(t *testing.T) {
	if _, err := New(t.TempDir()).Ensure(context.Background(), t.TempDir(), "p", "x", nil); err != ErrNotGit {
		t.Fatalf("err = %v", err)
	}
}
