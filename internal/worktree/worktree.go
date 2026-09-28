// Package worktree gives agents their own git worktree of a project: they edit
// files and run build/test there, and office turns what they changed into a
// diff that a person merges into the project folder (see ADR-037).
//
// A worktree starts from the project's current state, uncommitted and new
// files included, without touching the person's index or files. Its HEAD is
// the accepted point: changes on top of HEAD are pending; accepting a diff
// moves HEAD past it. Dependency folders (node_modules, .venv) are linked
// from the project and ignored .env files copied, so builds work.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrNotGit: the project folder is not a git repository.
var ErrNotGit = errors.New("thư mục project không phải git repo nên không tạo được worktree")

// Manager keeps the worktrees under Dir (<office home>/worktrees).
type Manager struct {
	Dir string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New builds a Manager.
func New(dir string) *Manager { return &Manager{Dir: dir, locks: map[string]*sync.Mutex{}} }

var safeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Path is where the worktree name of a project lives.
func (m *Manager) Path(projectID, name string) string {
	return filepath.Join(m.Dir, safeName.ReplaceAllString(projectID, "_"), safeName.ReplaceAllString(name, "_"))
}

// Exists reports whether the worktree is there.
func (m *Manager) Exists(projectID, name string) bool {
	_, err := os.Stat(filepath.Join(m.Path(projectID, name), ".git"))
	return err == nil
}

func (m *Manager) lock(key string) func() {
	m.mu.Lock()
	l := m.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// Ensure returns the worktree name of a project, creating it from repo's
// current state when missing. extra lists more ignored paths to link in.
func (m *Manager) Ensure(ctx context.Context, repo, projectID, name string, extra []string) (string, error) {
	dir := m.Path(projectID, name)
	defer m.lock(dir)()
	if m.Exists(projectID, name) {
		return dir, nil
	}
	if _, err := git(ctx, repo, "rev-parse", "--git-dir"); err != nil {
		return "", ErrNotGit
	}
	base, err := snapshot(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("chụp trạng thái project: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	_, _ = git(ctx, repo, "worktree", "prune")
	if _, err := git(ctx, repo, "worktree", "add", "--detach", dir, base); err != nil {
		return "", fmt.Errorf("tạo worktree: %w", err)
	}
	if err := linkIgnored(ctx, repo, dir, extra); err != nil {
		_ = m.remove(ctx, repo, dir)
		return "", err
	}
	return dir, nil
}

// snapshot commits the working tree of repo (tracked, modified and new files
// that git does not ignore) through a temporary index, leaving the person's
// index, files and branches as they are. It returns the commit.
func snapshot(ctx context.Context, repo string) (string, error) {
	tmp, err := os.MkdirTemp("", "office-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	index := filepath.Join(tmp, "index")
	if p, err := git(ctx, repo, "rev-parse", "--path-format=absolute", "--git-path", "index"); err == nil {
		_ = copyFile(strings.TrimSpace(p), index) // a head start for hashing; none is fine
	}
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := gitEnv(ctx, repo, env, "add", "-A"); err != nil {
		return "", err
	}
	tree, err := gitEnv(ctx, repo, env, "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(tree), "-m", "agent-office: trạng thái project khi tạo worktree"}
	if head, err := git(ctx, repo, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
		args = append(args, "-p", strings.TrimSpace(head))
	}
	c, err := gitEnv(ctx, repo, identity, args...)
	return strings.TrimSpace(c), err
}

var linkDirs = []string{"node_modules", ".venv", "venv"}

// linkIgnored links the project's dependency folders and copies its ignored
// .env files into the worktree, and tells git to ignore the links.
func linkIgnored(ctx context.Context, repo, dir string, extra []string) error {
	out, err := git(ctx, repo, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return err
	}
	var links []string
	for _, line := range strings.Split(out, "\n") {
		rel := strings.TrimSuffix(strings.TrimSpace(line), "/")
		if rel == "" || strings.HasPrefix(rel, ".office") || strings.Count(rel, "/") > 4 {
			continue
		}
		base := filepath.Base(rel)
		isDir := strings.HasSuffix(strings.TrimSpace(line), "/")
		switch {
		case isDir && slices.Contains(linkDirs, base):
			links = append(links, rel)
		case !isDir && (base == ".env" || strings.HasPrefix(base, ".env.")):
			_ = copyFile(filepath.Join(repo, rel), filepath.Join(dir, rel))
		}
	}
	for _, x := range extra {
		if x = strings.Trim(filepath.ToSlash(filepath.Clean(strings.TrimSpace(x))), "/"); x != "" && x != "." && !strings.HasPrefix(x, "..") && !slices.Contains(links, x) {
			links = append(links, x)
		}
	}
	var linked []string
	for _, rel := range links {
		src, dst := filepath.Join(repo, rel), filepath.Join(dir, rel)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			continue // tracked in git, or already there
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(src, dst); err != nil {
			return err
		}
		linked = append(linked, rel)
	}
	return excludeLinks(ctx, repo, linked)
}

const excludeMark = "# agent-office worktree links"

// excludeLinks adds the links to the repo's info/exclude: a pattern like
// "node_modules/" matches a folder, not a link to one.
func excludeLinks(ctx context.Context, repo string, links []string) error {
	if len(links) == 0 {
		return nil
	}
	p, err := git(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	file := filepath.Join(strings.TrimSpace(p), "info", "exclude")
	b, _ := os.ReadFile(file)
	have := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, rel := range links {
		if pat := "/" + rel; !have[pat] {
			add = append(add, pat)
		}
	}
	if len(add) == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.Write(b)
	if len(b) > 0 && !bytes.HasSuffix(b, []byte("\n")) {
		buf.WriteString("\n")
	}
	if !have[excludeMark] {
		buf.WriteString(excludeMark + "\n")
	}
	buf.WriteString(strings.Join(add, "\n") + "\n")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, buf.Bytes(), 0o644)
}

var dirLocks sync.Map // worktree dir → *sync.Mutex: one git index user at a time

func lockDir(dir string) func() {
	l, _ := dirLocks.LoadOrStore(filepath.Clean(dir), &sync.Mutex{})
	l.(*sync.Mutex).Lock()
	return l.(*sync.Mutex).Unlock
}

// Changes is what changed in the worktree on top of its accepted point, as a
// diff git can apply to the project folder, limited to only when given.
func Changes(ctx context.Context, dir string, only []string) (string, []string, error) {
	defer lockDir(dir)()
	return changes(ctx, dir, only)
}

func changes(ctx context.Context, dir string, only []string) (string, []string, error) {
	if _, err := git(ctx, dir, "add", "-A"); err != nil {
		return "", nil, err
	}
	names, err := git(ctx, dir, "diff", "--cached", "--name-only", "--no-renames", "HEAD")
	if err != nil {
		return "", nil, err
	}
	var files []string
	for _, f := range strings.Split(names, "\n") {
		if f = strings.TrimSpace(f); f != "" && (only == nil || slices.Contains(only, f)) {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return "", nil, nil
	}
	diff, err := git(ctx, dir, append([]string{"diff", "--cached", "--binary", "--no-renames", "HEAD", "--"}, files...)...)
	return diff, files, err
}

// Changed lists the files changed on top of the accepted point.
func Changed(ctx context.Context, dir string) ([]string, error) {
	_, files, err := Changes(ctx, dir, nil)
	return files, err
}

// Restore puts files back as they are at the accepted point (new files are
// removed): edits an agent made outside what it was given.
func Restore(ctx context.Context, dir string, files []string) error {
	defer lockDir(dir)()
	for _, f := range files {
		if _, err := git(ctx, dir, "cat-file", "-e", "HEAD:"+f); err == nil {
			if _, err := git(ctx, dir, "checkout", "HEAD", "--", f); err != nil {
				return err
			}
			continue
		}
		_, _ = git(ctx, dir, "rm", "-q", "--cached", "--ignore-unmatch", "--", f)
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Accept moves the accepted point past diff (merged into the project), so
// later changes are diffed from there. Files are left as they are.
func Accept(ctx context.Context, dir, diff string) error {
	defer lockDir(dir)()
	tmp, err := os.MkdirTemp("", "office-index-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
	if _, err := gitEnv(ctx, dir, env, "read-tree", "HEAD"); err != nil {
		return err
	}
	if _, err := gitInput(ctx, dir, env, diff, "apply", "--cached", "--binary", "-"); err != nil {
		return err
	}
	tree, err := gitEnv(ctx, dir, env, "write-tree")
	if err != nil {
		return err
	}
	c, err := gitEnv(ctx, dir, identity, "commit-tree", strings.TrimSpace(tree), "-p", "HEAD", "-m", "agent-office: đã gộp vào project")
	if err != nil {
		return err
	}
	_, err = git(ctx, dir, "update-ref", "HEAD", strings.TrimSpace(c))
	return err
}

// Discard takes diff back out of the worktree's files (a rejected change).
func Discard(ctx context.Context, dir, diff string) error {
	defer lockDir(dir)()
	_, err := gitInput(ctx, dir, nil, diff, "apply", "-R", "--binary", "-")
	return err
}

// Remove deletes the worktree name of a project from repo.
func (m *Manager) Remove(ctx context.Context, repo, projectID, name string) error {
	dir := m.Path(projectID, name)
	defer m.lock(dir)()
	return m.remove(ctx, repo, dir)
}

func (m *Manager) remove(ctx context.Context, repo, dir string) error {
	if repo != "" {
		_, _ = git(ctx, repo, "worktree", "remove", "--force", dir)
	}
	err := os.RemoveAll(dir)
	if repo != "" {
		_, _ = git(ctx, repo, "worktree", "prune")
	}
	return err
}

// Sweep removes the worktrees of a project that keep does not want (their
// conversation or task is gone), and those untouched for maxAge.
func (m *Manager) Sweep(ctx context.Context, repo, projectID string, keep func(name string) bool, maxAge time.Duration) {
	root := filepath.Join(m.Dir, safeName.ReplaceAllString(projectID, "_"))
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		old := err == nil && maxAge > 0 && time.Since(info.ModTime()) > maxAge
		if !keep(e.Name()) || old {
			_ = m.Remove(ctx, repo, projectID, e.Name())
		}
	}
}

// identity signs office's own commits in worktrees and snapshots.
var identity = []string{"GIT_AUTHOR_NAME=agent-office", "GIT_AUTHOR_EMAIL=office@localhost",
	"GIT_COMMITTER_NAME=agent-office", "GIT_COMMITTER_EMAIL=office@localhost"}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	return gitInput(ctx, dir, nil, "", args...)
}

func gitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	return gitInput(ctx, dir, env, "", args...)
}

func gitInput(ctx context.Context, dir string, env []string, input string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotepath=false", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
