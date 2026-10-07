package clitools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"

	"bitbucket.org/senprints/agent-office/internal/ids"
)

// JobView is a job as the dashboard sees it.
type JobView struct {
	ID        string     `json:"id"`
	Tool      string     `json:"tool"`
	Action    string     `json:"action"` // install | login
	Command   string     `json:"command"`
	State     string     `json:"state"` // running | succeeded | failed | cancelled
	Output    string     `json:"output"`
	URLs      []string   `json:"urls"`
	Codes     []string   `json:"codes"` // one-time device codes found in the output
	ExitCode  int        `json:"exit_code"`
	Error     string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// Job is one install or login run, executed in a pseudo-terminal so CLIs that
// expect a TTY (login prompts) behave as they would in a terminal.
type Job struct {
	ID        string
	Tool      string
	Action    string
	Command   string
	State     string
	ExitCode  int
	Error     string
	StartedAt time.Time
	EndedAt   *time.Time

	mu     sync.Mutex
	buf    bytes.Buffer
	tty    *os.File
	cancel context.CancelFunc
	done   chan struct{}
}

const maxOutput = 64 << 10

var (
	ansiRe   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*(\x07|\x1b\\)|\x1b[()][A-Z0-9]|\r`)
	longRule = regexp.MustCompile(`([─━═-])[─━═-]{39,}`)
	urlRe    = regexp.MustCompile(`https://[^\s"'<>\x1b]+`)
	// device codes look like ABCD-EFGH or ABCD-1234
	codeRe = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4,5}\b`)
)

// Snapshot returns a copy safe to serialise.
func (j *Job) Snapshot() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := cleanOutput(j.buf.String())
	s := JobView{ID: j.ID, Tool: j.Tool, Action: j.Action, Command: j.Command, State: j.State, Output: out,
		ExitCode: j.ExitCode, Error: j.Error, StartedAt: j.StartedAt, EndedAt: j.EndedAt, URLs: []string{}, Codes: []string{}}
	seen := map[string]bool{}
	at := map[string]int{} // a URL without its query → its place in URLs
	for _, u := range urlRe.FindAllString(out, -1) {
		u = strings.TrimRight(u, ".,;)")
		base, _, _ := strings.Cut(u, "?")
		switch i, ok := at[base]; {
		case seen[u]:
		case ok:
			// a sign-in link issued again (e.g. after a wrong code): the
			// newest one is the one that works
			s.URLs[i] = u
		default:
			at[base] = len(s.URLs)
			s.URLs = append(s.URLs, u)
		}
		seen[u] = true
	}
	for _, c := range codeRe.FindAllString(out, -1) {
		if !seen[c] {
			seen[c] = true
			s.Codes = append(s.Codes, c)
		}
	}
	return s
}

func cleanOutput(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	// a frame drawn across a wide window: keep its rules short
	s = longRule.ReplaceAllString(s, "$1$1$1$1$1$1$1$1$1$1$1$1$1$1$1$1$1$1$1$1")
	// collapse runs of blank lines left by TUI redraws
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Input writes text (plus Enter) to the job's terminal, e.g. a pasted login code.
func (j *Job) Input(text string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.State != "running" || j.tty == nil {
		return errors.New("tiến trình đã kết thúc")
	}
	_, err := io.WriteString(j.tty, text+"\r")
	return err
}

// Cancel stops the job.
func (j *Job) Cancel() {
	j.mu.Lock()
	if j.State == "running" {
		j.State = "cancelled"
	}
	j.mu.Unlock()
	j.cancel()
}

// finish stops a job that has done its work but would keep running (a CLI
// that opens its chat after signing in).
func (j *Job) finish() {
	j.mu.Lock()
	if j.State == "running" {
		j.State = "succeeded"
	}
	j.mu.Unlock()
	j.cancel()
}

// Wait blocks until the job ends (tests).
func (j *Job) Wait() { <-j.done }

func (j *Job) write(p []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.buf.Len()+len(p) > maxOutput {
		// keep the tail: the latest output matters most
		rest := j.buf.Bytes()[j.buf.Len()/2:]
		j.buf = *bytes.NewBuffer(append([]byte("…\n"), rest...))
	}
	j.buf.Write(p)
}

// term is how a job's pseudo-terminal behaves beyond passing bytes, for
// full-screen CLIs (Antigravity's sign-in).
type term struct {
	cols   uint16    // 0 = 120; wide keeps a long sign-in link on one line
	answer bool      // reply to the terminal queries a TUI waits on at start
	keys   []autoKey // keystrokes sent once some text shows up
}

// autoKey sends keys the first time after appears in the output (picking a
// menu entry the dashboard cannot pick).
type autoKey struct{ after, keys string }

// termReplies are what a plain xterm answers to the queries TUIs send: device
// attributes, background colour, keyboard protocol, cursor position.
var termReplies = []struct{ query, reply string }{
	{"\x1b[>c", "\x1b[>0;276;0c"},
	{"\x1b[c", "\x1b[?62;22c"},
	{"\x1b]11;?", "\x1b]11;rgb:0000/0000/0000\x1b\\"},
	{"\x1b]10;?", "\x1b]10;rgb:ffff/ffff/ffff\x1b\\"},
	{"\x1b[?u", "\x1b[?0u"},
	{"\x1b[6n", "\x1b[1;1R"},
}

// start runs argv in a PTY with env; finish is called after exit (for status refresh).
func startJob(tool, action string, argv []string, env []string, timeout time.Duration, tm term) (*Job, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	j := &Job{ID: ids.New("job"), Tool: tool, Action: action, Command: displayCommand(argv), State: "running",
		StartedAt: time.Now().UTC(), cancel: cancel, done: make(chan struct{})}
	cols := tm.cols
	if cols == 0 {
		cols = 120
	}
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: cols})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("không chạy được %s: %w", argv[0], err)
	}
	j.tty = tty
	go func() {
		defer close(j.done)
		defer cancel()
		buf := make([]byte, 4096)
		var seen strings.Builder // plain output so far, for autoKeys
		sent := make([]bool, len(tm.keys))
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				j.write(chunk)
				if tm.answer {
					for _, r := range termReplies {
						for range bytes.Count(chunk, []byte(r.query)) {
							_, _ = io.WriteString(tty, r.reply)
						}
					}
				}
				if len(tm.keys) > 0 {
					if seen.Len() > maxOutput {
						rest := seen.String()[seen.Len()/2:]
						seen.Reset()
						seen.WriteString(rest)
					}
					seen.WriteString(ansiRe.ReplaceAllString(string(chunk), ""))
					for i, k := range tm.keys {
						if !sent[i] && strings.Contains(seen.String(), k.after) {
							sent[i] = true
							_, _ = io.WriteString(tty, k.keys)
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
		werr := cmd.Wait()
		_ = tty.Close()
		now := time.Now().UTC()
		j.mu.Lock()
		defer j.mu.Unlock()
		j.tty, j.EndedAt = nil, &now
		if cmd.ProcessState != nil {
			j.ExitCode = cmd.ProcessState.ExitCode()
		}
		switch {
		case j.State == "cancelled", j.State == "succeeded":
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			j.State, j.Error = "failed", "quá thời gian chờ"
		case werr != nil:
			j.State, j.Error = "failed", werr.Error()
		default:
			j.State = "succeeded"
		}
	}()
	return j, nil
}

func displayCommand(argv []string) string {
	if len(argv) == 3 && argv[0] == "/bin/sh" && argv[1] == "-c" {
		return argv[2]
	}
	return strings.Join(argv, " ")
}
