package api

import (
	"context"
	"net/http"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// audit records a change made through the API (ADR-043): by the signed-in
// person unless the request context already names someone (an approved
// proposal is the agent's change).
func (s *server) audit(r *http.Request, c audit.Change) {
	ctx := r.Context()
	w, ok := audit.From(ctx)
	if !ok {
		u := userFrom(r)
		w = audit.Who{Kind: "human", ID: u.ID, Name: u.Email, Via: "ui"}
	}
	res := c.Resource
	if res == "" {
		res, _, _ = strings.Cut(c.Action, ".")
	}
	if c.ProjectID == "" {
		if p, ok := c.Detail["project"].(string); ok {
			c.ProjectID = p
		} else if res == "project" {
			c.ProjectID = c.ResourceID
		}
	}
	switch res {
	case "job":
		w.JobID = c.ResourceID
	case "task":
		w.TaskID = c.ResourceID
	}
	_ = audit.Record(audit.With(ctx, w), s.cfg.Store.Audit(), c)
}

// agentProject is the project an agent belongs to ("" = a template's agent).
func (s *server) agentProject(ctx context.Context, a storage.Agent) string {
	m, err := s.cfg.Store.OrgModels().Get(ctx, a.OrgModelID)
	if err != nil {
		return ""
	}
	return m.RepoID
}

// repoSnapshot / modelSnapshot: the fields a person edits (no timestamps, so
// a diff shows only what changed).
func repoSnapshot(x storage.Repo) map[string]any {
	return map[string]any{"name": x.Name, "path": x.Path, "git_remote": x.GitRemote, "description": x.Description}
}

func modelSnapshot(m storage.OrgModel) map[string]any {
	return map[string]any{"key": m.Key, "name": m.Name, "description": m.Description, "kind": m.Kind, "governance": m.Governance}
}
