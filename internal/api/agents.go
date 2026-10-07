package api

import (
	"bitbucket.org/senprints/agent-office/internal/audit"
	"errors"
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/senprints/agent-office/internal/agentinfo"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// The agent page: one agent with its numbers, what it worked on and how its
// settings changed (see internal/agentinfo).

func (s *server) agentInfo() *agentinfo.Service {
	return agentinfo.New(s.cfg.Store, s.cfg.Team, time.Local)
}

// agentOf loads the agent of the request and its project id.
func (s *server) agentOf(w http.ResponseWriter, r *http.Request) (storage.Agent, string, bool) {
	a, err := s.cfg.Store.Agents().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return a, "", false
	}
	return a, a.ProjectID, true
}

func (s *server) getAgent(w http.ResponseWriter, r *http.Request) {
	a, projectID, ok := s.agentOf(w, r)
	if !ok {
		return
	}
	out := map[string]any{"agent": toAgentDTO(a)}
	if p, err := s.cfg.Store.Repos().Get(r.Context(), projectID); err == nil {
		agents, _ := s.cfg.Store.Agents().List(r.Context(), p.ID)
		d, _ := storage.DefaultAgent(p, agents)
		out["project"] = map[string]any{"id": p.ID, "name": p.Name}
		out["is_default"] = d.ID == a.ID
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) agentStats(w http.ResponseWriter, r *http.Request) {
	a, projectID, ok := s.agentOf(w, r)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days == 0 {
		days = 7
	}
	st, err := s.agentInfo().Stats(r.Context(), a, projectID, days)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": st})
}

func (s *server) agentActivity(w http.ResponseWriter, r *http.Request) {
	a, projectID, ok := s.agentOf(w, r)
	if !ok {
		return
	}
	items := []agentinfo.Item{}
	if projectID != "" {
		var err error
		if items, err = s.agentInfo().Activity(r.Context(), a, projectID, 50); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) agentHistory(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.agentOf(w, r)
	if !ok {
		return
	}
	h, err := s.agentInfo().History(r.Context(), a)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": h})
}

func (s *server) restoreAgent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RevisionID string `json:"revision_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, _, ok := s.agentOf(w, r)
	if !ok {
		return
	}
	old := a
	a, err := s.agentInfo().Restore(r.Context(), a, in.RevisionID)
	if errors.Is(err, agentinfo.ErrNoBefore) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "agent.restore", ResourceID: a.ID, ProjectID: a.ProjectID, Before: toAgentDTO(old), After: toAgentDTO(a),
		Detail: map[string]any{"key": a.Key, "revision": in.RevisionID}})
	writeJSON(w, http.StatusOK, map[string]any{"agent": toAgentDTO(a)})
}
