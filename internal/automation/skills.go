package automation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/prompts"
)

// SkillRef is a skill or a custom command a project's chat can call with
// "/name". Office reads those of every CLI it runs (Claude Code, Codex,
// Gemini CLI, Antigravity) and puts the instructions in the prompt itself, so
// any agent can follow any of them.
type SkillRef struct {
	Name        string `json:"name"` // plugin ones are "<plugin>:<name>", like the CLIs
	Description string `json:"description"`
	Source      string `json:"source"` // project | user | plugin
	Kind        string `json:"kind"`   // skill | command
	Tool        string `json:"tool"`   // claude | codex | gemini | antigravity: whose folder it is in
	Path        string `json:"-"`      // the skill's folder, the command's file
}

// ProjectSkills lists skills and commands usable in a project: the project's
// own, then machine-wide, then plugins'. On a name clash the first wins (a
// skill before a command, Claude Code before the others). projectPath ""
// (machine-wide helper) skips the project's.
func ProjectSkills(home, projectPath string) []SkillRef {
	seen := map[string]bool{}
	var out []SkillRef
	add := func(items []SkillRef, tool, source, prefix string) {
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		for _, it := range items {
			it.Name, it.Source, it.Tool = prefix+it.Name, source, tool
			if seen[it.Name] {
				continue
			}
			seen[it.Name] = true
			out = append(out, it)
		}
	}
	// dir/skills and dir/<commands>
	both := func(dir, commands, tool, source, prefix string) {
		add(skillRefs(filepath.Join(dir, "skills")), tool, source, prefix)
		add(scanCommands(filepath.Join(dir, commands)), tool, source, prefix)
	}
	glob := func(pattern ...string) []string {
		dirs, _ := filepath.Glob(filepath.Join(pattern...))
		sort.Strings(dirs)
		return dirs
	}
	if projectPath != "" {
		both(filepath.Join(projectPath, ".claude"), "commands", "claude", "project", "")
		add(skillRefs(filepath.Join(projectPath, ".agents", "skills")), "codex", "project", "")
		both(filepath.Join(projectPath, ".gemini"), "commands", "gemini", "project", "")
		both(filepath.Join(projectPath, ".agent"), "workflows", "antigravity", "project", "")
	}
	both(filepath.Join(home, ".claude"), "commands", "claude", "user", "")
	// the skills the claude.ai organization syncs to the machine, named as in the CLI
	for _, d := range glob(home, ".claude", "skills", "synced", "*") {
		add(skillRefs(d), "claude", "user", "anthropic-skills:")
	}
	add(skillRefs(filepath.Join(home, ".codex", "skills")), "codex", "user", "")
	add(skillRefs(filepath.Join(home, ".codex", "skills", ".system")), "codex", "user", "")
	add(skillRefs(filepath.Join(home, ".agents", "skills")), "codex", "user", "")
	add(scanCommands(filepath.Join(home, ".codex", "prompts")), "codex", "user", "prompts:")
	both(filepath.Join(home, ".gemini"), "commands", "gemini", "user", "")
	both(filepath.Join(home, ".gemini", "antigravity"), "global_workflows", "antigravity", "user", "")
	for _, d := range glob(home, ".claude", "plugins", "cache", "*", "*", "*") {
		both(d, "commands", "claude", "plugin", pluginName(filepath.Join(d, "skills"))+":")
	}
	// plugins synced from claude.ai: synced/<bucket>/<plugin>[~g<generation>]
	for _, d := range glob(home, ".claude", "plugins", "synced", "*", "*") {
		name, _, _ := strings.Cut(filepath.Base(d), "~")
		both(d, "commands", "claude", "plugin", name+":")
	}
	for _, d := range glob(home, ".codex", "plugins", "cache", "*", "*", "*") {
		both(d, "commands", "codex", "plugin", pluginName(filepath.Join(d, "skills"))+":")
	}
	for _, d := range glob(home, ".gemini", "extensions", "*") {
		both(d, "commands", "gemini", "plugin", "")
	}
	if out == nil {
		out = []SkillRef{}
	}
	return out
}

func skillRefs(dir string) []SkillRef {
	var out []SkillRef
	for _, it := range scanSkills(dir, Location{}) {
		out = append(out, SkillRef{Name: filepath.Base(it.Location.Path), Description: it.Description, Kind: "skill", Path: it.Location.Path})
	}
	return out
}

