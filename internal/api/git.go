package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/events"
	"bitbucket.org/senprints/agent-office/internal/gitops"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

func (s *server) gitRoutes(mux *http.ServeMux, auth, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/projects/{id}/git", auth(s.gitStatus))
	mux.Handle("POST /api/projects/{id}/git/fetch", auth(s.gitFetch))
	mux.Handle("POST /api/projects/{id}/git/commit", admin(s.gitCommit))
	mux.Handle("POST /api/projects/{id}/git/push", admin(s.gitPush))
	mux.Handle("GET /api/projects/{id}/git/branches", admin(s.gitBranches))
	mux.Handle("POST /api/projects/{id}/git/branches", admin(s.gitCreateBranch))
	mux.Handle("POST /api/projects/{id}/git/switch", admin(s.gitSwitch))
	mux.Handle("POST /api/projects/{id}/git/branches/delete", admin(s.gitDeleteBranch))
}

func (s *server) gitBranches(w http.ResponseWriter, r *http.Request) {
	p, ok, err := s.projectRoot(r, r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "project chưa có thư mục")
		return
	}
	list, err := gitops.Branches(r.Context(), p.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": list})
}

// gitCreateBranch creates a branch and switches to it (local changes follow),
// as git_branch does for agents.
func (s *server) gitCreateBranch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.runGitAction(w, r, "git_branch", "", storage.ActionArgs{Branch: strings.TrimSpace(in.Name)})
}

// gitSwitch / gitDeleteBranch change the project folder's branch: git refuses
// what would lose work (changes in the way, a branch not merged).
func (s *server) gitSwitch(w http.ResponseWriter, r *http.Request) {
	s.gitBranchOp(w, r, "git.switch", gitops.SwitchBranch)
}

func (s *server) gitDeleteBranch(w http.ResponseWriter, r *http.Request) {
	s.gitBranchOp(w, r, "git.branch_delete", gitops.DeleteBranch)
}

func (s *server) gitBranchOp(w http.ResponseWriter, r *http.Request, action string, op func(ctx context.Context, root, name string) error) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, ok, err := s.projectRoot(r, r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "project chưa có thư mục")
		return
	}
	name := strings.TrimSpace(in.Name)
	err = op(r.Context(), p.Path, name)
	s.auditAction(r, action, name, map[string]any{"project": p.ID, "ok": err == nil})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeGitStatus(w, r, p.Path)
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
	done, err := s.gitBackground(r, p, "fetch", func(ctx context.Context) (any, error) {
		return nil, gitops.Fetch(ctx, p.Path)
	})
	if done && err != nil { // errGitBusy (not done): the one running tells its end
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, serr := gitops.ReadStatus(r.Context(), p.Path)
	if serr != nil {
		writeJSON(w, http.StatusOK, map[string]any{"repo": false, "error": serr.Error()})
		return
	}
	out := map[string]any{"repo": true, "status": st}
	if !done {
		out["running"] = "fetch"
	}
	writeJSON(w, http.StatusOK, out)
}

// gitWait is how long a fetch or push request waits for git; a slower one
// (a big push, a slow remote) goes on in the background and its end comes
// as a "git" event, so the request does not hold one of the browser's few
// connections to office for up to a minute (ADR-127).
var gitWait = 3 * time.Second

// gitRunning: a project's fetch or push in the background, one of each.
var gitRunning sync.Map

var errGitBusy = errors.New("git của project đang chạy việc này, chờ nó xong")

// gitBackground runs fn (the project's fetch or push) apart from the request
// and waits up to gitWait. done: it ended (with err); otherwise it goes on
// (or one already was: errGitBusy) and a "git" event tells the caller's pages its end
// (project, kind, repo, status, action, error).
func (s *server) gitBackground(r *http.Request, p storage.Repo, kind string, fn func(context.Context) (any, error)) (done bool, err error) {
	key := p.ID + ":" + kind
	if _, busy := gitRunning.LoadOrStore(key, true); busy {
		return false, errGitBusy
	}
	who := userFrom(r).ID
	ctx := context.WithoutCancel(r.Context())
	end := make(chan error, 1)
	waiting := make(chan bool, 1)
	waiting <- true
	go func() {
		defer gitRunning.Delete(key)
		extra, err := fn(ctx)
		select {
		case <-waiting: // the request still waits: it answers
			end <- err
			return
		default:
		}
		if s.cfg.Events == nil {
			return
		}
		data := map[string]any{"project": p.ID, "kind": kind, "repo": true}
		if st, serr := gitops.ReadStatus(ctx, p.Path); serr == nil {
			data["status"] = st
		}
		if err != nil {
			data["error"] = err.Error()
		}
		if extra != nil {
			data["action"] = extra
		}
		s.cfg.Events.Send(events.Event{Name: "git", Data: data}, func(v events.Viewer, _ map[string]bool) bool { return v.UserID == who || v.Admin })
	}()
	select {
	case err := <-end:
		return true, err
	case <-time.After(gitWait):
	}
	select {
	case <-waiting: // it did not end: from now on the event tells
		return false, nil
	default: // it ended just now
		return true, <-end
	}
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
	p, ok, err := s.projectRoot(r, r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "project không gắn thư mục")
		return
	}
	if _, busy := gitRunning.Load(p.ID + ":push"); busy {
		writeError(w, http.StatusConflict, errGitBusy.Error())
		return
	}
	who := userFrom(r).Email
	sc := actions.Scope{ProjectID: p.ID, Agent: who, Level: perm.Propose}
	a, err := s.cfg.Actions.Propose(r.Context(), sc, "git_push", "", "người dùng thao tác trên dashboard", storage.ActionArgs{})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Status != "pending" { // it ran on its own
		s.writeGitAction(w, r, "git_push", a)
		return
	}
	audit := r.Clone(context.WithoutCancel(r.Context()))
	var decided storage.Action // read only once it ended (done)
	done, err := s.gitBackground(r, p, "push", func(ctx context.Context) (any, error) {
		d, err := s.cfg.Actions.Decide(ctx, a.ID, true, who)
		if err != nil {
			return nil, err
		}
		decided = d
		s.auditAction(audit, "git.push", d.ID, map[string]any{"project": d.ProjectID, "status": d.Status})
		if d.Status == "failed" {
			return chat.ToActionDTO(d), errors.New(d.Detail)
		}
		return chat.ToActionDTO(d), nil
	})
	switch {
	case errors.Is(err, errGitBusy): // another push started meanwhile
		_, _ = s.cfg.Actions.Decide(r.Context(), a.ID, false, who)
		writeError(w, http.StatusConflict, err.Error())
	case !done: // it goes on: a "git" event tells its end
		writeJSON(w, http.StatusAccepted, map[string]any{"action": chat.ToActionDTO(a), "running": "push"})
	case decided.ID == "": // not decided
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.writeGitAction(w, r, "", decided)
	}
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
	s.writeGitAction(w, r, kind, a)
}

// writeGitAction answers with a decided git action (audited first, unless
// kind is "": its runner did).
func (s *server) writeGitAction(w http.ResponseWriter, r *http.Request, kind string, a storage.Action) {
	if kind != "" {
		s.auditAction(r, "git."+strings.TrimPrefix(kind, "git_"), a.ID, map[string]any{"project": a.ProjectID, "status": a.Status, "files": len(a.Args.Files)})
	}
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
