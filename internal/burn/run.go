package burn

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// One run, one result (ADR-123): the run's own worktree, on the run's own
// branch, a commit per finished piece; the person reviews it there and
// merges the branch. Pieces still run side by side, each in a worktree
// started from the run as it is then, and are merged into it one at a time.

// baseRef is where a piece's worktree started, per worktree: its diff is
// taken from there, whatever the agent committed.
const baseRef = "refs/worktree/burn-base"

// errConflict: a piece's changes no longer apply to the run (another piece
// changed the same lines meanwhile).
var errConflict = errors.New("xung đột với việc khác đã gộp")

// newRunBranch names a new run's branch: burn/<when it started>.
func newRunBranch(now time.Time) string {
	return "burn/" + now.Local().Format("2006-01-02-150405")
}

// runTree is the name of the run's worktree.
func runTree(b storage.BurnSession) string {
	return "burn-run-" + strings.TrimPrefix(b.RunBranch, "burn/")
}

// ensureRun gives the run its worktree, made once, on the run's branch (from
// the project's HEAD, without its uncommitted changes). Called with runMu held.
func (s *Service) ensureRun(ctx context.Context, b storage.BurnSession, repo string) (string, error) {
	if b.RunBranch == "" {
		return "", errors.New("Burn chưa có nhánh của lần chạy")
	}
	name := runTree(b)
	fresh := !s.trees.Exists(b.ProjectID, name)
	dir, err := s.trees.Ensure(ctx, repo, b.ProjectID, name, nil)
	if err != nil || !fresh {
		return dir, err
	}
	if _, err := gitIn(ctx, dir, "", "rev-parse", "--verify", "-q", "refs/heads/"+b.RunBranch); err == nil {
		_, err = gitIn(ctx, dir, "", "switch", "--discard-changes", b.RunBranch) // made again: the branch goes on
		return dir, err
	}
	head, err := gitIn(ctx, repo, "", "rev-parse", "HEAD")
	if err != nil {
		return dir, err
	}
	if _, err := gitIn(ctx, dir, "", "reset", "--hard", head); err != nil {
		return dir, err
	}
	_, err = gitIn(ctx, dir, "", "switch", "-c", b.RunBranch)
	return dir, err
}

// pieceTree gives a piece its worktree, started from the run as it is now (a
// retry goes on from what is there).
func (s *Service) pieceTree(ctx context.Context, b storage.BurnSession, repo, name string) (string, error) {
	if s.trees.Exists(b.ProjectID, name) {
		return s.trees.Path(b.ProjectID, name), nil
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	run, err := s.ensureRun(ctx, b, repo)
	if err != nil {
		return "", err
	}
	base, err := gitIn(ctx, run, "", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	dir, err := s.trees.Ensure(ctx, repo, b.ProjectID, name, nil)
	if err != nil {
		return "", err
	}
	for _, args := range [][]string{{"reset", "--hard", base}, {"update-ref", baseRef, base}} {
		if _, err := gitIn(ctx, dir, "", args...); err != nil {
			_ = s.trees.Remove(ctx, repo, b.ProjectID, name)
			return "", err
		}
	}
	return dir, nil
}

// integrate merges a finished piece into the run (a commit on its branch) and
// drops the piece's worktree.
func (s *Service) integrate(ctx context.Context, b storage.BurnSession, it *storage.BurnItem) error {
	if it.Worktree == "" {
		return errors.New("không có worktree")
	}
	p, err := s.store.Repos().Get(ctx, b.ProjectID)
	if err != nil {
		return err
	}
	if _, err := gitIn(ctx, it.Worktree, "", "add", "-A"); err != nil {
		return err
	}
	base, err := gitIn(ctx, it.Worktree, "", "rev-parse", "--verify", "-q", baseRef)
	if err != nil {
		base = "HEAD" // started before ADR-123
	}
	diff, err := gitIn(ctx, it.Worktree, "", "diff", "--cached", "--binary", "--no-renames", base)
	if err != nil {
		return err
	}
	if diff != "" {
		s.runMu.Lock()
		err = s.mergeInto(ctx, b, p.Path, *it, diff+"\n")
		s.runMu.Unlock()
		if err != nil {
			return err
		}
		it.Branch = b.RunBranch
	}
	if err := s.trees.Remove(ctx, p.Path, b.ProjectID, "burn-"+it.ID); err == nil {
		it.Worktree = ""
	}
	return nil
}

// mergeInto applies a piece's diff to the run. Called with runMu held.
func (s *Service) mergeInto(ctx context.Context, b storage.BurnSession, repo string, it storage.BurnItem, diff string) error {
	run, err := s.ensureRun(ctx, b, repo)
	if err != nil {
		return err
	}
	if out, err := gitIn(ctx, run, diff, "apply", "--binary", "--whitespace=nowarn", "-"); err != nil {
		return fmt.Errorf("%w: %s", errConflict, oneLine(out, 300))
	}
	return commit(ctx, run, b.RunBranch, it.Title, it.Summary)
}

// RunTree is where a project's latest Burn run is, to review: its worktree
// ("" when there is none).
func (s *Service) RunTree(b storage.BurnSession) string {
	if b.RunBranch == "" || s.trees == nil || !s.trees.Exists(b.ProjectID, runTree(b)) {
		return ""
	}
	return s.trees.Path(b.ProjectID, runTree(b))
}

func gitIn(ctx context.Context, dir, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotepath=false", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return strings.TrimSpace(stderr.String() + "\n" + string(out)), err
	}
	return strings.TrimSpace(string(out)), nil
}
