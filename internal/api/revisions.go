package api

import (
	"net/http"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// Revisions are snapshots of a project's agents taken before each change.

type revisionDTO struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Action     string    `json:"action"`
	Actor      string    `json:"actor"`
	AgentCount int       `json:"agent_count"`
	CreatedAt  time.Time `json:"created_at"`
}

func toRevisionDTO(r storage.Revision) revisionDTO {
	return revisionDTO{ID: r.ID, ProjectID: r.ProjectID, Action: r.Action, Actor: r.Actor, AgentCount: r.AgentCount, CreatedAt: r.CreatedAt}
}

func (s *server) listRevisions(w http.ResponseWriter, r *http.Request) {
	revs, err := s.cfg.Team.Revisions(r.Context(), r.PathValue("id"), team.KeepRevisions)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]revisionDTO, 0, len(revs))
	for _, x := range revs {
		out = append(out, toRevisionDTO(x))
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": out})
}

func (s *server) getRevision(w http.ResponseWriter, r *http.Request) {
	rev, err := s.cfg.Store.Revisions().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	snap, err := team.RevisionSnapshot(rev)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": toRevisionDTO(rev), "snapshot": snap})
}

func (s *server) restoreRevision(w http.ResponseWriter, r *http.Request) {
	rev, err := s.cfg.Team.Restore(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "project.restore_agents", rev.ProjectID, map[string]any{"revision": rev.ID})
	x, err := s.cfg.Store.Repos().Get(r.Context(), rev.ProjectID)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	d, _ := s.repoDTO(r, x, true)
	writeJSON(w, http.StatusOK, map[string]any{"project": d})
}
