package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitAndStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	root := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@x.io")
	run("config", "user.name", "T")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644)
	run("add", ".")
	run("commit", "-qm", "init")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("ONE\n"), 0o644)
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("n\n"), 0o644)
	os.WriteFile(filepath.Join(root, "other.txt"), []byte("o\n"), 0o644)
	ctx := context.Background()
	st, err := ReadStatus(ctx, root)
	if err != nil || st.Branch != "main" || len(st.Changes) != 3 {
		t.Fatalf("%+v %v", st, err)
	}
	if d, _ := Diff(ctx, root, []string{"a.txt"}, 0); !strings.Contains(d, "+ONE") {
		t.Fatal(d)
	}
	hash, err := Commit(ctx, root, "feat: đổi a", []string{"a.txt", "new.txt"})
	if err != nil || hash == "" {
		t.Fatal(err)
	}
	st, _ = ReadStatus(ctx, root)
	if len(st.Changes) != 1 || st.Changes[0].Path != "other.txt" {
		t.Fatalf("only chosen files are committed: %+v", st.Changes)
	}
	if err := CreateBranch(ctx, root, "feat/x"); err != nil {
		t.Fatal(err)
	}
	if err := CreateBranch(ctx, root, "bad name"); err == nil {
		t.Fatal("invalid branch accepted")
	}
	if _, err := ReadStatus(ctx, t.TempDir()); err != ErrNotRepo {
		t.Fatalf("not a repo: %v", err)
	}
}
