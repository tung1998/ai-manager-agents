package api

import (
	"bitbucket.org/senprints/agent-office/internal/audit"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// Automations: schedules and webhooks that start jobs (ADR-040).

func (s *server) triggerRoutes(mux *http.ServeMux) {
	auth := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }
	admin := func(h http.HandlerFunc) http.Handler { return s.requireRole(storage.RoleAdmin, h) }
	mux.Handle("GET /api/projects/{id}/automations", auth(s.listAutomations))
	mux.Handle("POST /api/projects/{id}/automations", admin(s.createAutomation))
	mux.Handle("GET /api/automations/preview-schedule", auth(s.previewSchedule))
	mux.Handle("GET /api/automations/{id}", auth(s.getAutomation))
	mux.Handle("PATCH /api/automations/{id}", admin(s.updateAutomation))
	mux.Handle("DELETE /api/automations/{id}", admin(s.deleteAutomation))
	mux.Handle("POST /api/automations/{id}/run", admin(s.runAutomation))
	mux.Handle("POST /api/automations/{id}/rotate-secret", admin(s.rotateSecret))
	mux.Handle("POST /api/automations/{id}/conversation", auth(s.automationConversation))
	mux.Handle("POST /api/projects/{id}/automations/test-script", admin(s.testScript))

	mux.Handle("GET /api/jobs", auth(s.listJobs))
	mux.Handle("GET /api/jobs/stats", auth(s.jobStats))
	mux.Handle("GET /api/jobs/{id}", auth(s.getJob))
	mux.Handle("POST /api/jobs/{id}/cancel", admin(s.cancelJob))
	mux.Handle("POST /api/jobs/{id}/retry", admin(s.retryJob))
}

type automationDTO struct {
	ID             string                     `json:"id"`
	ProjectID      string                     `json:"project_id"`
	Name           string                     `json:"name"`
	Enabled        bool                       `json:"enabled"`
	Source         string                     `json:"source"`
	Config         map[string]any             `json:"config"`
	Action         string                     `json:"action"`
	AgentID        string                     `json:"agent_id"`
	Prompt         string                     `json:"prompt"`
	EditMode       string                     `json:"edit_mode"`
	KeepContext    bool                       `json:"keep_context"`
	Limits         storage.AutomationLimits   `json:"limits"`
	Script         storage.AutomationScript   `json:"script"`
	Escalate       storage.AutomationEscalate `json:"escalate"`
	Failures       int                        `json:"failures"`
	DisabledCode   string                     `json:"disabled_code"`
	DisabledReason string                     `json:"disabled_reason"`
	LastRunAt      *time.Time                 `json:"last_run_at"`
	NextRunAt      *time.Time                 `json:"next_run_at"`
	WebhookURL     string                     `json:"webhook_url,omitempty"`
	LastJob        *jobDTO                    `json:"last_job"`
	CreatedAt      time.Time                  `json:"created_at"`
}

func (s *server) toAutomationDTO(r *http.Request, a storage.Automation) automationDTO {
	c := a.Config
	cfg := map[string]any{"every_minutes": c.EveryMinutes, "cron": c.Cron, "timezone": c.Timezone, "auth": c.Auth, "auth_name": c.AuthName}
	d := automationDTO{ID: a.ID, ProjectID: a.ProjectID, Name: a.Name, Enabled: a.Enabled, Source: a.Source, Config: cfg, Action: a.Action,
		AgentID: a.AgentID, Prompt: a.Prompt, EditMode: a.EditMode, KeepContext: a.KeepContext, Limits: a.Limits, Script: a.Script, Escalate: a.Escalate, Failures: a.Failures,
		DisabledCode: a.DisabledCode, DisabledReason: a.DisabledReason, LastRunAt: a.LastRunAt, NextRunAt: a.NextRunAt, CreatedAt: a.CreatedAt}
	if a.Source == "webhook" {
		d.WebhookURL = "/hooks/" + a.ID
	}
	if jobs, err := s.cfg.Store.Jobs().List(r.Context(), storage.JobFilter{Origin: "automation", OriginID: a.ID, Limit: 1}); err == nil && len(jobs) > 0 {
		j := s.toJobDTO(r, jobs[0], nil)
		d.LastJob = &j
	}
	return d
}

