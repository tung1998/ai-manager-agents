package api

import (
	"net/http"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/gitops"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

func (s *server) gitRoutes(mux *http.ServeMux, auth, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/projects/{id}/git", auth(s.gitStatus))
	mux.Handle("POST /api/projects/{id}/git/fetch", auth(s.gitFetch))
	mux.Handle("POST /api/projects/{id}/git/commit", admin(s.gitCommit))
	mux.Handle("POST /api/projects/{id}/git/push", admin(s.gitPush))
}

func (s *server) projectRoot(r *http.Request, id string) (storage.Repo, bool, error) {
	p, err := s.cfg.Store.Repos().Get(r.Context(), id)
	if err != nil {
		return p, false, err
	}
	return p, p.Path != "", nil
}

func (s *server) gitStatus(w http.ResponseWriter, r *http.Request) {
	p, ok, err := s.projectRoot(r, r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"repo": false})
		return
	}
	s.writeGitStatus(w, r, p.Path)
}

// gitFetch refreshes the remote-tracking branches first (read-only for the
// working tree), so ahead/behind is current.
func (s *server) gitFetch(w http.ResponseWriter, r *http.Request) {
	p, ok, err := s.projectRoot(r, r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"repo": false})
		return
	}
	if err := gitops.Fetch(r.Context(), p.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeGitStatus(w, r, p.Path)
}

func (s *server) writeGitStatus(w http.ResponseWriter, r *http.Request, root string) {
	st, err := gitops.ReadStatus(r.Context(), root)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"repo": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo": true, "status": st})
}

// gitCommit / gitPush go through the actions service (a proposal decided by
// the person at once), so they share its checks and its history.
func (s *server) gitCommit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message string   `json:"message"`
		Files   []string `json:"files"`
		TaskID  string   `json:"task_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.runGitAction(w, r, "git_commit", in.TaskID, storage.ActionArgs{Message: in.Message, Files: in.Files})
}

func (s *server) gitPush(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TaskID string `json:"task_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.runGitAction(w, r, "git_push", in.TaskID, storage.ActionArgs{})
}

func (s *server) runGitAction(w http.ResponseWriter, r *http.Request, kind, taskID string, args storage.ActionArgs) {
	who := userFrom(r).Email
	_ = taskID // a person's commit is not merged with an agent's pending proposal for the task
	sc := actions.Scope{ProjectID: r.PathValue("id"), Agent: who, Level: perm.Propose}
	a, err := s.cfg.Actions.Propose(r.Context(), sc, kind, "", "người dùng thao tác trên dashboard", args)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Status == "pending" {
		if a, err = s.cfg.Actions.Decide(r.Context(), a.ID, true, who); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.auditAction(r, "git."+strings.TrimPrefix(kind, "git_"), a.ID, map[string]any{"project": a.ProjectID, "status": a.Status, "files": len(a.Args.Files)})
	if a.Status == "failed" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": a.Detail, "action": chat.ToActionDTO(a)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"action": chat.ToActionDTO(a)})
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
