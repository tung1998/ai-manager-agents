package support

import (
	"context"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Case types (ADR-174): a project's kinds of matter, each with the words
// that name it, how its answers go out and its SLA. Settings, no code.

// CaseType is one kind of matter.
type CaseType struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Keywords    []string `json:"keywords"`    // any of them in the message (accents and case ignored)
	Description string   `json:"description"` // for the agent: when a message is this case
	Mode        string   `json:"mode"`        // "" = the source's; auto, approve, human (no AI answer)
	SLAMinutes  int      `json:"sla_minutes"` // 0 = the pack's
}

// Settings are the pack's settings in a project.
type Settings struct {
	Cases      []CaseType `json:"cases"`
	SLAMinutes int        `json:"sla_minutes"` // the first answer is due within (0 = no SLA)
	// HumanWords: a customer asking for a person (the bot stops on the ticket)
	HumanWords []string `json:"human_words"`
}

func settingsKey(projectID string) string { return "support:" + projectID }

// DefaultHumanWords ask for a person.
var DefaultHumanWords = []string{"gặp người", "gặp nhân viên", "nói chuyện với người", "nhân viên thật", "talk to a human", "real person"}

// LoadSettings reads a project's settings (defaults when none).
func (s *Service) LoadSettings(ctx context.Context, projectID string) Settings {
	st := Settings{SLAMinutes: 60, HumanWords: DefaultHumanWords}
	_, _ = s.store.Settings().Get(ctx, settingsKey(projectID), &st)
	if st.Cases == nil {
		st.Cases = []CaseType{}
	}
	return st
}

// SaveSettings checks and stores a project's settings.
func (s *Service) SaveSettings(ctx context.Context, projectID string, st Settings) (Settings, error) {
	seen := map[string]bool{}
	var cases []CaseType
	for _, c := range st.Cases {
		c.Key, c.Name = strings.TrimSpace(c.Key), strings.TrimSpace(c.Name)
		switch {
		case c.Key == "":
			return st, invalid("loại case cần key")
		case seen[c.Key]:
			return st, invalid("loại case %q trùng", c.Key)
		case c.Mode != "" && c.Mode != "auto" && c.Mode != "approve" && c.Mode != "human":
			return st, invalid("loại case %q: chế độ phải là auto, approve hoặc human", c.Key)
		case c.SLAMinutes < 0:
			return st, invalid("SLA không âm")
		}
		seen[c.Key] = true
		c.Keywords = clean(c.Keywords)
		if c.Name == "" {
			c.Name = c.Key
		}
		cases = append(cases, c)
	}
	if st.SLAMinutes < 0 {
		return st, invalid("SLA không âm")
	}
	if st.HumanWords == nil { // not sent: the defaults stay
		st.HumanWords = DefaultHumanWords
	}
	st.Cases, st.HumanWords = cases, clean(st.HumanWords)
	if st.Cases == nil {
		st.Cases = []CaseType{}
	}
	return st, s.store.Settings().Set(ctx, settingsKey(projectID), st)
}

func clean(words []string) []string {
	out := []string{}
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// fold: lower case, no accents (đ as d), for matching words.
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, _ := transform.String(t, strings.ToLower(s))
	return strings.ReplaceAll(out, "đ", "d")
}

func has(text string, words []string) bool {
	f := fold(text)
	for _, w := range words {
		if w = fold(w); w != "" && strings.Contains(f, w) {
			return true
		}
	}
	return false
}

// classify: the first case whose words the message has ("" = none).
func classify(st Settings, text string) string {
	for _, c := range st.Cases {
		if has(text, c.Keywords) {
			return c.Key
		}
	}
	return ""
}

func (st Settings) caseOf(key string) (CaseType, bool) {
	for _, c := range st.Cases {
		if c.Key == key {
			return c, true
		}
	}
	return CaseType{}, false
}

// mode: how a ticket's answers go out: its case's, else its source's.
func (st Settings) mode(t storage.SupportTicket, src storage.SupportSource) string {
	if c, ok := st.caseOf(t.CaseType); ok && c.Mode != "" {
		return c.Mode
	}
	return src.Mode
}

// sla: minutes for a ticket's first answer (0 = none).
func (st Settings) sla(caseKey string) int {
	if c, ok := st.caseOf(caseKey); ok && c.SLAMinutes > 0 {
		return c.SLAMinutes
	}
	return st.SLAMinutes
}
