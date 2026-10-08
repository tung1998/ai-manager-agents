package api

import (
	"net/http"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/chat"
)

// How agents speak (ADR-121): office's language, and the one they answer the
// person in (auto: the person's own).

func (s *server) getLanguage(w http.ResponseWriter, r *http.Request) {
	l := chat.LoadLanguage(r.Context(), s.cfg.Store)
	writeJSON(w, http.StatusOK, map[string]any{"system": l.System, "response": l.Response, "languages": chat.Languages})
}

func (s *server) setLanguage(w http.ResponseWriter, r *http.Request) {
	var in chat.Language
	if !decode(w, r, &in) {
		return
	}
	in = in.Clean()
	before := chat.LoadLanguage(r.Context(), s.cfg.Store)
	if err := s.cfg.Store.Settings().Set(r.Context(), chat.LanguageKey, in); err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "language.update", Resource: "setting", ResourceID: chat.LanguageKey, Before: before, After: in})
	writeJSON(w, http.StatusOK, map[string]any{"system": in.System, "response": in.Response})
}
