package burn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/worktree"
)

// mergeTest: a git project with a.txt of five lines, a Burn with its run.
func mergeTest(t *testing.T) (*Service, storage.BurnSession, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-c", "user.email=t@x.io", "-c", "user.name=T"}, args...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n2\n3\n4\n5\n6\n7\n8\n"), 0o644)
	run("add", "-A")
	run("commit", "-qm", "init")
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: dir})
	s := New(st, nil, worktree.New(filepath.Join(tmp, "trees")))
	b, err := st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: p.ID, RunBranch: "burn/test", State: "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	return s, b, dir
}

// piece starts a piece's worktree and changes a.txt there.
func piece(t *testing.T, s *Service, b storage.BurnSession, repo, id string, edit func(string) string) storage.BurnItem {
	t.Helper()
	dir, err := s.pieceTree(context.Background(), b, repo, "burn-"+id)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(edit(string(raw))), 0o644)
	return storage.BurnItem{ID: id, Title: id, Worktree: dir}
}

func runFile(t *testing.T, s *Service, b storage.BurnSession, repo string) string {
	run, err := s.ensureRun(context.Background(), b, repo)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(run, "a.txt"))
	return string(raw)
}

// Two pieces started from the same run, changing nearby lines of a file
// (one in the other's context): the second merges too (3-way), not "a
// conflict, do it again".
func TestIntegrateMergesDifferentLines(t *testing.T) {
	s, b, repo := mergeTest(t)
	ctx := context.Background()
	one := piece(t, s, b, repo, "one", func(x string) string { return strings.Replace(x, "3\n", "three\n", 1) })
	two := piece(t, s, b, repo, "two", func(x string) string { return strings.Replace(x, "5\n", "five\n", 1) })
	if err := s.integrate(ctx, b, &one); err != nil {
		t.Fatal(err)
	}
	if err := s.integrate(ctx, b, &two); err != nil {
		t.Fatalf("different lines must merge: %v", err)
	}
	if got := runFile(t, s, b, repo); got != "1\n2\nthree\n4\nfive\n6\n7\n8\n" {
		t.Fatalf("run = %q", got)
	}
}

// The same lines: the run stays clean, and replay puts the piece on the
// run's latest code with conflict markers for its worker, keeping its work.
func TestReplayKeepsTheWorkOnAClash(t *testing.T) {
	s, b, repo := mergeTest(t)
	ctx := context.Background()
	one := piece(t, s, b, repo, "one", func(x string) string { return strings.Replace(x, "4\n", "four\n", 1) })
	two := piece(t, s, b, repo, "two", func(x string) string { return strings.Replace(x, "4\n", "FOUR\n", 1) + "new\n" })
	if err := s.integrate(ctx, b, &one); err != nil {
		t.Fatal(err)
	}
	if err := s.integrate(ctx, b, &two); err == nil || !strings.Contains(err.Error(), errConflict.Error()) {
		t.Fatalf("same lines must clash: %v", err)
	}
	if got := runFile(t, s, b, repo); got != "1\n2\n3\nfour\n5\n6\n7\n8\n" {
		t.Fatalf("the run must stay as it was: %q", got)
	}
	files, err := s.replay(ctx, b, repo, two)
	if err != nil || len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("replay = %v, %v", files, err)
	}
	raw, _ := os.ReadFile(filepath.Join(two.Worktree, "a.txt"))
	if got := string(raw); !strings.Contains(got, "<<<<<<<") || !strings.Contains(got, "four") || !strings.Contains(got, "FOUR") || !strings.Contains(got, "new\n") {
		t.Fatalf("worktree = %q", got)
	}
	// its worker resolves the markers: it merges now
	os.WriteFile(filepath.Join(two.Worktree, "a.txt"), []byte("1\n2\n3\nfour FOUR\n5\n6\n7\n8\nnew\n"), 0o644)
	if err := s.integrate(ctx, b, &two); err != nil {
		t.Fatalf("resolved, it must merge: %v", err)
	}
	if got := runFile(t, s, b, repo); got != "1\n2\n3\nfour FOUR\n5\n6\n7\n8\nnew\n" {
		t.Fatalf("run = %q", got)
	}
}
