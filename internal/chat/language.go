package chat

import (
	"context"

	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Language is how agents speak (ADR-121): office's own prompts, and what
// agents say to each other, are in English; to the person, the language they
// write in (or the one set here), else office's language.
type Language struct {
	System   string `json:"system"`   // office's language: vi | en
	Response string `json:"response"` // to the person: auto (theirs) or a language code
}

// LanguageKey is where the setting is kept.
const LanguageKey = "language"

// Languages are the ones offered, by code.
var Languages = map[string]string{"vi": "Vietnamese", "en": "English", "ja": "Japanese", "ko": "Korean", "zh": "Chinese",
	"fr": "French", "de": "German", "es": "Spanish", "th": "Thai", "id": "Indonesian"}

// LoadLanguage reads the setting (Vietnamese, the person's own, by default).
func LoadLanguage(ctx context.Context, st storage.Store) Language {
	var l Language
	_, _ = st.Settings().Get(ctx, LanguageKey, &l)
	return l.Clean()
}

// Clean keeps known values only.
func (l Language) Clean() Language {
	if l.System != "en" {
		l.System = "vi"
	}
	if _, ok := Languages[l.Response]; !ok {
		l.Response = "auto"
	}
	return l
}

// Rule is the language part of an agent's instructions.
func (l Language) Rule() string {
	l = l.Clean()
	return prompts.Render("chat/language", map[string]any{"Auto": l.Response == "auto", "System": Languages[l.System], "Response": Languages[l.Response]}) + "\n"
}
