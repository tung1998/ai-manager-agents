package automation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Library is the office's own collection of skills, agents, MCP and automation templates,
// kept as plain files under <office home>/library so it can be versioned,
// copied or exported:
//
//	library/skills/<name>/SKILL.md (+ files)
//	library/agents/<name>.md
//	library/mcp/<name>.json   (an MCPTemplate)
type Library struct {
	Dir   string
	Trash string
}

// LibraryItem is an entry in the library.
type LibraryItem struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Source      string            `json:"source,omitempty"`   // skill: frontmatter "source", set by catalogs imported from elsewhere
	Category    string            `json:"category,omitempty"` // skill: frontmatter "category", the catalog's own
	Area        string            `json:"area,omitempty"`     // skill: the work area of Category (SkillArea)
	Tags        []string          `json:"tags,omitempty"`     // skill: frontmatter "tags"
	Risk        string            `json:"risk,omitempty"`     // skill: frontmatter "risk" (safe, critical, offensive…)
	UpdatedAt   time.Time         `json:"updated_at"`
	Files       map[string]string `json:"files,omitempty"`               // skill files / agent: {"<name>.md": content}
	Template    *MCPTemplate      `json:"template,omitempty"`            // mcp
	Automation  *AutomationPreset `json:"automation_template,omitempty"` // reusable automation prompt
}

// AutomationPreset is a reusable starter for a project automation. Agent IDs
// and other project-specific settings are intentionally not part of it.
type AutomationPreset struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Prompt      string `json:"prompt"`
	Source      string `json:"source"`
}

var ErrNotFound = errors.New("không tìm thấy")

func (l Library) sub(kind string) string {
	return filepath.Join(l.Dir, map[string]string{"skill": "skills", "agent": "agents", "mcp": "mcp", "template": "automation-templates"}[kind])
}

func (l Library) path(kind, name string) string {
	switch kind {
	case "skill":
		return filepath.Join(l.sub(kind), name)
	case "agent":
		return filepath.Join(l.sub(kind), name+".md")
	}
	return filepath.Join(l.sub(kind), name+".json")
}

