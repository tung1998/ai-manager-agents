// Package gitops runs the few git operations office allows: reading status,
// diffs and history, committing chosen files, creating a branch and pushing
// the current branch (never forced).
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Change is one changed file from `git status`.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"` // M, A, D, R, ?? (untracked)…
}

// Status describes the working tree.
type Status struct {
	Branch   string   `json:"branch"`
	Upstream string   `json:"upstream,omitempty"`
	Ahead    int      `json:"ahead"`
	Behind   int      `json:"behind"`
	Changes  []Change `json:"changes"`
}

var ErrNotRepo = errors.New("thư mục project không phải git repo")

func git(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if strings.Contains(msg, "not a git repository") {
			return "", ErrNotRepo
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}

// ReadStatus parses `git status --porcelain=v1 -b`.
func ReadStatus(ctx context.Context, root string) (Status, error) {
	out, err := git(ctx, root, "status", "--porcelain=v1", "-b", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	st := Status{Changes: []Change{}}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			// "main...origin/main [ahead 1, behind 2]"
			head, info, _ := strings.Cut(rest, " [")
			st.Branch, st.Upstream, _ = strings.Cut(head, "...")
			for _, part := range strings.Split(strings.TrimSuffix(info, "]"), ", ") {
				var n int
				if _, err := fmt.Sscanf(part, "ahead %d", &n); err == nil {
					st.Ahead = n
				} else if _, err := fmt.Sscanf(part, "behind %d", &n); err == nil {
					st.Behind = n
				}
			}
			continue
		}
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if _, to, ok := strings.Cut(path, " -> "); ok {
			path = to
		}
		st.Changes = append(st.Changes, Change{Path: strings.Trim(path, `"`), Status: strings.TrimSpace(line[:2])})
	}
	return st, nil
}

// Diff returns the working-tree diff (tracked files), optionally for some files.
func Diff(ctx context.Context, root string, files []string, maxBytes int) (string, error) {
	args := append([]string{"diff", "HEAD", "--"}, files...)
	out, err := git(ctx, root, args...)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && len(out) > maxBytes {
		out = out[:maxBytes] + "\n… (cắt bớt)"
	}
	return out, nil
}

// Log returns the last n commits, one per line.
func Log(ctx context.Context, root string, n int) (string, error) {
	return git(ctx, root, "log", fmt.Sprintf("-%d", n), "--pretty=format:%h %ad %an: %s", "--date=short")
}

// Commit stages exactly files (new, changed or deleted) and commits them.
func Commit(ctx context.Context, root, message string, files []string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("commit message trống")
	}
	if len(files) == 0 {
		return "", errors.New("chưa chọn file để commit")
	}
	if _, err := git(ctx, root, append([]string{"add", "-A", "--"}, files...)...); err != nil {
		return "", err
	}
	if _, err := git(ctx, root, append([]string{"commit", "-m", message, "--"}, files...)...); err != nil {
		return "", err
	}
	hash, err := git(ctx, root, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(hash), err
}

// CreateBranch creates and switches to a new branch (local changes follow).
func CreateBranch(ctx context.Context, root, name string) error {
	if _, err := git(ctx, root, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("tên nhánh không hợp lệ: %s", name)
	}
	_, err := git(ctx, root, "switch", "-c", name)
	return err
}

// Push pushes the current branch to its remote (setting the upstream the
// first time). Never forced.
func Push(ctx context.Context, root string) (string, error) {
	st, err := ReadStatus(ctx, root)
	if err != nil {
		return "", err
	}
	if st.Branch == "" || strings.HasPrefix(st.Branch, "HEAD") {
		return "", errors.New("không ở trên nhánh nào (detached HEAD)")
	}
	args := []string{"push"}
	if st.Upstream == "" {
		args = append(args, "-u", "origin", st.Branch)
	}
	out, err := git(ctx, root, args...)
	return strings.TrimSpace(out), err
}