type automationInput struct {
	Name        string                     `json:"name"`
	Enabled     *bool                      `json:"enabled"`
	Source      string                     `json:"source"`
	Action      string                     `json:"action"`
	AgentID     string                     `json:"agent_id"`
	Prompt      string                     `json:"prompt"`
	EditMode    string                     `json:"edit_mode"`
	KeepContext bool                       `json:"keep_context"`
	Config      storage.AutomationConfig   `json:"config"`
	Limits      storage.AutomationLimits   `json:"limits"`
	Script      storage.AutomationScript   `json:"script"`
	Escalate    storage.AutomationEscalate `json:"escalate"`
	// ConversationID ties the chat that built it (ADR-042)
	ConversationID string `json:"conversation_id"`
}

// apply validates in and puts it on a (the secret hash and state stay).
func (s *server) applyAutomation(r *http.Request, in automationInput, a *storage.Automation) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("hãy đặt tên cho tự động hóa")
	}
	if in.Source != "schedule" && in.Source != "webhook" {
		return errors.New("nguồn phải là lịch chạy hoặc webhook")
	}
	if in.Action != "chat" && in.Action != "task" && in.Action != "script" {
		return errors.New("hành động phải là gửi tin (chat), giao Việc (task) hoặc chạy code (script)")
	}
	if in.Action == "script" {
		spec := trigger.Spec{Name: in.Name, Source: in.Source, EveryMinutes: in.Config.EveryMinutes, Cron: in.Config.Cron, Timezone: in.Config.Timezone,
			Action: in.Action, Script: in.Script, Escalate: in.Escalate}
		if err := spec.Check(); err != nil {
			return err
		}
	}
	if err := trigger.CheckAgents(r.Context(), s.cfg.Store, a.ProjectID, in.AgentID, in.Escalate.AgentID); err != nil {
		return err
	}
	cfg := storage.AutomationConfig{ConversationID: a.Config.ConversationID, SecretHash: a.Config.SecretHash}
	if in.Source == "schedule" {
		cfg.EveryMinutes, cfg.Cron, cfg.Timezone = in.Config.EveryMinutes, strings.TrimSpace(in.Config.Cron), strings.TrimSpace(in.Config.Timezone)
		if cfg.Cron != "" {
			cfg.EveryMinutes = 0
		}
		if err := trigger.Validate(cfg); err != nil {
			return err
		}
	} else {
		cfg.Auth, cfg.AuthName = in.Config.Auth, strings.TrimSpace(in.Config.AuthName)
		if cfg.Auth != "header" && cfg.Auth != "query" {
			cfg.Auth = "bearer"
		}
	}
	lim := in.Limits
	lim.MaxRunsPerHour, lim.DisableAfterFailures = max(lim.MaxRunsPerHour, 0), max(lim.DisableAfterFailures, 0)
	lim.DebounceSeconds, lim.DebounceMaxSeconds = min(max(lim.DebounceSeconds, 0), 3600), min(max(lim.DebounceMaxSeconds, 0), 6*3600)
	lim.DailyCostUSD = max(lim.DailyCostUSD, 0)
	a.Name, a.Source, a.Action, a.AgentID, a.Prompt, a.KeepContext = in.Name, in.Source, in.Action, in.AgentID, in.Prompt, in.KeepContext
	a.EditMode, a.Config, a.Limits = s.allowedEditMode(r, in.EditMode), cfg, lim
	a.Script, a.Escalate = storage.AutomationScript{}, storage.AutomationEscalate{}
	if a.Action == "script" {
		a.Script, a.Escalate = in.Script, in.Escalate
		if a.Escalate.When == "" {
			a.Escalate.When = "failure"
		}
	}
	if in.Enabled != nil {
		if *in.Enabled && !a.Enabled {
			a.Failures, a.DisabledCode, a.DisabledReason = 0, "", "" // turned back on
		}
		a.Enabled = *in.Enabled
	}
	a.NextRunAt = nil
	if a.Source == "schedule" {
		if next, err := trigger.Next(a.Config, time.Now().UTC()); err == nil {
			a.NextRunAt = &next
		}
	}
	return nil
}

