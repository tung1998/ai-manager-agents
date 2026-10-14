package perm

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// risky: commands that delete or publish — always asked, never allowed for good.
var risky = regexp.MustCompile(`(?i)\brm\s|\bgit\s+(push|reset\s+--hard|clean|branch\s+-D)|\bdrop\s|\bdelete\b|\btruncate\b`)

// Risky reports a command line that deletes or publishes. git's global
// options are skipped first: "git -C . push" is a push.
func Risky(line string) bool {
	return risky.MatchString(line) || risky.MatchString(skipGitGlobals(line))
}

// gitValued: git global options whose value is the next word.
var gitValued = map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--super-prefix": true, "--config-env": true}

// skipGitGlobals drops the options between git and its subcommand
// ("git -c x=y --no-pager reset --hard" → "git reset --hard").
func skipGitGlobals(line string) string {
	f := strings.Fields(line)
	out := make([]string, 0, len(f))
	for i := 0; i < len(f); i++ {
		out = append(out, f[i])
		if strings.ToLower(filepath.Base(f[i])) != "git" {
			continue
		}
		for i+1 < len(f) && strings.HasPrefix(f[i+1], "-") {
			if gitValued[f[i+1]] {
				i++
			}
			i++
		}
	}
	return strings.Join(out, " ")
}

// wrappers run another command: "luôn cho phép" one would allow anything.
var wrappers = map[string]bool{"sudo": true, "env": true, "xargs": true, "bash": true, "sh": true, "zsh": true,
	"eval": true, "exec": true, "nohup": true, "time": true, "timeout": true, "nice": true, "watch": true, "su": true, "doas": true}

// wrapper: prog is one, by any path or case ("/bin/bash", "SUDO").
func wrapper(prog string) bool { return wrappers[strings.ToLower(filepath.Base(prog))] }

// ErrNotAlways: a command that may not be allowed for good.
var ErrNotAlways = errors.New("lệnh này không được luôn cho phép (xóa, push, hoặc chạy lệnh khác bên trong)")

// SuggestPattern is the pattern "luôn cho phép" adds for a command: its first
// two words with " *" when it has more ("git switch main" → "git switch *"),
// else the command itself. A flag as second word, or a pattern that would
// also cover a risky command (git reset * → git reset --hard), keeps the exact
// command. Risky commands and wrappers get none.
func SuggestPattern(line string) (string, bool) {
	args, err := SplitCommand(line)
	if err != nil || len(args) == 0 || Risky(line) || wrapper(args[0]) {
		return "", false
	}
	exact, err := CleanPattern(strings.Join(args, " "))
	if err != nil || strings.ContainsAny(strings.Join(args, ""), " \t") {
		return "", false // quoted arguments with spaces don't round-trip
	}
	if len(args) <= 2 || strings.HasPrefix(args[1], "-") {
		return exact, true
	}
	p, err := CleanPattern(args[0] + " " + args[1] + " *")
	if err != nil || CoversRisky(p) {
		return exact, true
	}
	return p, true
}

// CoversRisky: the pattern allows a risky command (or a wrapper's).
func CoversRisky(pattern string) bool {
	if wrapper(program(pattern)) {
		return true
	}
	base, wild := strings.CutSuffix(pattern, " *")
	if !wild {
		return Risky(pattern)
	}
	// " x push": x fills a global option that takes a value ("git -c *")
	for _, more := range []string{" --hard", " -D", " x", " push", " reset --hard", " clean -fd", " branch -D"} {
		if Risky(base+more) || Risky(base+" x"+more) {
			return true
		}
	}
	return false
}

// Allowed is what "luôn cho phép" did.
type Allowed struct {
	Pattern string `json:"pattern"`
	Pack    string `json:"pack"`     // the label of the pack that has it
	NewPack bool   `json:"new_pack"` // the pack was made for it
	Added   bool   `json:"added"`    // the pattern was added to the project's packs
	// Auto: the agent runs it on its own from now on (it has commands.run);
	// without it, only in its own worktree.
	Auto  bool   `json:"auto"`
	Error string `json:"error,omitempty"` // the command ran, the permission was not saved
}

// program is a command's program, the first word.
func program(c string) string {
	f := strings.Fields(c)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

var notID = regexp.MustCompile(`[^a-z0-9-]+`)

func autoPackID(prog string) string {
	id := strings.Trim(notID.ReplaceAllString(strings.ToLower(prog), "-"), "-")
	if id == "" {
		id = "cmd"
	}
	return "auto-" + id
}

// PlanAlways says which pack a pattern goes to: the one that has it already,
// a project pack with a command of the same program, or a new one (index -1).
func PlanAlways(root string, p Policy, pattern string) (label string, index int, isNew bool) {
	if slices.Contains(p.Catalog, pattern) {
		for _, x := range ProjectPacks(root, p.Packs) {
			if slices.Contains(x.Commands, pattern) {
				return x.Label, -2, false
			}
		}
	}
	prog := program(pattern)
	for i, x := range p.Packs {
		if x.ID == autoPackID(prog) || slices.ContainsFunc(x.Commands, func(c string) bool { return program(c) == prog }) {
			return x.Label, i, false
		}
	}
	return prog + " (đã duyệt)", -1, true
}

// AllowAlways lets an agent run pattern on its own from now on: the pattern
// joins the project's packs (if none has it) and the agent's commands. An
// agent on the default picks (nil) keeps them: they become its own list.
func AllowAlways(ctx context.Context, st storage.Store, projectID, agentID, pattern string) (Allowed, error) {
	pattern, err := CleanPattern(pattern)
	if err != nil {
		return Allowed{}, err
	}
	if CoversRisky(pattern) {
		return Allowed{}, ErrNotAlways
	}
	ag, err := st.Agents().Get(ctx, agentID)
	if err != nil {
		return Allowed{}, err
	}
	root := ""
	if r, err := st.Repos().Get(ctx, projectID); err == nil {
		root = r.Path
	}
	out := Allowed{Pattern: pattern}
	pol, err := UpdatePolicy(ctx, st, projectID, func(pol *Policy) (bool, error) {
		var i int
		out.Added = false // a retry starts over from the re-read policy
		out.Pack, i, out.NewPack = PlanAlways(root, *pol, pattern)
		if i == -2 { // already in the catalog
			return false, nil
		}
		if out.NewPack {
			pol.Packs = append(pol.Packs, Pack{ID: autoPackID(program(pattern)), Label: out.Pack, Icon: "i-lucide-terminal", Commands: []string{pattern}, Custom: true})
		} else {
			pol.Packs[i].Commands = append(pol.Packs[i].Commands, pattern)
		}
		out.Added = true
		return true, nil
	})
	if err != nil {
		return out, err
	}
	if out.Added {
		pol = LoadPolicy(ctx, st, projectID) // Catalog/Safe as saved
	}
	picks := slices.Clone(pol.Safe)
	if ag.Permissions.Commands != nil {
		picks = slices.Clone(*ag.Permissions.Commands)
	}
	if !slices.Contains(picks, pattern) {
		picks = append(picks, pattern)
		ag.Permissions.Commands = &picks
		if err := st.Agents().Update(ctx, ag); err != nil {
			return out, err
		}
	}
	out.Auto = Resolve(ag, Operate, pol).Can(CapCommands)
	return out, nil
}