// List returns entries of a kind (without file contents).
func (l Library) List(kind string) ([]LibraryItem, error) {
	entries, err := os.ReadDir(l.sub(kind))
	if errors.Is(err, os.ErrNotExist) {
		return []LibraryItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []LibraryItem{}
	for _, e := range entries {
		name := e.Name()
		switch kind {
		case "skill":
			// only SKILL.md: the library may hold thousands of skills
			if !e.IsDir() || !nameRe.MatchString(name) {
				continue
			}
			if it, ok := skillHead(filepath.Join(l.sub(kind), name)); ok {
				out = append(out, it)
			}
			continue
		case "agent":
			if e.IsDir() || !strings.HasSuffix(name, ".md") {
				continue
			}
			name = strings.TrimSuffix(name, ".md")
		case "mcp", "template":
			if e.IsDir() || !strings.HasSuffix(name, ".json") {
				continue
			}
			name = strings.TrimSuffix(name, ".json")
		}
		it, err := l.Get(kind, name)
		if err != nil {
			continue
		}
		if kind != "mcp" && kind != "template" {
			it.Files = nil
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// skillHeads caches the frontmatter of library skills by SKILL.md path, kept
// while the file's size and time are unchanged: listing thousands of skills
// then only stats them.
var skillHeads = struct {
	sync.Mutex
	m map[string]skillHeadEntry
}{m: map[string]skillHeadEntry{}}

type skillHeadEntry struct {
	mod  time.Time
	size int64
	item LibraryItem
}

func skillHead(dir string) (LibraryItem, bool) {
	p := filepath.Join(dir, "SKILL.md")
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return LibraryItem{}, false
	}
	skillHeads.Lock()
	c, ok := skillHeads.m[p]
	skillHeads.Unlock()
	if ok && c.mod.Equal(st.ModTime()) && c.size == st.Size() {
		return c.item, true
	}
	raw := readText(p, 32<<10)
	if raw == "" {
		return LibraryItem{}, false
	}
	fm, _ := Frontmatter(raw)
	it := LibraryItem{Kind: "skill", Name: filepath.Base(dir), Description: fm["description"], Source: fm["source"],
		Category: fm["category"], Area: SkillArea(fm["category"]), Tags: frontmatterList(raw, "tags"), Risk: fm["risk"], UpdatedAt: st.ModTime().UTC()}
	skillHeads.Lock()
	skillHeads.m[p] = skillHeadEntry{mod: st.ModTime(), size: st.Size(), item: it}
	skillHeads.Unlock()
	return it, true
}

// frontmatterList reads a list key of a Markdown header, inline
// (`tags: [a, b]`) or a YAML block of `- a` lines.
func frontmatterList(md, key string) []string {
	m := frontmatterRe.FindStringSubmatch(md)
	if m == nil {
		return nil
	}
	var out []string
	add := func(v string) {
		if v = strings.Trim(strings.TrimSpace(v), `"'`); v != "" {
			out = append(out, v)
		}
	}
	lines := strings.Split(m[1], "\n")
	for i, line := range lines {
		v, ok := strings.CutPrefix(line, key+":")
		if !ok {
			continue
		}
		if v = strings.TrimSpace(v); strings.HasPrefix(v, "[") {
			for _, t := range strings.Split(strings.Trim(v, "[]"), ",") {
				add(t)
			}
			return out
		}
		for _, l := range lines[i+1:] {
			t, ok := strings.CutPrefix(strings.TrimSpace(l), "- ")
			if !ok {
				break
			}
			add(t)
		}
		return out
	}
	return nil
}

// skillAreas sorts a catalog's categories into the office's work areas
// (ADR-090, plus design and ops); a category not here is "other".
var skillAreas = map[string][]string{
	"code": {"development", "web-development", "backend", "frontend", "front-end", "fullstack", "mobile", "testing", "test-automation",
		"code-quality", "code", "coding", "core-dev", "super-code", "engineering", "architecture", "api-integration", "database", "databases",
		"database-processing", "framework", "developer-tools", "debugging", "development-and-testing", "game-development", "blockchain", "web",
		"app-builder", "ai-ml", "ai", "ai-agents", "ai-testing", "ai-research", "agent-orchestration", "agent-tooling", "agent-evaluation",
		"agent-behavior", "agent-squad", "orchestration", "mcp", "memory", "context-optimization", "prompt-engineering", "ml-ops", "data-ai",
		"skill-authoring", "tool-quality", "reverse-engineering", "voice-agents", "browser-automation"},
	"ops":      {"devops", "cloud", "reliability", "security", "pentest-tools", "operations", "automation", "workflow", "workflow-bundle", "granular-workflow-bundle"},
	"data":     {"data", "data-science", "data-engineering", "analysis", "spreadsheet-processing", "research", "science", "reasoning"},
	"design":   {"design", "design-it", "graphics-processing", "creative"},
	"plan":     {"business", "business-strategy", "project-management", "product", "product-management", "planning", "finance", "legal", "consulting", "productivity", "office-productivity", "knowledge-management", "collaboration", "document-processing", "document-verification", "presentation-processing"},
	"customer": {"ecommerce", "shopping", "growth"},
	"content":  {"marketing", "seo", "content", "content-writing", "writing", "media", "media-processing", "video", "education"},
}

var skillAreaOf = func() map[string]string {
	m := map[string]string{}
	for area, cats := range skillAreas {
		for _, c := range cats {
			m[c] = area
		}
	}
	return m
}()

// SkillArea is the work area of a skill's category; "" without a category.
func SkillArea(category string) string {
	if category == "" {
		return ""
	}
	if a, ok := skillAreaOf[strings.ToLower(category)]; ok {
		return a
	}
	return "other"
}

// Get loads one entry with its contents.
func (l Library) Get(kind, name string) (LibraryItem, error) {
	if !nameRe.MatchString(name) {
		return LibraryItem{}, ErrBadName
	}
	p := l.path(kind, name)
	st, err := os.Stat(p)
	if err != nil {
		return LibraryItem{}, ErrNotFound
	}
	it := LibraryItem{Kind: kind, Name: name, UpdatedAt: st.ModTime().UTC()}
	switch kind {
	case "skill":
		files, err := ReadSkill(p)
		if err != nil {
			return it, err
		}
		fm, _ := Frontmatter(files["SKILL.md"])
		it.Description, it.Source, it.Files = fm["description"], fm["source"], files
		it.Category, it.Area, it.Tags, it.Risk = fm["category"], SkillArea(fm["category"]), frontmatterList(files["SKILL.md"], "tags"), fm["risk"]
	case "agent":
		raw, err := os.ReadFile(p)
		if err != nil {
			return it, err
		}
		fm, _ := Frontmatter(string(raw))
		it.Description, it.Files = fm["description"], map[string]string{name + ".md": string(raw)}
	case "mcp":
		raw, err := os.ReadFile(p)
		if err != nil {
			return it, err
		}
		var t MCPTemplate
		if err := json.Unmarshal(raw, &t); err != nil {
			return it, err
		}
		t.Source = "library"
		if t.Inputs == nil {
			t.Inputs = []Input{}
		}
		it.Description, it.Template = t.Description, &t
	case "template":
		raw, err := os.ReadFile(p)
		if err != nil {
			return it, err
		}
		var t AutomationPreset
		if err := json.Unmarshal(raw, &t); err != nil {
			return it, err
		}
		t.Key = name
		it.Description, it.Automation = t.Description, &t
	}
	return it, nil
}

// SaveSkill replaces a skill folder.
func (l Library) SaveSkill(name string, files map[string]string) error {
	if !nameRe.MatchString(name) {
		return ErrBadName
	}
	in := Installer{Trash: l.Trash}
	dir := l.path("skill", name)
	if _, err := os.Stat(dir); err == nil {
		if _, err := in.toTrash(dir, "library-skill-"+name); err != nil {
			return err
		}
	}
	// Reuse the installer's file writer with a synthetic target.
	if err := os.MkdirAll(l.sub("skill"), 0o755); err != nil {
		return err
	}
	return writeFiles(dir, files)
}

// SaveAgent writes an agent file.
func (l Library) SaveAgent(name, content string) error {
	if !nameRe.MatchString(name) {
		return ErrBadName
	}
	if err := os.MkdirAll(l.sub("agent"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.path("agent", name), []byte(content), 0o644)
}

// SaveMCP writes an MCP template. Stored values must be placeholders, not
// secrets: the caller strips secret inputs before saving.
func (l Library) SaveMCP(t MCPTemplate) error {
	if !nameRe.MatchString(t.Name) {
		return ErrBadName
	}
	if t.Config == nil {
		return errors.New("thiếu cấu hình MCP")
	}
	t.Source = ""
	if err := os.MkdirAll(l.sub("mcp"), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(l.path("mcp", t.Name), append(raw, '\n'), 0o644)
}

// SaveAutomationPreset writes a reusable prompt template. It stores only
// project-independent data; the selected project supplies its own agent and
// automation settings when the template is used.
func (l Library) SaveAutomationPreset(key string, t AutomationPreset) error {
	if !nameRe.MatchString(key) {
		return ErrBadName
	}
	if strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Description) == "" || strings.TrimSpace(t.Category) == "" || strings.TrimSpace(t.Prompt) == "" {
		return errors.New("tên, mô tả, nhóm và nội dung mẫu là bắt buộc")
	}
	if utf8.RuneCountInString(t.Name) > 120 || utf8.RuneCountInString(t.Description) > 500 || utf8.RuneCountInString(t.Category) > 80 || len(t.Prompt) > 100_000 {
		return errors.New("mẫu tự động hóa vượt quá giới hạn độ dài")
	}
	if t.Source != "schedule" && t.Source != "webhook" {
		return errors.New("nguồn tự động hóa không hợp lệ")
	}
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	t.Category = strings.TrimSpace(t.Category)
	t.Key = key
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.sub("template"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.path("template", key), append(raw, '\n'), 0o644)
}

// Delete moves an entry to trash.
func (l Library) Delete(kind, name string) error {
	if !nameRe.MatchString(name) {
		return ErrBadName
	}
	p := l.path(kind, name)
	if _, err := os.Stat(p); err != nil {
		return ErrNotFound
	}
	_, err := Installer{Trash: l.Trash}.toTrash(p, "library-"+kind+"-"+name)
	return err
}
