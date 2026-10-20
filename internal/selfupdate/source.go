package selfupdate

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Fingerprint is the source fingerprint of the running build, stamped by
// "Cập nhật office" (-ldflags -X); "" for a build made by hand.
var Fingerprint string

const fingerprintVar = "bitbucket.org/senprints/agent-office/internal/selfupdate.Fingerprint"

// gitTimeout bounds each git call of a source check.
const gitTimeout = 10 * time.Second

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	return gitFor(ctx, gitTimeout, root, args...)
}

// fetchTimeout bounds asking the remote (network).
const fetchTimeout = 30 * time.Second

// gitEnv: never stop at a password prompt (no one is there to answer)
var gitEnv = []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"}

func gitFor(ctx context.Context, d time.Duration, root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), gitEnv...)
	return cmd.Output()
}

// SourceFingerprint hashes what a build of root would contain: HEAD, the
// uncommitted changes and the files git does not know yet (ignored ones
// left out). Two equal fingerprints mean the same code.
func SourceFingerprint(ctx context.Context, root string) (string, error) {
	head, err := git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", errors.New("không đọc được git của mã nguồn")
	}
	diff, err := git(ctx, root, "diff", "HEAD", "--binary")
	if err != nil {
		return "", err
	}
	others, err := git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(bytes.TrimSpace(head))
	h.Write([]byte{0})
	h.Write(diff)
	for _, name := range strings.Split(string(others), "\x00") {
		if name == "" {
			continue
		}
		h.Write([]byte{0})
		h.Write([]byte(name))
		h.Write([]byte{0})
		if raw, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			h.Write(raw)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// Commit is one commit the running build does not have.
type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	At      string `json:"at"`
}

// Changes says whether the source differs from the running build.
type Changes struct {
	Changed bool `json:"changed"`
	// fingerprints of the source now and of the running build ("" built by hand)
	Fingerprint string `json:"fingerprint"`
	Running     string `json:"running,omitempty"`
	Head        string `json:"head"`
	// commits since the running build, newest first, at most maxCommits
	Commits []Commit `json:"commits"`
	More    bool     `json:"more"`
	// files changed or new, not committed
	Uncommitted int `json:"uncommitted"`
	// the running revision is not in the source
	Unknown bool `json:"unknown,omitempty"`
	// the branch on the remote (nil: the source has no remote)
	Remote *Remote `json:"remote,omitempty"`
}

// Remote is what the source's branch on the remote (origin/main,
// origin/master…) has that the source has not pulled yet.
type Remote struct {
	Branch string `json:"branch"`
	// commits there not here, newest first, at most maxCommits
	Behind  int      `json:"behind"`
	Commits []Commit `json:"commits"`
	More    bool     `json:"more"`
	// commits here not there (pushed later; an update keeps them)
	Ahead int `json:"ahead"`
	// fetching or comparing failed (offline, no access…)
	Error string `json:"error,omitempty"`
}

const maxCommits = 20

// Changes compares the source with the running build (revision: what the Go
// toolchain stamped in it). With a stamped fingerprint the answer is
// exact; a build made by hand is compared by commit, and counts as changed
// while the source has uncommitted files.
func (s Source) Changes(ctx context.Context, revision string) (Changes, error) {
	fp, err := SourceFingerprint(ctx, s.Root)
	if err != nil {
		return Changes{}, err
	}
	head, _ := git(ctx, s.Root, "rev-parse", "HEAD")
	c := Changes{Fingerprint: fp, Running: Fingerprint, Head: strings.TrimSpace(string(head)), Commits: []Commit{}}
	if st, err := git(ctx, s.Root, "status", "--porcelain"); err == nil {
		for _, l := range strings.Split(string(st), "\n") {
			if strings.TrimSpace(l) != "" {
				c.Uncommitted++
			}
		}
	}
	if revision != "" && revision != c.Head {
		out, err := git(ctx, s.Root, "log", "--format=%h%x00%s%x00%cI", "-n", "21", revision+"..HEAD", "--")
		if err != nil {
			c.Unknown = true
		}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f := strings.SplitN(l, "\x00", 3); len(f) == 3 {
				c.Commits = append(c.Commits, Commit{Hash: f[0], Subject: f[1], At: f[2]})
			}
		}
		if len(c.Commits) > maxCommits {
			c.Commits, c.More = c.Commits[:maxCommits], true
		}
	}
	if Fingerprint != "" {
		c.Changed = fp != Fingerprint
	} else {
		c.Changed = revision == "" || revision != c.Head || c.Uncommitted > 0
	}
	if c.Remote = s.Remote(ctx); c.Remote != nil && c.Remote.Behind > 0 {
		c.Changed = true
	}
	return c, nil
}

// Upstream is the remote branch the source follows: its upstream, else
// origin's default branch, else origin/main or origin/master ("" when none).
func (s Source) Upstream(ctx context.Context) string {
	if out, err := git(ctx, s.Root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		return strings.TrimSpace(string(out))
	}
	if out, err := git(ctx, s.Root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(string(out))
	}
	for _, b := range []string{"origin/main", "origin/master"} {
		if _, err := git(ctx, s.Root, "rev-parse", "--verify", "--quiet", b); err == nil {
			return b
		}
	}
	return ""
}

// Remote fetches origin and compares the source with its branch there
// (nil when the source has no origin).
func (s Source) Remote(ctx context.Context) *Remote {
	if _, err := git(ctx, s.Root, "remote", "get-url", "origin"); err != nil {
		return nil
	}
	r := &Remote{Commits: []Commit{}}
	if _, err := gitFor(ctx, fetchTimeout, s.Root, "fetch", "--quiet", "origin"); err != nil {
		r.Error = "không lấy được mã từ origin: " + gitError(err)
	}
	if r.Branch = s.Upstream(ctx); r.Branch == "" {
		r.Error = cmp.Or(r.Error, "không thấy nhánh main hay master trên origin")
		return r
	}
	count := func(rng string) int {
		out, _ := git(ctx, s.Root, "rev-list", "--count", rng)
		n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		return n
	}
	r.Behind, r.Ahead = count("HEAD.."+r.Branch), count(r.Branch+"..HEAD")
	if r.Behind > 0 {
		out, _ := git(ctx, s.Root, "log", "--format=%h%x00%s%x00%cI", "-n", "21", "HEAD.."+r.Branch, "--")
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f := strings.SplitN(l, "\x00", 3); len(f) == 3 {
				r.Commits = append(r.Commits, Commit{Hash: f[0], Subject: f[1], At: f[2]})
			}
		}
		if len(r.Commits) > maxCommits {
			r.Commits, r.More = r.Commits[:maxCommits], true
		}
	}
	return r
}

// gitError is the first line git wrote on stderr, or err.
func gitError(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		line, _, _ := strings.Cut(strings.TrimSpace(string(ee.Stderr)), "\n")
		return line
	}
	return err.Error()
}
