package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/assistant"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Personal tokens (ADR-047): a person's own Claude Code CLI uses the office
// tools over MCP, in office scope, as that person; proposals wait for approval.

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func (s *server) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Tokens().List(r.Context(), userFrom(r).ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		out = append(out, map[string]any{"id": t.ID, "name": t.Name, "created_at": t.CreatedAt, "last_used_at": t.LastUsedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (s *server) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		s.internal(w, r, err)
		return
	}
	tok := "ofc_" + hex.EncodeToString(b)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Claude Code CLI"
	}
	t, err := s.cfg.Store.Tokens().Create(r.Context(), storage.UserToken{UserID: userFrom(r).ID, Name: name, TokenHash: hashToken(tok)})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.auditAction(r, "token.create", t.ID, map[string]any{"name": name})
	writeJSON(w, http.StatusCreated, map[string]any{"id": t.ID, "name": t.Name, "token": tok}) // shown once
}

func (s *server) revokeToken(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Store.Tokens().Revoke(r.Context(), r.PathValue("id"), userFrom(r).ID); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "token.revoke", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// tokenScope turns a personal token into the office scope of its person.
func (s *server) tokenScope(ctx context.Context, tok string) (officetools.Scope, bool) {
	if !strings.HasPrefix(tok, "ofc_") {
		return officetools.Scope{}, false
	}
	t, err := s.cfg.Store.Tokens().GetByHash(ctx, hashToken(tok))
	if err != nil || t.Revoked {
		return officetools.Scope{}, false
	}
	u, err := s.cfg.Store.Users().GetByID(ctx, t.UserID)
	if err != nil || u.Disabled {
		return officetools.Scope{}, false
	}
	_ = s.cfg.Store.Tokens().Touch(ctx, t.ID, time.Now().UTC())
	project := assistant.ID(ctx, s.cfg.Store)
	if project == "" {
		return officetools.Scope{}, false
	}
	// it proposes (a person approves on the dashboard); it never runs anything
	// itself. Only an admin calls MCP tools that write through the gateway
	// without asking; the role is read on every call, so a demotion counts at once.
	caps := []string{perm.CapPropose}
	if u.Role == storage.RoleAdmin {
		caps = append(caps, perm.CapMCPWrite)
	}
	return officetools.Scope{ProjectID: project, Office: true, Agent: "Claude Code CLI (" + u.Email + ")",
		RunRef: "cli-" + t.ID, Level: perm.Propose, Access: perm.Access{Level: perm.Propose, Caps: caps}}, true
}

// pendingActions is the approval inbox: every proposal waiting for a person
// (the CLI's have no chat to show them in).
func (s *server) pendingActions(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Actions().Pending(r.Context(), 100)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]chat.ActionDTO, 0, len(list))
	for _, a := range list {
		out = append(out, chat.ToActionDTO(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": out})
}
