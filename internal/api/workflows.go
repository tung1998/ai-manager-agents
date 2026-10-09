package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// Workflows (spec 2026-10-07-workflows-design): the library (files) and each
// project's copies, and their runs in chats.

func (s *server) workflowRoutes(mux *http.ServeMux) {
	auth := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }
	admin := func(h http.HandlerFunc) http.Handler { return s.requireRole(storage.RoleAdmin, h) }
	mux.Handle("GET /api/projects/{id}/workflows", auth(s.listWorkflows))
	mux.Handle("POST /api/projects/{id}/workflows", admin(s.createWorkflow))
	mux.Handle("GET /api/workflows/{id}", auth(s.getWorkflow))
	mux.Handle("PATCH /api/workflows/{id}", admin(s.updateWorkflow))
	mux.Handle("DELETE /api/workflows/{id}", admin(s.deleteWorkflow))
	mux.Handle("POST /api/workflows/{id}/from-library", admin(s.workflowFromLibrary))
	mux.Handle("POST /api/workflows/{id}/to-library", admin(s.workflowToLibrary))
	mux.Handle("GET /api/workflow-library", auth(s.listWorkflowLibrary))
	mux.Handle("POST /api/workflow-library/validate", auth(s.validateWorkflow))
	mux.Handle("POST /api/workflow-library/format", auth(s.formatWorkflow))
	mux.Handle("GET /api/workflow-library/{key}", auth(s.getLibraryWorkflow))
	mux.Handle("PUT /api/workflow-library/{key}", admin(s.saveLibraryWorkflow))
	mux.Handle("DELETE /api/workflow-library/{key}", admin(s.deleteLibraryWorkflow))
	mux.Handle("POST /api/workflow-library/{key}/reset", admin(s.resetLibraryWorkflow))
	mux.Handle("GET /api/projects/{id}/workflow-runs", auth(s.listWorkflowRuns))
	mux.Handle("GET /api/workflow-runs/{id}", auth(s.getWorkflowRun))
	mux.Handle("POST /api/workflow-runs/{id}/stop", auth(s.stopWorkflowRun))
}

type workflowDTO struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Input       string            `json:"input"`
	Enabled     bool              `json:"enabled"`
	Source      string            `json:"source"`
	Bindings    map[string]string `json:"bindings"`
	Roles       []workflow.Role   `json:"roles"`
	Parallel    [][]string        `json:"parallel"`
	Gates       []workflow.Gate   `json:"gates"`
	Vote        *workflow.Vote    `json:"vote"`
	Limits      workflow.Limits   `json:"limits"`
	Brief       []string          `json:"brief"`
	Inputs      []workflow.Field  `json:"inputs"`
	Outputs     []workflow.Field  `json:"outputs"`
	Callable    string            `json:"callable"`
	Error       string            `json:"error,omitempty"`
	SourceKey   string            `json:"source_key"`
	HasUpdate   bool              `json:"has_update"`
	UpdatedAt   time.Time         `json:"updated_at"`
	LastRun     *workflowRunDTO   `json:"last_run"`
	Version     string            `json:"version"`
}

type workflowRunDTO struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	ConversationID string `json:"conversation_id"` // the run's own chat (a run from before: the chat it ran in)
	// the chat that called it, and its title ("" = a run from before, in ConversationID)
	CallerConversationID string `json:"caller_conversation_id"`
	CallerTitle          string `json:"caller_title"`
	// the run whose role called it (a sub-workflow, ADR-102) and how deep
	ParentRunID     string            `json:"parent_run_id"`
	Depth           int               `json:"depth"`
	WorkflowID      string            `json:"workflow_id"`
	WorkflowKey     string            `json:"workflow_key"`
	WorkflowName    string            `json:"workflow_name"`
	CoordinatorID   string            `json:"coordinator_id"`
	CoordinatorName string            `json:"coordinator_name"`
	Input           string            `json:"input"`
	Status          string            `json:"status"`
	Turns           int               `json:"turns"`
	MaxTurns        int               `json:"max_turns"`
	CostUSD         float64           `json:"cost_usd"`
	Result          string            `json:"result"`
	Error           string            `json:"error"`
	Roles           []storage.RunRole `json:"roles"`
	Gates           []storage.RunGate `json:"gates"`
	Log             []storage.RunLog  `json:"log"`
	Outputs         map[string]string `json:"outputs"`
	StartedAt       time.Time         `json:"started_at"`
	FinishedAt      *time.Time        `json:"finished_at"`
}