func (s *server) listAutomations(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Automations().List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]automationDTO, 0, len(list))
	for _, a := range list {
		out = append(out, s.toAutomationDTO(r, a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"automations": out})
}

func (s *server) createAutomation(w http.ResponseWriter, r *http.Request) {
	var in automationInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	a := storage.Automation{ProjectID: p.ID, Enabled: true, CreatedBy: userFrom(r).Email}
	if err := s.applyAutomation(r, in, &a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	secret := ""
	if a.Source == "webhook" {
		secret, a.Config.SecretHash = trigger.NewSecret()
	}
	a, err = s.cfg.Store.Automations().Create(r.Context(), a)
	if err != nil {
		s.audit(r, audit.Change{Action: "automation.create", ProjectID: p.ID, After: a, Err: err})
		s.internal(w, r, err)
		return
	}
	s.linkBuilder(r, a, in.ConversationID)
	s.audit(r, audit.Change{Action: "automation.create", ResourceID: a.ID, ProjectID: p.ID, After: s.toAutomationDTO(r, a),
		Detail: map[string]any{"name": a.Name, "source": a.Source}})
	out := map[string]any{"automation": s.toAutomationDTO(r, a)}
	if secret != "" {
		out["secret"] = secret // shown once
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *server) getAutomation(w http.ResponseWriter, r *http.Request) {
	a, err := s.cfg.Store.Automations().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"automation": s.toAutomationDTO(r, a)})
}

