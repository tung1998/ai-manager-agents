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

// SkillRef is a skill or a custom command (.claude/commands/*.md) a
// project's chat can call with "/name".
type SkillRef struct {
	Name        string `json:"name"` // plugin ones are "<plugin>:<name>", like Claude Code
	Description string `json:"description"`
	Source      string `json:"source"` // project | user | plugin
	Kind        string `json:"kind"`   // skill | command
	Path        string `json:"-"`      // the skill's folder, the command's file
}

// ProjectSkills lists skills and commands usable in a project: the project's
// own, then machine-wide, then plugins'. On a name clash the first wins (a
// skill before a command), as in Claude Code. projectPath "" (machine-wide
// helper) skips the project's.
func ProjectSkills(home, projectPath string) []SkillRef {
	seen := map[string]bool{}
	var out []SkillRef
	add := func(items []SkillRef, source, prefix string) {
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		for _, it := range items {
			it.Name, it.Source = prefix+it.Name, source
			if seen[it.Name] {
				continue
			}
			seen[it.Name] = true
			out = append(out, it)
		}
	}
	both := func(dir, source, prefix string) {
		add(skillRefs(filepath.Join(dir, "skills")), source, prefix)
		add(scanCommands(filepath.Join(dir, "commands")), source, prefix)
	}
	if projectPath != "" {
		both(filepath.Join(projectPath, ".claude"), "project", "")
	}
	both(filepath.Join(home, ".claude"), "user", "")
	dirs, _ := filepath.Glob(filepath.Join(home, ".claude", "plugins", "cache", "*", "*", "*"))
	sort.Strings(dirs)
	for _, d := range dirs {
		both(d, "plugin", pluginName(filepath.Join(d, "skills"))+":")
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

// scanCommands lists <dir>/**/*.md custom commands; a subfolder joins the
// name with ":" ("frontend/build.md" is "frontend:build").
func scanCommands(dir string) []SkillRef {
	var out []SkillRef
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		name := strings.ReplaceAll(filepath.ToSlash(strings.TrimSuffix(rel, ".md")), "/", ":")
		fm, body := Frontmatter(readText(p, 16<<10))
		desc := fm["description"]
		if desc == "" { // as Claude Code: the first line of the prompt
			desc, _, _ = strings.Cut(strings.TrimSpace(body), "\n")
			desc = strings.TrimLeft(desc, "# ")
		}
		out = append(out, SkillRef{Name: name, Description: desc, Kind: "command", Path: p})
		return nil
	})
	return out
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
		_, body := Frontmatter(readText(skill.Path, 32<<10))
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

var argRe = regexp.MustCompile(`\$(ARGUMENTS|[1-9])`)

// commandArgs fills a command's $ARGUMENTS (the whole request) and $1..$9
// (its words), as Claude Code does.
func commandArgs(body, rest string) string {
	words := strings.Fields(rest)
	return argRe.ReplaceAllStringFunc(body, func(m string) string {
		if m == "$ARGUMENTS" {
			return rest
		}
		if i := int(m[1] - '1'); i < len(words) {
			return words[i]
		}
		return ""
	})
}