func (s *server) toWorkflowRunDTO(ctx context.Context, r storage.WorkflowRun) workflowRunDTO {
	roles := make([]storage.RunRole, len(r.Roles))
	for i, x := range r.Roles {
		x.SessionID, x.Runtime = "", "" // its sessions stay on the server
		roles[i] = x
	}
	d := workflowRunDTO{ID: r.ID, ProjectID: r.ProjectID, ConversationID: r.ConversationID, CallerConversationID: r.CallerConversationID, ParentRunID: r.ParentRunID, Depth: r.Depth, WorkflowID: r.WorkflowID, WorkflowKey: r.WorkflowKey,
		WorkflowName: r.WorkflowName, CoordinatorID: r.CoordinatorID, CoordinatorName: r.CoordinatorName, Input: r.Input, Status: r.Status, Turns: r.Turns,
		CostUSD: r.CostUSD, Result: r.Result, Error: r.Error, Roles: roles, Gates: r.Gates, Log: r.Log, Outputs: r.Outputs, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt}
	if c, err := s.cfg.Store.Chat().GetConversation(ctx, cmp.Or(r.CallerConversationID, r.ConversationID)); err == nil {
		d.CallerTitle = c.Title
	}
	if w, err := s.cfg.Store.Workflows().Get(ctx, r.WorkflowID); err == nil {
		if def, err := workflow.Parse(w.Source); err == nil {
			d.MaxTurns = def.Limits.Turns
		}
	}
	return d
}

func (s *server) toWorkflowDTO(ctx context.Context, w storage.Workflow, withRun bool) workflowDTO {
	d := workflowDTO{ID: w.ID, ProjectID: w.ProjectID, Key: w.Key, Name: w.Name, Description: w.Description, Enabled: w.Enabled, Source: w.Source,
		Bindings: w.Bindings, SourceKey: w.SourceKey, UpdatedAt: w.UpdatedAt, Roles: []workflow.Role{}, Inputs: []workflow.Field{}, Outputs: []workflow.Field{}, Gates: []workflow.Gate{}, Parallel: [][]string{}, Brief: []string{},
		Version: workflowVersion(w)}
	if def, err := workflow.Parse(w.Source); err != nil {
		d.Error = err.Error()
	} else {
		d.Input, d.Roles, d.Vote, d.Limits, d.Callable = def.Input, def.Roles, def.Vote, def.Limits, def.Callable
		d.Inputs, d.Outputs = append([]workflow.Field{}, def.Inputs...), append([]workflow.Field{}, def.Outputs...)
		if def.Gates != nil {
			d.Gates = def.Gates
		}
		if def.Parallel != nil {
			d.Parallel = def.Parallel
		}
		if def.Brief != nil {
			d.Brief = def.Brief
		}
	}
	if s.cfg.Workflows != nil {
		d.HasUpdate = s.cfg.Workflows.HasUpdate(w)
	}
	if withRun {
		if runs, err := s.cfg.Store.WorkflowRuns().ListByWorkflow(ctx, w.ProjectID, "", w.ID, 1); err == nil && len(runs) > 0 {
			x := s.toWorkflowRunDTO(ctx, runs[0])
			d.LastRun = &x
		}
	}
	return d
}

func (s *server) workflowsOn(w http.ResponseWriter) bool {
	if s.cfg.Workflows == nil {
		writeError(w, http.StatusNotFound, "office chưa bật quy trình")
		return false
	}
	return true
}

func (s *server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	pid := r.PathValue("id")
	if _, err := s.cfg.Store.Repos().Get(r.Context(), pid); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	list, err := s.cfg.Store.Workflows().List(r.Context(), pid)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]workflowDTO, 0, len(list))
	for _, x := range list {
		out = append(out, s.toWorkflowDTO(r.Context(), x, true))
	}
	lib, _ := s.cfg.Workflows.Lib.List()
	writeJSON(w, http.StatusOK, map[string]any{"workflows": out, "library": lib})
}

