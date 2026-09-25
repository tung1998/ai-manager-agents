package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var diffBlockRe = regexp.MustCompile("(?s)```(?:diff|patch)[ \t]*\n(.*?)```")

// ExtractPatches finds unified diffs in an answer (```diff blocks with ---/+++ headers).
func ExtractPatches(text string) []string {
	var out []string
	for _, m := range diffBlockRe.FindAllStringSubmatch(text, -1) {
		d := strings.TrimSpace(m[1]) + "\n"
		if strings.Contains(d, "\n+++ ") || strings.HasPrefix(d, "--- ") {
			out = append(out, d)
		}
	}
	return out
}

var fileHeaderRe = regexp.MustCompile(`(?m)^(?:---|\+\+\+) (?:[ab]/)?([^\t\n]+)`)

// PatchFiles lists the files a diff touches and rejects unsafe paths.
func PatchFiles(diff string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	for _, m := range fileHeaderRe.FindAllStringSubmatch(diff, -1) {
		p := strings.TrimSpace(m[1])
		if p == "/dev/null" {
			continue
		}
		if filepath.IsAbs(p) || strings.HasPrefix(filepath.Clean(p), "..") || strings.Contains(p, "/../") {
			return nil, fmt.Errorf("diff chứa đường dẫn không an toàn: %s", p)
		}
		if strings.HasPrefix(p, ".git/") || p == ".git" || strings.HasPrefix(p, ".office/") {
			return nil, fmt.Errorf("diff không được sửa %s", p)
		}
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return nil, errors.New("không thấy file nào trong diff")
	}
	return files, nil
}

// CheckPatch verifies a diff applies cleanly without touching files.
func CheckPatch(ctx context.Context, root, diff string) error {
	return gitApply(ctx, root, diff, true)
}

// ApplyPatch writes a diff to the project (after a person approved it).
func ApplyPatch(ctx context.Context, root, diff string) error {
	if _, err := PatchFiles(diff); err != nil {
		return err
	}
	if err := gitApply(ctx, root, diff, true); err != nil {
		return err
	}
	return gitApply(ctx, root, diff, false)
}

func gitApply(ctx context.Context, root, diff string, check bool) error {
	args := []string{"apply", "--recount", "--whitespace=nowarn"}
	if check {
		args = append(args, "--check")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(diff)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("không áp được diff: %s", msg)
	}
	return nil
}
