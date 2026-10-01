package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// decideAction approves (runs) or rejects an operation an agent proposed.
func (s *server) decideAction(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		// a provider's card: the key the person pasted goes straight to the change
		var in struct {
			APIKey string `json:"api_key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.APIKey != "" {
			r = r.WithContext(context.WithValue(r.Context(), cfgSecretKey{}, strings.TrimSpace(in.APIKey)))
		}
		if cur, err := s.cfg.Store.Actions().Get(r.Context(), r.PathValue("id")); err == nil {
			who := proposerWho(cur, u)
			if !approve { // the person's decision
				who = audit.Who{Kind: "human", ID: u.ID, Name: u.Email, Via: "ui", ConversationID: cur.ConversationID, JobID: cur.JobID, TaskID: cur.TaskID, ActionID: cur.ID}
			}
			r = r.WithContext(audit.With(r.Context(), who))
		}
		a, err := s.cfg.Actions.Decide(r.Context(), r.PathValue("id"), approve, u.Email)
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
		var runErr error
		if a.Status == "failed" {
			runErr = errors.New(a.Detail)
		}
		s.audit(r, audit.Change{Action: verb, Resource: "action", ResourceID: a.ID, ProjectID: a.ProjectID,
			Detail: map[string]any{"kind": a.Kind, "target": a.Target, "status": a.Status}, Err: runErr})
		if s.cfg.Chat != nil { // its agent goes on (ADR-084)
			label := firstNonEmptyStr(actions.Kinds[a.Kind], a.Kind) + ": " + a.Target
			line := "❌ Đã từ chối " + label
			if approve {
				line = "✅ Đã duyệt " + label + " — " + firstNonEmptyStr(a.Detail, a.Status)
			}
			s.cfg.Chat.Decided(a.ConversationID, u.Email, line)
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": chat.ToActionDTO(a)})
	}
}

// proposerWho: an approved proposal is the agent's change, approved by the
// person (ADR-043).
func proposerWho(a storage.Action, u storage.User) audit.Who {
	via := "chat"
	if a.ConversationID == "" && a.TaskID != "" {
		via = "task"
	}
	return audit.Who{Kind: "agent", Name: a.ProposedBy, ApprovedBy: u.Email, Via: via,
		ConversationID: a.ConversationID, JobID: a.JobID, TaskID: a.TaskID, ActionID: a.ID}
}
