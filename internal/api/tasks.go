package api

import (
	"bitbucket.org/senprints/agent-office/internal/attach"
	"bitbucket.org/senprints/agent-office/internal/automation"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/tasks"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Tasks.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": list})
}

func (s *server) createTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Goal        string   `json:"goal"`
		BudgetUSD   float64  `json:"budget_usd"`
		Attachments []string `json:"attachments"`
		Mode        string   `json:"mode"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.BudgetUSD < 0 {
		writeError(w, http.StatusBadRequest, "ngân sách không được âm")
		return
	}
	if s.cfg.Usage != nil {
		if err := s.cfg.Usage.Check(r.Context(), r.PathValue("id")); err != nil {
			var be *usage.BudgetError
			if errors.As(err, &be) {
				writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": be.Error(), "code": "budget"})
				return
			}
		}
	}
	t, err := s.cfg.Tasks.Start(r.Context(), r.PathValue("id"), in.Goal, in.BudgetUSD, in.Attachments, s.allowedMode(r, in.Mode))
	switch {
	case errors.Is(err, tasks.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, tasks.ErrNoModel), errors.Is(err, automation.ErrUnknownSkill), errors.Is(err, attach.ErrNotFound), errors.Is(err, attach.ErrTooMany):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "task.start", t.ID, map[string]any{"project": t.ProjectID, "mode": t.Mode, "budget_usd": t.BudgetUSD})
	d, _ := s.cfg.Tasks.Get(r.Context(), t.ID)
	writeJSON(w, http.StatusAccepted, d)
}

// approveAllPatches applies a task's pending diffs as one batch (all or none).
func (s *server) approveAllPatches(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Chat.ApproveTaskPatches(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, chat.ErrNoFolder) || strings.HasPrefix(err.Error(), "chưa áp gì") || strings.HasPrefix(err.Error(), "không có diff") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "task.patches.approve_all", r.PathValue("id"), map[string]any{"count": len(out)})
	writeJSON(w, http.StatusOK, map[string]any{"patches": out})
}

// revertAllPatches takes back a task's applied diffs (all or none).
func (s *server) revertAllPatches(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Chat.RevertTaskPatches(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, chat.ErrNoFolder) || strings.HasPrefix(err.Error(), "không") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "task.patches.revert_all", r.PathValue("id"), map[string]any{"count": len(out)})
	writeJSON(w, http.StatusOK, map[string]any{"patches": out})
}

// retryTask starts a finished task again; learn feeds it last run's lessons.
func (s *server) retryTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Learn  bool   `json:"learn"`
		Mode   string `json:"mode"`
		Answer string `json:"answer"` // reply to a task that stopped with a question
	}
	if !decode(w, r, &in) {
		return
	}
	mode := ""
	if in.Mode != "" {
		mode = s.allowedMode(r, in.Mode)
	}
	t, err := s.cfg.Tasks.Retry(r.Context(), r.PathValue("id"), in.Learn, mode, in.Answer)
	switch {
	case errors.Is(err, tasks.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, tasks.ErrNoModel), errors.Is(err, automation.ErrUnknownSkill), errors.Is(err, attach.ErrNotFound):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "task.retry", t.ID, map[string]any{"from": r.PathValue("id"), "learn": in.Learn})
	d, _ := s.cfg.Tasks.Get(r.Context(), t.ID)
	writeJSON(w, http.StatusAccepted, d)
}

func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	d, err := s.cfg.Tasks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) deleteTask(w http.ResponseWriter, r *http.Request) {
	if l, ok := s.cfg.Tasks.Live(r.PathValue("id")); ok {
		if _, done, _ := l.Since(0); !done {
			writeError(w, http.StatusConflict, "Việc đang chạy, hãy dừng trước")
			return
		}
	}
	if err := s.cfg.Store.Tasks().Delete(r.Context(), r.PathValue("id")); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) cancelTask(w http.ResponseWriter, r *http.Request) {
	l, ok := s.cfg.Tasks.Live(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Việc không còn chạy")
		return
	}
	l.Cancel()
	s.auditAction(r, "task.cancel", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// streamTask sends a running task's events as SSE, replayable from ?from / Last-Event-ID.
func (s *server) streamTask(w http.ResponseWriter, r *http.Request) {
	l, ok := s.cfg.Tasks.Live(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Việc đã kết thúc")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	seq, _ := strconv.Atoi(r.URL.Query().Get("from"))
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if n, err := strconv.Atoi(last); err == nil {
			seq = n + 1
		}
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, done, wake := l.Since(seq)
		for _, e := range events {
			raw, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, raw)
			seq = e.Seq + 1
		}
		flusher.Flush()
		if done {
			return
		}
		select {
		case <-wake:
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
