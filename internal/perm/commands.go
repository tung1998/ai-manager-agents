package perm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Pack is a named set of commands (e.g. "Go": go test, go vet…). Packs only
// group commands for picking; a project enables commands one by one.
type Pack struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Icon     string   `json:"icon,omitempty"`
	Commands []string `json:"commands"`
	Custom   bool     `json:"custom,omitempty"`
}

// A command pattern is a command line, optionally ending in " *" for "with any
// arguments". Commands run without a shell, so pipes, redirects, ";", "&&"
// and "$" are never allowed.
const shellChars = ";&|<>$`\\\n\r"

var ErrCommand = errors.New("lệnh không hợp lệ")

// SplitCommand splits a command line into arguments (single/double quotes).
func SplitCommand(line string) ([]string, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.ContainsAny(line, shellChars) {
		return nil, ErrCommand
	}
	var (
		args  []string
		cur   strings.Builder
		quote rune
		has   bool
	)
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == ' ' || r == '\t':
			if cur.Len() > 0 || has {
				args = append(args, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, ErrCommand
	}
	if cur.Len() > 0 || has {
		args = append(args, cur.String())
	}
	return args, nil
}

// CleanPattern normalises a pattern (single spaces) or rejects it.
func CleanPattern(p string) (string, error) {
	p = strings.Join(strings.Fields(p), " ")
	base, _ := strings.CutSuffix(p, " *")
	if _, err := SplitCommand(base); err != nil || strings.Contains(base, "*") {
		return "", ErrCommand
	}
	return p, nil
}

// MatchCommand returns the pattern that allows args, if any.
func MatchCommand(patterns []string, args []string) (string, bool) {
	line := strings.Join(args, " ")
	for _, p := range patterns {
		if base, ok := strings.CutSuffix(p, " *"); ok {
			if line == base || strings.HasPrefix(line, base+" ") {
				return p, true
			}
		} else if line == p {
			return p, true
		}
	}
	return "", false
}

// BuiltinPacks are offered in every project.
var BuiltinPacks = []Pack{
	{ID: "git-read", Label: "Git (đọc)", Icon: "i-lucide-git-branch", Commands: []string{"git status", "git diff *", "git log *", "git show *", "git branch"}},
	{ID: "go", Label: "Go", Icon: "i-simple-icons-go", Commands: []string{"go build ./...", "go vet ./...", "go test ./...", "go test *", "gofmt -l .", "go mod tidy"}},
	{ID: "node", Label: "Node", Icon: "i-simple-icons-nodedotjs", Commands: []string{"npm test", "npm run lint", "npm run build", "npx tsc --noEmit"}},
	{ID: "python", Label: "Python", Icon: "i-simple-icons-python", Commands: []string{"pytest", "pytest *", "ruff check .", "mypy ."}},
	{ID: "rust", Label: "Rust", Icon: "i-simple-icons-rust", Commands: []string{"cargo check", "cargo test", "cargo clippy", "cargo fmt --check"}},
	{ID: "docker-read", Label: "Docker (đọc)", Icon: "i-simple-icons-docker", Commands: []string{"docker compose ps", "docker compose logs *", "docker ps"}},
}

// ProjectPacks: the built-in packs relevant to a project folder, with a
// pack for its package.json scripts, then the project's own packs.
func ProjectPacks(root string, custom []Pack) []Pack {
	has := func(name string) bool {
		if root == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	relevant := map[string]bool{
		"git-read":    has(".git"),
		"go":          has("go.mod"),
		"node":        has("package.json"),
		"python":      has("pyproject.toml") || has("requirements.txt") || has("setup.py"),
		"rust":        has("Cargo.toml"),
		"docker-read": has("docker-compose.yml") || has("compose.yaml") || has("docker-compose.yaml") || has("compose.yml"),
	}
	out := []Pack{}
	if p, ok := scriptsPack(root, has); ok {
		out = append(out, p)
	}
	for _, p := range BuiltinPacks {
		if relevant[p.ID] || root == "" {
			out = append(out, p)
		}
	}
	for _, p := range custom {
		p.Custom = true
		out = append(out, p)
	}
	return out
}

// scriptsPack turns package.json scripts into commands for its package manager.
func scriptsPack(root string, has func(string) bool) (Pack, bool) {
	if root == "" {
		return Pack{}, false
	}
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return Pack{}, false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(raw, &pkg) != nil || len(pkg.Scripts) == 0 {
		return Pack{}, false
	}
	pm := "npm run"
	switch {
	case has("pnpm-lock.yaml"):
		pm = "pnpm run"
	case has("yarn.lock"):
		pm = "yarn run"
	case has("bun.lockb") || has("bun.lock"):
		pm = "bun run"
	}
	names := make([]string, 0, len(pkg.Scripts))
	for n := range pkg.Scripts {
		if !strings.ContainsAny(n, shellChars+" \"'") {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	p := Pack{ID: "scripts", Label: "Script trong package.json", Icon: "i-lucide-file-json"}
	for _, n := range names {
		p.Commands = append(p.Commands, pm+" "+n)
	}
	return p, len(p.Commands) > 0
}
