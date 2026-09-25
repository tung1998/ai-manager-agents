// Package perm defines what agents may do, as nested packages: each level
// includes everything below it. What an agent may do in a run is the lowest
// of its own level, the mode chosen for the chat/task, and the project's cap.
package perm

import (
	"context"
	"path"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Levels, lowest first.
const (
	Read    = "read"    // read code, logs, operations state
	Propose = "propose" // + propose diffs and operations, a person approves
	Check   = "check"   // + run the project's allowed check commands on its own
	Edit    = "edit"    // + apply clean diffs on its own (never denied files)
	Operate = "operate" // + run/restart allowed processes and containers on its own
)

// Info describes a level for the dashboard.
type Info struct {
	Level       string `json:"level"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// All lists the levels in order.
var All = []Info{
	{Read, "Chỉ đọc", "Đọc code, log, trạng thái vận hành"},
	{Propose, "Đề xuất", "Đề xuất sửa code và thao tác, người duyệt mới làm"},
	{Check, "Tự kiểm tra", "Tự chạy lệnh kiểm tra được phép (test, typecheck, lint, build)"},
	{Edit, "Tự sửa code", "Tự áp diff áp được sạch, trừ file cấm"},
	{Operate, "Vận hành", "Tự chạy lại tiến trình và container được phép"},
}

// Rank orders levels; unknown ones rank as Propose (the safe default).
func Rank(level string) int {
	for i, x := range All {
		if x.Level == level {
			return i
		}
	}
	return 1
}

// Valid reports a known level.
func Valid(level string) bool {
	for _, x := range All {
		if x.Level == level {
			return true
		}
	}
	return false
}

// Label is the Vietnamese name of a level.
func Label(level string) string { return All[Rank(level)].Label }

// Min returns the lowest of the given levels (empty ones are ignored).
func Min(levels ...string) string {
	out := Operate
	for _, l := range levels {
		if l != "" && Rank(l) < Rank(out) {
			out = l
		}
	}
	return out
}

// AtLeast reports whether level includes want.
func AtLeast(level, want string) bool { return Rank(level) >= Rank(want) }

// Agent returns an agent's own level: its package, or with its own picks of
// capabilities, the lowest level that includes all of them.
func Agent(a storage.Agent) string {
	if a.Permissions.Caps != nil {
		out := Read
		for _, id := range *a.Permissions.Caps {
			if c, ok := capInfo(id); ok && Rank(c.Min) > Rank(out) {
				out = c.Min
			}
		}
		return out
	}
	if Valid(a.Permissions.Level) {
		return a.Permissions.Level
	}
	if a.Permissions.ReadOnly {
		return Read
	}
	return Propose
}

// Policy is a project's limits on agents.
type Policy struct {
	MaxLevel          string   `json:"max_level"`          // cap for every agent and mode
	AllowedCommands   []string `json:"allowed_commands"`   // process ids agents may run on their own (Check+)
	Commands          []string `json:"commands"`           // command patterns agents may run on their own (commands.run)
	Packs             []Pack   `json:"packs"`              // the project's own command packs
	AllowedContainers []string `json:"allowed_containers"` // compose services agents may restart on their own (Operate)
	DenyPaths         []string `json:"deny_paths"`         // files no diff may touch, at any level
}

// DefaultPolicy keeps agents at "propose" and protects secrets.
func DefaultPolicy() Policy {
	return Policy{MaxLevel: Propose, AllowedCommands: []string{}, Commands: []string{}, Packs: []Pack{}, AllowedContainers: []string{},
		DenyPaths: []string{".env", ".env.*", "**/.env", "**/*.pem", "**/*.key"}}
}

func policyKey(projectID string) string { return "policy:" + projectID }

// LoadPolicy reads a project's policy (the default when none is saved).
func LoadPolicy(ctx context.Context, st storage.Store, projectID string) Policy {
	p := DefaultPolicy()
	if ok, err := st.Settings().Get(ctx, policyKey(projectID), &p); err != nil || !ok {
		return DefaultPolicy()
	}
	if !Valid(p.MaxLevel) {
		p.MaxLevel = Propose
	}
	if p.Commands == nil {
		p.Commands = []string{}
	}
	if p.Packs == nil {
		p.Packs = []Pack{}
	}
	return p
}

// SavePolicy stores a project's policy.
func SavePolicy(ctx context.Context, st storage.Store, projectID string, p Policy) error {
	return st.Settings().Set(ctx, policyKey(projectID), p)
}

// Denied returns the files of a diff that the policy forbids.
func (p Policy) Denied(files []string) []string {
	var out []string
	for _, f := range files {
		f = strings.TrimPrefix(f, "./")
		for _, pat := range p.DenyPaths {
			if matchPath(strings.TrimSpace(pat), f) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// matchPath supports shell globs, "**/" for any folder depth, and a trailing
// "/" for a whole folder.
func matchPath(pat, file string) bool {
	if pat == "" {
		return false
	}
	if strings.HasSuffix(pat, "/") {
		return strings.HasPrefix(file, pat) || strings.Contains(file, "/"+pat)
	}
	if rest, ok := strings.CutPrefix(pat, "**/"); ok {
		if ok, _ := path.Match(rest, path.Base(file)); ok {
			return true
		}
		for i := range file {
			if file[i] == '/' {
				if ok, _ := path.Match(rest, file[i+1:]); ok {
					return true
				}
			}
		}
		return false
	}
	ok, _ := path.Match(pat, file)
	return ok
}

// Effective is what an agent may do in one run.
func Effective(a storage.Agent, mode string, p Policy) string {
	return Min(Agent(a), mode, p.MaxLevel)
}
