package burn

import (
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A Burn's template (ADR-128) is what its workers look for, and how a piece
// is checked; the person picks one, or writes their own (custom). The rest is
// the same whatever the template: worktrees, claim, do, verify, report, review.

// Templates are the templates a Burn can take.
var Templates = []string{"general", "ux", "ideas", "security", "performance", "test", "docs", "custom"}

// templateLabel names a template in the Burn's log.
var templateLabel = map[string]string{"general": "Tổng quát", "ux": "Trải nghiệm & mobile", "ideas": "Ý tưởng & tích hợp",
	"security": "Bảo mật", "performance": "Hiệu năng", "test": "Test", "docs": "Tài liệu", "custom": "Tùy chỉnh"}

// ValidTemplate: t is a template a Burn can take.
func ValidTemplate(t string) bool { return slices.Contains(Templates, t) }

// templateOf is b's template ("" is the general one).
func templateOf(b storage.BurnSession) string {
	if b.Template == "" {
		return "general"
	}
	return b.Template
}

// lensOf is the lens that is b's template, if it is one.
func lensOf(b storage.BurnSession) (lens, bool) {
	i := slices.IndexFunc(lenses, func(l lens) bool { return l.key == templateOf(b) })
	if i < 0 {
		return lens{}, false
	}
	return lenses[i], true
}

// hunt is what b's workers look for; "" for the general template (its
// order: roadmap, bugs, upgrades).
func hunt(b storage.BurnSession) string {
	switch t := templateOf(b); t {
	case "general":
		return ""
	case "custom":
		return strings.TrimSpace(b.HuntPrompt)
	case "ux", "ideas":
		return prompts.Text("burn/hunt-" + t)
	}
	if l, ok := lensOf(b); ok {
		return "Go through the project for this, the parts that matter most first: " + l.look + "."
	}
	return ""
}

// looks is what the focus adds to look at, beside the template's own.
func looks(b storage.BurnSession) []string {
	var out []string
	for _, l := range focusLenses(b.Focus) {
		if l.key != templateOf(b) {
			out = append(out, l.look)
		}
	}
	return out
}

// checks is how a piece of b is verified: its template's, then its focus's.
func checks(b storage.BurnSession) []string {
	var out []string
	if l, ok := lensOf(b); ok {
		out = append(out, l.check)
	}
	for _, l := range focusLenses(b.Focus) {
		if l.key != templateOf(b) {
			out = append(out, l.check)
		}
	}
	return out
}