// scanCommands lists <dir>/**/*.md custom commands (Gemini CLI's are
// .toml); a subfolder joins the name with ":" ("frontend/build.md" is
// "frontend:build").
func scanCommands(dir string) []SkillRef {
	var out []SkillRef
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		ext := filepath.Ext(p)
		if err != nil || d.IsDir() || ext != ".md" && ext != ".toml" {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		name := strings.ReplaceAll(filepath.ToSlash(strings.TrimSuffix(rel, ext)), "/", ":")
		desc, body := commandFile(p)
		if desc == "" { // as Claude Code: the first line of the prompt
			desc, _, _ = strings.Cut(strings.TrimSpace(body), "\n")
			desc = strings.TrimLeft(desc, "# ")
		}
		out = append(out, SkillRef{Name: name, Description: desc, Kind: "command", Path: p})
		return nil
	})
	return out
}

// commandFile reads a command's description and prompt: markdown with a
// frontmatter, or Gemini CLI's TOML (description = "…", prompt = """…""").
func commandFile(p string) (desc, body string) {
	text := readText(p, 32<<10)
	if filepath.Ext(p) != ".toml" {
		fm, body := Frontmatter(text)
		return fm["description"], body
	}
	return tomlString(text, "description"), tomlString(text, "prompt")
}

// tomlString reads a top-level string key, quoted in any of TOML's four ways.
func tomlString(text, key string) string {
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*`).FindStringIndex(text)
	if m == nil {
		return ""
	}
	rest := text[m[1]:]
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if !strings.HasPrefix(rest, q) {
			continue
		}
		v := rest[len(q):]
		end := strings.Index(v, q)
		for q == `"` && end > 0 && v[end-1] == '\\' { // an escaped quote
			if next := strings.Index(v[end+1:], q); next >= 0 {
				end += next + 1
			} else {
				end = -1
			}
		}
		if end >= 0 {
			v = v[:end]
		}
		if q == `"` || q == `"""` {
			v = strings.NewReplacer(`\"`, `"`, `\n`, "\n", `\\`, `\`).Replace(v)
		}
		return strings.TrimPrefix(v, "\n")
	}
	return ""
}

var (
	skillCallRe     = regexp.MustCompile(`^/([A-Za-z0-9][A-Za-z0-9._:-]*)(?:\s+|$)`)
	ErrUnknownSkill = errors.New("không có skill này trong project")
)

// ParseSkillCall splits "/name rest" into name and rest. ok is false when the
// text is not a skill call ("/Users/x" is a path, not a call).
func ParseSkillCall(text string) (name, rest string, ok bool) {
	m := skillCallRe.FindStringSubmatchIndex(text)
	if m == nil {
		return "", text, false
	}
	return text[m[2]:m[3]], strings.TrimSpace(text[m[1]:]), true
}

// ExpandSkillCall turns "/name rest" into a prompt carrying the skill's
// instructions, so every runtime (Claude Code, API, Codex) can follow it.
func ExpandSkillCall(home, projectPath, text string) (string, *SkillRef, error) {
	name, rest, ok := ParseSkillCall(text)
	if !ok {
		return text, nil, nil
	}
	var skill *SkillRef
	for _, s := range ProjectSkills(home, projectPath) {
		if s.Name == name {
			s := s
			skill = &s
			break
		}
	}
	if skill == nil {
		return "", nil, fmt.Errorf("%w: /%s", ErrUnknownSkill, name)
	}
	if skill.Kind == "command" {
		_, body := commandFile(skill.Path)
		return prompts.Render("chat/command-call", map[string]any{"Name": name, "File": skill.Path, "Body": strings.TrimSpace(commandArgs(body, rest)), "Request": rest}), skill, nil
	}
	body := readText(filepath.Join(skill.Path, "SKILL.md"), 32<<10)
	var others []string
	_ = filepath.WalkDir(skill.Path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(others) >= 30 {
			return nil
		}
		if rel, _ := filepath.Rel(skill.Path, p); rel != "SKILL.md" {
			others = append(others, filepath.ToSlash(rel))
		}
		return nil
	})
	if rest == "" {
		rest = "(no further request; follow the skill)"
	}
	return prompts.Render("chat/skill-call", map[string]any{"Name": name, "Dir": skill.Path, "Body": strings.TrimSpace(body), "Others": others, "Request": rest}), skill, nil
}

var argRe = regexp.MustCompile(`\$(ARGUMENTS|[1-9])|\{\{args\}\}`)

// commandArgs fills a command's $ARGUMENTS or {{args}} (Gemini CLI) with the
// whole request and $1..$9 with its words, as the CLIs do.
func commandArgs(body, rest string) string {
	words := strings.Fields(rest)
	return argRe.ReplaceAllStringFunc(body, func(m string) string {
		if m == "$ARGUMENTS" || m == "{{args}}" {
			return rest
		}
		if i := int(m[1] - '1'); i < len(words) {
			return words[i]
		}
		return ""
	})
}