type workflowInput struct {
	Key      string            `json:"key"`    // create: the library workflow to copy ("" = Source)
	Source   *string           `json:"source"` // the whole file
	Bindings map[string]string `json:"bindings"`
	Enabled  *bool             `json:"enabled"`
	Version  string            `json:"version"` // ADR-072: the version read when the edit started; "" skips the check
	// ConversationID: the chat that wrote it (read back by the config registry; unused)
	ConversationID string `json:"conversation_id"`
}

func (s *server) workflowError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workflow.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, storage.ErrConflict):
		writeError(w, http.StatusConflict, "Project đã có quy trình cùng key")
	case errors.Is(err, workflow.ErrBadBinding), errors.Is(err, workflow.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error()) // the person's to fix: say why
	default:
		s.internal(w, r, err)
	}
}

func (s *server) createWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	var in workflowInput
	if !decode(w, r, &in) {
		return
	}
	pid := r.PathValue("id")
	if _, err := s.cfg.Store.Repos().Get(r.Context(), pid); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	var (
		x   storage.Workflow
		err error
	)
	switch {
	case in.Key != "":
		x, err = s.cfg.Workflows.Install(r.Context(), pid, in.Key, in.Bindings)
	case in.Source != nil:
		x, err = s.cfg.Workflows.Create(r.Context(), pid, *in.Source, in.Bindings)
	default:
		writeError(w, http.StatusBadRequest, "cần key (quy trình trong thư viện) hoặc source")
		return
	}
	if err == nil && in.Enabled != nil && !*in.Enabled {
		x, err = s.cfg.Workflows.Update(r.Context(), x.ID, workflow.Change{Enabled: in.Enabled})
	}
	s.audit(r, audit.Change{Action: "workflow.create", Resource: "workflow", ResourceID: x.ID, ProjectID: pid,
		Detail: map[string]any{"key": x.Key, "from": in.Key}, After: map[string]any{"key": x.Key, "bindings": x.Bindings}, Err: err})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workflow": s.toWorkflowDTO(r.Context(), x, false)})
}

func (s *server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	x, err := s.cfg.Store.Workflows().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": s.toWorkflowDTO(r.Context(), x, true)})
}

func (s *server) updateWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	var in workflowInput
	if !decode(w, r, &in) {
		return
	}
	before, err := s.cfg.Store.Workflows().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if conflicted(w, in.Version, workflowVersion(before)) {
		return
	}
	x, err := s.cfg.Workflows.Update(r.Context(), before.ID, workflow.Change{Source: in.Source, Bindings: in.Bindings, Enabled: in.Enabled})
	s.audit(r, audit.Change{Action: "workflow.update", Resource: "workflow", ResourceID: before.ID, ProjectID: before.ProjectID,
		Before: map[string]any{"source": before.Source, "bindings": before.Bindings, "enabled": before.Enabled},
		After:  map[string]any{"source": x.Source, "bindings": x.Bindings, "enabled": x.Enabled}, Err: err})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": s.toWorkflowDTO(r.Context(), x, false)})
}

func (s *server) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	x, err := s.cfg.Store.Workflows().Get(r.Context(), r.PathValue("id"))
	if err == nil {
		err = s.cfg.Store.Workflows().Delete(r.Context(), x.ID)
	}
	s.audit(r, audit.Change{Action: "workflow.delete", Resource: "workflow", ResourceID: r.PathValue("id"), ProjectID: x.ProjectID,
		Before: map[string]any{"key": x.Key, "source": x.Source}, Err: err})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) workflowFromLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	x, err := s.cfg.Workflows.FromLibrary(r.Context(), r.PathValue("id"))
	s.audit(r, audit.Change{Action: "workflow.update", Resource: "workflow", ResourceID: r.PathValue("id"), ProjectID: x.ProjectID,
		Detail: map[string]any{"from_library": x.SourceKey}, Err: err})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": s.toWorkflowDTO(r.Context(), x, false)})
}

func (s *server) workflowToLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	x, err := s.cfg.Store.Workflows().Get(r.Context(), r.PathValue("id"))
	if err == nil {
		_, err = s.cfg.Workflows.Lib.Save(x.Key, x.Source)
	}
	s.audit(r, audit.Change{Action: "workflow.to_library", Resource: "workflow", ResourceID: r.PathValue("id"), ProjectID: x.ProjectID,
		Detail: map[string]any{"key": x.Key}, Err: err})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) listWorkflowLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	list, err := s.cfg.Workflows.Lib.List()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": list})
}

