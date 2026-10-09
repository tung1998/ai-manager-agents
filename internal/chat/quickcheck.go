package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"go.yaml.in/yaml/v3"
)

// CheckCommand runs `office hook check`: set by the office binary at start,
// next to GuardCommand (ADR-133). Empty: no quick check.
var CheckCommand string

const (
	checkTimeout = 20 * time.Second // the whole check of one file, hard
	checkMax     = 2000             // what goes back to the agent
)

// checkSettings: after an edit, `office hook check` looks at the edited file
// at once (Claude Code's own limit is a safety net above checkTimeout).
func checkSettings() map[string]any {
	return map[string]any{"matcher": "Write|Edit|MultiEdit", "hooks": []map[string]any{
		{"type": "command", "command": CheckCommand, "timeout": 30},
	}}
}

// QuickCheck is a PostToolUse hook (ADR-133): the file just written is
// checked by its kind (Go: gofmt and vet, JSON and YAML: parsed) and what is
// wrong goes back to the agent to fix now. A file outside the working
// folder, a kind it does not know, a missing tool, a check that fails or
// runs too long: nothing is said.
func QuickCheck(in io.Reader, out io.Writer) {
	var ev struct {
		Cwd       string         `json:"cwd"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		return
	}
	p, _ := ev.ToolInput["file_path"].(string)
	if p == "" || ev.Cwd == "" || strings.HasPrefix(p, "~") {
		return
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(ev.Cwd, p)
	}
	root, target := realPath(ev.Cwd), realPath(p)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	if fi, err := os.Stat(target); err != nil || !fi.Mode().IsRegular() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	msg := checkFile(ctx, target)
	if msg == "" {
		return
	}
	reason := "agent-office kiểm tra nhanh " + filepath.ToSlash(rel) + " sau khi sửa, sửa ngay:\n" + msg
	if len(reason) > checkMax {
		reason = strings.ToValidUTF8(reason[:checkMax], "") + "\n…"
	}
	_ = json.NewEncoder(out).Encode(map[string]any{"decision": "block", "reason": reason})
}

// checkFile: what is wrong with the file, "" when nothing (or no check).
func checkFile(ctx context.Context, path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return checkGo(ctx, path)
	case ".json":
		return checkJSON(path)
	case ".yaml", ".yml":
		return checkYAML(path)
	}
	return "" // .ts/.vue/.js…: no checker fast enough per edit
}

// checkGo: gofmt (syntax and format), then go vet of the file's package,
// only the lines about this file (another file half-edited is not its
// business). vet type-checks too, so a broken build shows here.
func checkGo(ctx context.Context, path string) string {
	if bin, err := exec.LookPath("gofmt"); err == nil {
		out, code, ok := runCheck(ctx, filepath.Dir(path), bin, "-l", "-e", path)
		switch {
		case !ok:
			return ""
		case code != 0: // a syntax error: vet would only say it again
			return strings.TrimSpace(out)
		case strings.TrimSpace(out) != "":
			return "chưa đúng gofmt: chạy `gofmt -w " + filepath.Base(path) + "`"
		}
	}
	bin, err := exec.LookPath("go")
	if err != nil {
		return ""
	}
	out, code, ok := runCheck(ctx, filepath.Dir(path), bin, "vet", ".")
	if !ok || code == 0 {
		return ""
	}
	return linesAbout(out, filepath.Base(path))
}

// linesAbout keeps the lines that point into file ("./x.go:3:2: …").
func linesAbout(out, file string) string {
	re := regexp.MustCompile(`(^|[\s/])` + regexp.QuoteMeta(file) + `:\d+`)
	var keep []string
	for l := range strings.SplitSeq(out, "\n") {
		if re.MatchString(l) {
			keep = append(keep, strings.TrimSpace(l))
		}
	}
	return strings.Join(keep, "\n")
}

func checkJSON(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return ""
	}
	var v any
	err = json.Unmarshal(b, &v)
	var se *json.SyntaxError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &se):
		return fmt.Sprintf("JSON sai ở dòng %d: %v", 1+bytes.Count(b[:min(int(se.Offset), len(b))], []byte("\n")), err)
	}
	return "JSON sai: " + err.Error()
}

func checkYAML(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return ""
		}
		if err != nil {
			return "YAML sai: " + err.Error()
		}
	}
}

// runCheck runs one checker in its own process group, the network and other
// Go toolchains off; ok is false when it could not run or ran out of time.
func runCheck(ctx context.Context, dir, bin string, args ...string) (out string, code int, ok bool) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOTOOLCHAIN=local")
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", -1, false
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return buf.String(), 0, true
	case errors.As(err, &ee):
		return buf.String(), ee.ExitCode(), true
	}
	return "", -1, false
}
