package burn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A Burn's checks (ADR-131): a worker reporting done is not taken at its
// word. Office runs the checks itself in the piece's worktree, one command a
// line (the Burn's, else guessed from the project); one failing, the piece
// goes back to the worker with the output. No AI: it costs nothing.

// verifyTimeout caps one check.
const verifyTimeout = 10 * time.Minute

// maxVerifyOut caps what a failed check hands back.
const maxVerifyOut = 3000

// maxVerify caps the checks the person writes.
const maxVerify = 2000

// unverified marks a piece no check ran on (the Burn has none, none guessed).
const unverified = "(chưa kiểm chứng tự động: Burn chưa có lệnh kiểm chứng)"

// verifyCommands are the checks of b in dir: the Burn's own lines, else
// what the project's files suggest (none known: no check).
func verifyCommands(b storage.BurnSession, dir string) []string {
	var out []string
	for _, l := range strings.Split(b.Verify, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	if dir == "" {
		return out
	}
	if len(out) == 0 {
		out = guessVerify(dir)
	}
	if templateOf(b) == "builder" { // what the features done say they pass (ADR-139)
		out = append(out, acceptance(dir)...)
	}
	return out
}

// guessVerify: the build checks a project's files suggest, fast ones only
// (tests can be long: the person adds them).
func guessVerify(dir string) []string {
	has := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	var out []string
	if has("go.mod") {
		out = append(out, "go build ./...", "go vet ./...")
	}
	if has("Cargo.toml") {
		out = append(out, "cargo check")
	}
	return out
}

// ValidVerify checks what the person wrote as the Burn's checks.
func ValidVerify(v string) error {
	if len([]rune(v)) > maxVerify {
		return fmt.Errorf("lệnh kiểm chứng dài quá %d ký tự", maxVerify)
	}
	return nil
}

// verify runs the checks of b on a piece in its worktree: "" when they pass
// (or there are none), else which failed and its output's end.
func verify(ctx context.Context, b storage.BurnSession, dir string) string {
	if dir == "" {
		return ""
	}
	for _, c := range verifyCommands(b, dir) {
		cctx, cancel := context.WithTimeout(ctx, verifyTimeout)
		cmd := exec.CommandContext(cctx, "sh", "-c", c)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		timedOut := errors.Is(cctx.Err(), context.DeadlineExceeded)
		cancel()
		if ctx.Err() != nil {
			return "" // stopped: not the piece's fault
		}
		if err == nil {
			continue
		}
		why := err.Error()
		if timedOut {
			why = fmt.Sprintf("quá %s", verifyTimeout)
		}
		text := strings.TrimSpace(string(out))
		if r := []rune(text); len(r) > maxVerifyOut {
			text = "…" + string(r[len(r)-maxVerifyOut:])
		}
		return fmt.Sprintf("`%s` lỗi (%s):\n```\n%s\n```", c, why, text)
	}
	return ""
}

// checked runs the checks on a piece its worker reported done (before its
// result's review and before it is merged): false when one failed, the
// piece then going back to its worker with the output (ADR-131).
func (s *Service) checked(ctx context.Context, b storage.BurnSession, it *storage.BurnItem) bool {
	failed := verify(ctx, b, it.Worktree)
	if ctx.Err() != nil { // stopped meanwhile: checked again when it goes on
		it.Status = "paused"
		return false
	}
	if failed == "" {
		if it.Worktree != "" && !strings.Contains(it.Summary, unverified) && len(verifyCommands(b, it.Worktree)) == 0 { // nothing ran: the person reads it with care
			it.Summary = strings.TrimSpace(it.Summary + " " + unverified)
		}
		return true
	}
	it.ReviewNote = "Kiểm chứng tự động chưa qua, sửa cho qua rồi báo burn_done lại:\n" + failed
	it.Status, it.Summary = s.failedOrAgain(*it, "Kiểm chứng tự động chưa qua: "+oneLine(failed, 300))
	return false
}

// maxLessons caps the lessons kept (ADR-131).
const maxLessons = 3000

// maxSetbacks caps the pieces turned down or failed a scan is shown.
const maxSetbacks = 15

// setbacks are the latest pieces turned down (skipped with a reason) or
// failed, newest first: what a scan learns lessons from.
func setbacks(items []storage.BurnItem) []string {
	var list []storage.BurnItem
	for _, it := range items {
		// a quest turned down teaches nothing about what to look for: the person chose it
		if !hunting(it) && it.Kind != KindQuest && it.Summary != "" && (it.Status == "failed" || it.Status == "skipped") {
			list = append(list, it)
		}
	}
	slices.SortStableFunc(list, func(a, b storage.BurnItem) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	var out []string
	for i, it := range list {
		if i == maxSetbacks {
			break
		}
		out = append(out, fmt.Sprintf("[%s] %s: %s", it.Status, oneLine(it.Title, 100), oneLine(it.Summary, 240)))
	}
	return out
}

// keepLessons saves what a scan wrote as the Burn's lessons.
func (s *Service) keepLessons(ctx context.Context, b storage.BurnSession, lessons string) {
	lessons = strings.TrimSpace(lessons)
	if lessons == "" {
		return
	}
	if r := []rune(lessons); len(r) > maxLessons {
		lessons = string(r[:maxLessons])
	}
	_ = s.store.Burn().SetLessons(context.WithoutCancel(ctx), b.ID, lessons)
}