func (s *server) updateAutomation(w http.ResponseWriter, r *http.Request) {
	var in automationInput
	if !decode(w, r, &in) {
		return
	}
	a, err := s.cfg.Store.Automations().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	old := a
	wasWebhook := a.Source == "webhook"
	if err := s.applyAutomation(r, in, &a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	secret := ""
	if a.Source == "webhook" && !wasWebhook {
		secret, a.Config.SecretHash = trigger.NewSecret()
	}
	if a.Source != "webhook" {
		a.Config.SecretHash = ""
	}
	change := audit.Change{Action: "automation.update", ResourceID: a.ID, ProjectID: a.ProjectID,
		Before: s.toAutomationDTO(r, old), After: s.toAutomationDTO(r, a), Detail: map[string]any{"name": a.Name, "enabled": a.Enabled}}
	if err := s.cfg.Store.Automations().Update(r.Context(), a); err != nil {
		change.Err = err
		s.audit(r, change)
		s.internal(w, r, err)
		return
	}
	s.linkBuilder(r, a, in.ConversationID)
	s.audit(r, change)
	out := map[string]any{"automation": s.toAutomationDTO(r, a)}
	if secret != "" {
		out["secret"] = secret
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	old, err := s.cfg.Store.Automations().Get(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if err := s.cfg.Store.Automations().Delete(r.Context(), id); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "automation.delete", ResourceID: id, ProjectID: old.ProjectID, Before: s.toAutomationDTO(r, old)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) runAutomation(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Trigger == nil {
		writeError(w, http.StatusServiceUnavailable, "bộ chạy tự động chưa bật")
		return
	}
	a, err := s.cfg.Store.Automations().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	j, _, err := s.cfg.Trigger.Enqueue(r.Context(), a, "manual", "", "", "")
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.cfg.Trigger.StartReady(detached(r), time.Now().UTC())
	s.auditAction(r, "automation.run", a.ID, map[string]any{"job": j.ID})
	writeJSON(w, http.StatusAccepted, map[string]any{"job": s.toJobDTO(r, j, nil)})
}

func (s *server) rotateSecret(w http.ResponseWriter, r *http.Request) {
	a, err := s.cfg.Store.Automations().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if a.Source != "webhook" {
		writeError(w, http.StatusBadRequest, "chỉ webhook mới có secret")
		return
	}
	var secret string
	secret, a.Config.SecretHash = trigger.NewSecret()
	if err := s.cfg.Store.Automations().Update(r.Context(), a); err != nil {
		s.internal(w, r, err)
		return
	}
	s.auditAction(r, "automation.rotate", a.ID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret})
}

func (s *server) previewSchedule(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	every, _ := strconv.Atoi(q.Get("every"))
	cfg := storage.AutomationConfig{EveryMinutes: every, Cron: strings.TrimSpace(q.Get("cron")), Timezone: q.Get("tz")}
	if err := trigger.Validate(cfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"next": []time.Time{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"next": trigger.Upcoming(cfg, time.Now().UTC(), 5)})
}

// linkBuilder ties a building chat of the same project to the automation.
func (s *server) linkBuilder(r *http.Request, a storage.Automation, conversationID string) {
	if conversationID == "" {
		return
	}
	if c, err := s.cfg.Store.Chat().GetConversation(r.Context(), conversationID); err == nil && c.ProjectID == a.ProjectID && c.Purpose == "automation" && c.AutomationID == "" {
		_ = s.cfg.Store.Chat().LinkAutomation(r.Context(), c.ID, a.ID)
	}
}

// automationConversation is the chat that builds an automation (made on first use).
func (s *server) automationConversation(w http.ResponseWriter, r *http.Request) {
	a, err := s.cfg.Store.Automations().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	c, err := s.cfg.Store.Chat().AutomationConversation(r.Context(), a.ID)
	if err != nil {
		if c, err = s.cfg.Chat.StartConversationPurpose(r.Context(), a.ProjectID, "", "automation"); err != nil {
			s.chatError(w, r, err)
			return
		}
		if err := s.cfg.Store.Chat().LinkAutomation(r.Context(), c.ID, a.ID); errors.Is(err, storage.ErrConflict) {
			// another tab made it first: use that one
			_ = s.cfg.Store.Chat().DeleteConversation(r.Context(), c.ID)
			if c, err = s.cfg.Store.Chat().AutomationConversation(r.Context(), a.ID); err != nil {
				s.internal(w, r, err)
				return
			}
		} else if err != nil {
			s.internal(w, r, err)
			return
		} else {
			c.AutomationID = a.ID
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": s.toConvDTO(c)})
}

// testScript runs a script now, without saving it (the builder's "Chạy thử").
func (s *server) testScript(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Script  storage.AutomationScript `json:"script"`
		Payload string                   `json:"payload"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if !trigger.ValidLang(in.Script.Lang) || strings.TrimSpace(in.Script.Body) == "" || len(in.Script.Body) > trigger.MaxScript {
		writeError(w, http.StatusBadRequest, "script cần ngôn ngữ bash, node hoặc python và nội dung không quá 64KB")
		return
	}
	in.Script.TimeoutS = min(max(in.Script.TimeoutS, 1), 120)
	dir := p.Path
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	env := []string{"OFFICE_PAYLOAD=" + in.Payload, "OFFICE_TRIGGER=test", "OFFICE_AUTOMATION=test"}
	out, code, timedOut, err := trigger.RunScript(r.Context(), dir, in.Script, env, in.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditAction(r, "automation.test_script", p.ID, map[string]any{"lang": in.Script.Lang, "exit_code": code})
	writeJSON(w, http.StatusOK, map[string]any{"output": out, "exit_code": code, "timed_out": timedOut})
}
