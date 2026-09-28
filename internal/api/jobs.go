package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Jobs: every run (a chat answer, a task, an automation's turn), to follow
// and act on in one place (ADR-040).

func detached(r *http.Request) context.Context { return context.WithoutCancel(r.Context()) }

type jobDTO struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	Kind           string     `json:"kind"`
	Origin         string     `json:"origin"`
	OriginID       string     `json:"origin_id"`
	Trigger        string     `json:"trigger"`
	CreatedBy      string     `json:"created_by"`
	ConversationID string     `json:"conversation_id"`
	MessageID      string     `json:"message_id"`
	TaskID         string     `json:"task_id"`
	Status         string     `json:"status"`
	Error          string     `json:"error"`
	ErrorCode      string     `json:"error_code"`
	AgentID        string     `json:"agent_id"`
	AgentName      string     `json:"agent_name"`
	AutomationName string     `json:"automation_name"`
	ProjectName    string     `json:"project_name"`
	Title          string     `json:"title"`
	CostUSD        float64    `json:"cost_usd"`
	InputTokens    int        `json:"input_tokens"`
	OutputTokens   int        `json:"output_tokens"`
	DurationMS     int64      `json:"duration_ms"`
	Payload        string     `json:"payload,omitempty"`
	Output         string     `json:"output,omitempty"` // script: only with the job itself
	ExitCode       *int       `json:"exit_code"`
	ParentJobID    string     `json:"parent_job_id"`
	NextAttemptAt  *time.Time `json:"next_attempt_at"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

// names caches the labels a job listing needs.
type names struct{ agents, automations, projects map[string]string }

func (s *server) toJobDTO(r *http.Request, j storage.Job, n *names) jobDTO {
	if n == nil {
		n = &names{map[string]string{}, map[string]string{}, map[string]string{}}
	}
	look := func(m map[string]string, id string, get func() (string, error)) string {
		if id == "" {
			return ""
		}
		if v, ok := m[id]; ok {
			return v
		}
		v, _ := get()
		m[id] = v
		return v
	}
	ctx := r.Context()
	d := jobDTO{ID: j.ID, ProjectID: j.ProjectID, Kind: j.Kind, Origin: j.Origin, OriginID: j.OriginID, Trigger: j.Trigger, CreatedBy: j.CreatedBy,
		ConversationID: j.ConversationID, MessageID: j.MessageID, TaskID: j.TaskID, Status: j.Status, Error: j.Error, ErrorCode: j.ErrorCode,
		AgentID: j.AgentID, Title: j.Title, CostUSD: j.CostUSD, InputTokens: j.InputTokens, OutputTokens: j.OutputTokens, DurationMS: j.DurationMS,
		NextAttemptAt: j.NextAttemptAt, CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt, ExitCode: j.ExitCode, ParentJobID: j.ParentJobID}
	d.AgentName = look(n.agents, j.AgentID, func() (string, error) {
		a, err := s.cfg.Store.Agents().Get(ctx, j.AgentID)
		return a.Name, err
	})
	if j.Origin == "automation" {
		d.AutomationName = look(n.automations, j.OriginID, func() (string, error) {
			a, err := s.cfg.Store.Automations().Get(ctx, j.OriginID)
			return a.Name, err
		})
	}
	d.ProjectName = look(n.projects, j.ProjectID, func() (string, error) {
		p, err := s.cfg.Store.Repos().Get(ctx, j.ProjectID)
		return p.Name, err
	})
	return d
}

func jobFilter(r *http.Request) storage.JobFilter {
	q := r.URL.Query()
	f := storage.JobFilter{ProjectID: q.Get("project"), Kind: q.Get("kind"), Origin: q.Get("origin"), OriginID: q.Get("origin_id"),
		Status: q.Get("status"), AgentID: q.Get("agent"), Before: q.Get("before")}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if d, err := time.ParseDuration(q.Get("since")); err == nil && d > 0 {
		f.Since = time.Now().UTC().Add(-d)
	} else if days, err := strconv.Atoi(q.Get("days")); err == nil && days > 0 {
		f.Since = time.Now().UTC().AddDate(0, 0, -days)
	}
	return f
}

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	f := jobFilter(r)
	jobs, err := s.cfg.Store.Jobs().List(r.Context(), f)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	n := &names{map[string]string{}, map[string]string{}, map[string]string{}}
	out := make([]jobDTO, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, s.toJobDTO(r, j, n))
	}
	next := ""
	if limit := max(f.Limit, 50); len(jobs) >= min(limit, 200) {
		next = jobs[len(jobs)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out, "next_before": next})
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.cfg.Store.Jobs().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	d := s.toJobDTO(r, j, nil)
	d.Payload, d.Output = j.Payload, j.Output
	children, _ := s.cfg.Store.Jobs().List(r.Context(), storage.JobFilter{OriginID: j.OriginID, Since: j.CreatedAt, Limit: 50})
	var kids []jobDTO
	for _, c := range children {
		if c.ParentJobID == j.ID {
			kids = append(kids, s.toJobDTO(r, c, nil))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": d, "children": kids})
}

func (s *server) jobStats(w http.ResponseWriter, r *http.Request) {
	f := jobFilter(r)
	if f.Since.IsZero() {
		f.Since = time.Now().UTC().AddDate(0, 0, -7)
	}
	f.Status, f.Before, f.Limit = "", "", 0
	by := r.URL.Query().Get("by")
	rows, err := s.cfg.Store.Jobs().Stats(r.Context(), f, by)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	// the numbers of the moment: running and waiting now, failed and spent in 24h
	now := storage.JobFilter{ProjectID: f.ProjectID, Kind: f.Kind, Origin: f.Origin, OriginID: f.OriginID, AgentID: f.AgentID}
	count := func(status string, since time.Time) int {
		x := now
		x.Status, x.Since, x.Limit = status, since, 200
		list, _ := s.cfg.Store.Jobs().List(r.Context(), x)
		return len(list)
	}
	day := time.Now().UTC().Add(-24 * time.Hour)
	x := now
	x.Since = day
	cost := 0.0
	if d, err := s.cfg.Store.Jobs().Stats(r.Context(), x, "status"); err == nil {
		for _, row := range d {
			cost += row.CostUSD
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "totals": map[string]any{
		"running": count("running", time.Time{}), "pending": count("pending", time.Time{}), "needs_input": count("needs_input", day),
		"failed_24h": count("failed", day), "cost_24h": cost,
	}})
}

func (s *server) cancelJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.cfg.Store.Jobs().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	switch {
	case j.Status == "pending":
		j, err = s.cfg.Store.Jobs().Finish(r.Context(), j.ID, "cancelled", "cancelled", "", time.Now().UTC())
		if err != nil {
			s.internal(w, r, err)
			return
		}
	case j.Status == "running" && j.Kind == "chat_turn" && j.ConversationID != "":
		if t, ok := s.cfg.Chat.Active(j.ConversationID); ok {
			t.Cancel()
		}
	case j.Status == "running" && j.Kind == "task" && j.TaskID != "":
		if l, ok := s.cfg.Tasks.Live(j.TaskID); ok {
			l.Cancel()
		}
	default:
		writeError(w, http.StatusConflict, "job này không còn chạy")
		return
	}
	s.auditAction(r, "job.cancel", j.ID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"job": s.toJobDTO(r, j, nil)})
}

func (s *server) retryJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.cfg.Store.Jobs().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	switch {
	case j.Origin == "automation" && s.cfg.Trigger != nil:
		a, err := s.cfg.Store.Automations().Get(r.Context(), j.OriginID)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		nj, _, err := s.cfg.Trigger.Enqueue(r.Context(), a, "manual", j.Payload, "", "")
		if err != nil {
			s.internal(w, r, err)
			return
		}
		s.cfg.Trigger.StartReady(detached(r), time.Now().UTC())
		s.auditAction(r, "job.retry", j.ID, map[string]any{"job": nj.ID})
		writeJSON(w, http.StatusAccepted, map[string]any{"job": s.toJobDTO(r, nj, nil)})
	case j.Kind == "task" && j.TaskID != "":
		t, err := s.cfg.Tasks.Retry(r.Context(), j.TaskID, true, "", "")
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		s.auditAction(r, "job.retry", j.ID, map[string]any{"task": t.ID})
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": t.ID})
	default:
		writeError(w, http.StatusBadRequest, "job này chạy lại từ cuộc Chat")
	}
}
