package api

import (
	"errors"
	"net/http"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/chat"
)

// decideAction approves (runs) or rejects an operation an agent proposed.
func (s *server) decideAction(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := s.cfg.Actions.Decide(r.Context(), r.PathValue("id"), approve, userFrom(r).Email)
		if errors.Is(err, actions.ErrDecided) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "action": chat.ToActionDTO(a)})
			return
		}
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		verb := "action.reject"
		if approve {
			verb = "action.approve"
		}
		s.auditAction(r, verb, a.ID, map[string]any{"kind": a.Kind, "target": a.Target, "status": a.Status, "project": a.ProjectID})
		writeJSON(w, http.StatusOK, map[string]any{"action": chat.ToActionDTO(a)})
	}
}