func (s *server) getLibraryWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	it, err := s.cfg.Workflows.Lib.Get(r.PathValue("key"))
	if errors.Is(err, workflow.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": it}) // one that does not parse comes with its error
}

func (s *server) validateWorkflow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source string `json:"source"`
	}
	if !decode(w, r, &in) {
		return
	}
	d, err := workflow.Parse(in.Source)
	if err != nil {
		out := map[string]any{"ok": false, "error": err.Error()}
		if d.Key != "" || len(d.Roles) > 0 || len(d.Steps) > 0 { // it reads, it only does not check: the canvas can still show it
			out["draft"] = d
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "def": d})
}

// formatWorkflow writes a workflow's file from its definition (the editor's
// canvas changes the definition): the file, and what is wrong with it.
func (s *server) formatWorkflow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Def workflow.Def `json:"def"`
	}
	if !decode(w, r, &in) {
		return
	}
	src, err := workflow.Format(in.Def)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := map[string]any{"source": src}
	if _, err := workflow.Parse(src); err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) saveLibraryWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	var in struct {
		Source string `json:"source"`
	}
	if !decode(w, r, &in) {
		return
	}
	key := r.PathValue("key")
	_, err := s.cfg.Workflows.Lib.Save(key, in.Source)
	s.audit(r, audit.Change{Action: "workflow_library.save", Resource: "workflow_library", ResourceID: key, Err: err})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	it, _ := s.cfg.Workflows.Lib.Get(key)
	writeJSON(w, http.StatusOK, map[string]any{"workflow": it})
}

func (s *server) deleteLibraryWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	key := r.PathValue("key")
	err := s.cfg.Workflows.Lib.Delete(key)
	s.audit(r, audit.Change{Action: "workflow_library.delete", Resource: "workflow_library", ResourceID: key, Err: err})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) resetLibraryWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsOn(w) {
		return
	}
	key := r.PathValue("key")
	_, err := s.cfg.Workflows.Lib.Reset(key)
	s.audit(r, audit.Change{Action: "workflow_library.reset", Resource: "workflow_library", ResourceID: key, Err: err})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	it, _ := s.cfg.Workflows.Lib.Get(key)
	writeJSON(w, http.StatusOK, map[string]any{"workflow": it})
}

func (s *server) listWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	// ?conversation=: a chat's (called from it, or run in it); ?workflow=: one workflow's
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	wf := r.URL.Query().Get("workflow")
	pid, conv := r.PathValue("id"), r.URL.Query().Get("conversation")
	var runs []storage.WorkflowRun
	var err error
	if wf != "" {
		runs, err = s.cfg.Store.WorkflowRuns().ListByWorkflow(r.Context(), pid, conv, wf, limit)
	} else {
		runs, err = s.cfg.Store.WorkflowRuns().List(r.Context(), pid, conv, limit)
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]workflowRunDTO, 0, len(runs))
	for _, x := range runs {
		out = append(out, s.toWorkflowRunDTO(r.Context(), x))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (s *server) getWorkflowRun(w http.ResponseWriter, r *http.Request) {
	x, err := s.cfg.Store.WorkflowRuns().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	// the sub-workflows it called (its own chat called them)
	children := []workflowRunDTO{}
	if list, err := s.cfg.Store.WorkflowRuns().List(r.Context(), x.ProjectID, x.ConversationID, 100); err == nil {
		for _, c := range list {
			if c.ParentRunID == x.ID {
				children = append(children, s.toWorkflowRunDTO(r.Context(), c))
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": s.toWorkflowRunDTO(r.Context(), x), "children": children})
}

// stopWorkflowRun stops a run and every answer of its chat (as the chat's Dừng).
func (s *server) stopWorkflowRun(w http.ResponseWriter, r *http.Request) {
	x, err := s.cfg.Store.WorkflowRuns().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if x.Status == storage.RunRunning && s.cfg.Chat != nil && s.cfg.Chat.RunningWorkflow(x.ConversationID) == x.ID {
		s.cfg.Chat.StopAll(x.ConversationID)
	}
	s.audit(r, audit.Change{Action: "workflow_run.stop", Resource: "workflow_run", ResourceID: x.ID, ProjectID: x.ProjectID})
	if x, err = s.cfg.Store.WorkflowRuns().Get(r.Context(), x.ID); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": s.toWorkflowRunDTO(r.Context(), x)})
}
