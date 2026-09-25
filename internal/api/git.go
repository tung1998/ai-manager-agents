package api

import (
	"fmt"
	"net/http"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/gitops"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

func (s *server) gitRoutes(mux *http.ServeMux, auth, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/projects/{id}/git", auth(s.gitStatus))
	mux.Handle("POST /api/projects/{id}/git/commit", admin(s.gitCommit))
	mux.Handle("POST /api/projects/{id}/git/push", admin(s.gitPush))
	mux.Handle("POST /api/tasks/{id}/commit-draft", admin(s.commitDraft))
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
	st, err := gitops.ReadStatus(r.Context(), p.Path)
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

// commitDraft suggests a commit message (and files) for a task's applied
// changes, in the style of the repo's recent history, with a fast model.
func (s *server) commitDraft(w http.ResponseWriter, r *http.Request) {
	t, err := s.cfg.Store.Tasks().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	p, ok, err := s.projectRoot(r, t.ProjectID)
	if err != nil || !ok {
		writeError(w, http.StatusBadRequest, "project không gắn thư mục")
		return
	}
	st, err := gitops.ReadStatus(r.Context(), p.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	changed := map[string]bool{}
	for _, c := range st.Changes {
		changed[c.Path] = true
	}
	patches, _ := s.cfg.Store.Tasks().ListPatches(r.Context(), t.ID)
	seen := map[string]bool{}
	files := []string{}
	for _, pt := range patches {
		if pt.Status != "applied" {
			continue
		}
		for _, f := range pt.Files {
			if changed[f] && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "không còn thay đổi nào của Việc này chưa commit")
		return
	}
	diff, _ := gitops.Diff(r.Context(), p.Path, files, 30000)
	history, _ := gitops.Log(r.Context(), p.Path, 15)
	prompt := fmt.Sprintf(`Viết commit message cho các thay đổi dưới đây, cùng phong cách với lịch sử commit của repo (ngôn ngữ, tiền tố kiểu "feat:", "fix:"…).
Dòng đầu tối đa 72 ký tự; nếu cần, thêm dòng trống rồi vài gạch đầu dòng ngắn. Chỉ trả về commit message, không thêm chữ nào khác.

Việc: %s

Commit gần đây:
%s

Diff:
%s`, t.Title, history, diff)
	prov, err := s.cfg.Providers.Default(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"message": t.Title, "files": files})
		return
	}
	model := firstNonEmptyStr(prov.TierModels["fast"], prov.TierModels["balanced"])
	res, err := s.cfg.Providers.Call(r.Context(), prov, llm.Request{Model: model, Prompt: prompt, MaxTokens: 400},
		usage.Meta{Kind: "commit_message", ProjectID: t.ProjectID})
	msg := strings.Trim(strings.TrimSpace(res.Text), "`")
	if err != nil || msg == "" {
		msg = t.Title
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": msg, "files": files})
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
