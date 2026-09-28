package trigger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Script limits (ADR-041).
const (
	MaxScript        = 64 << 10
	maxOutput        = 64 << 10
	defaultTimeout   = 5 * time.Minute
	MaxScriptTimeout = time.Hour
)

// interpreters run a script file, by language.
var interpreters = map[string][]string{"bash": {"bash"}, "node": {"node"}, "python": {"python3"}}

// ValidLang reports whether a script language is supported.
func ValidLang(lang string) bool { _, ok := interpreters[lang]; return ok }

// tail keeps the last n bytes written to it.
type tail struct {
	mu  sync.Mutex
	n   int
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.n {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.n:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return truncateBytes(string(t.buf), t.n) // a cut rune at the start is dropped
}

// RunScript runs s in dir with env and stdin, in its own process group so a
// timeout kills whatever it started. It returns the last 64KB of output
// (stdout and stderr together) and the exit code.
func RunScript(ctx context.Context, dir string, s storage.AutomationScript, env []string, stdin string) (string, int, bool, error) {
	argv, ok := interpreters[s.Lang]
	if !ok {
		return "", -1, false, fmt.Errorf("ngôn ngữ script %q không hỗ trợ (bash, node, python)", s.Lang)
	}
	if len(s.Body) > MaxScript {
		return "", -1, false, errors.New("script quá 64KB")
	}
	timeout := time.Duration(s.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	timeout = min(timeout, MaxScriptTimeout)
	tmp, err := os.MkdirTemp("", "office-script-")
	if err != nil {
		return "", -1, false, err
	}
	defer os.RemoveAll(tmp)
	ext := map[string]string{"bash": ".sh", "node": ".js", "python": ".py"}[s.Lang]
	file := filepath.Join(tmp, "script"+ext)
	if err := os.WriteFile(file, []byte(s.Body), 0o600); err != nil {
		return "", -1, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(argv[0], append(argv[1:], file)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out := &tail{n: maxOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return "", -1, false, fmt.Errorf("không chạy được %s: %w", argv[0], err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var werr error
	timedOut := false
	select {
	case werr = <-done:
	case <-ctx.Done():
		timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // the whole group
		werr = <-done
	}
	code := 0
	var ee *exec.ExitError
	switch {
	case werr == nil:
	case errors.As(werr, &ee):
		code = ee.ExitCode()
	default:
		return out.String(), -1, timedOut, werr
	}
	if timedOut {
		code = -1
	}
	return out.String(), code, timedOut, nil
}

// Signals are the "@@agent: …" lines a script prints to ask for an agent.
func Signals(output string) []string {
	var out []string
	for _, l := range strings.Split(output, "\n") {
		if msg, ok := strings.CutPrefix(strings.TrimSpace(l), "@@agent:"); ok {
			if msg = strings.TrimSpace(msg); msg != "" {
				out = append(out, msg)
			}
		}
	}
	return out
}
