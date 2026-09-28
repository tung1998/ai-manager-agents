package trigger

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Spec is an automation as an agent proposes it (ADR-041): flat, validated
// before anyone is asked to approve it.
type Spec struct {
	AutomationID string                     `json:"automation_id,omitempty"` // update: which one
	Name         string                     `json:"name"`
	Source       string                     `json:"source"` // schedule | webhook
	EveryMinutes int                        `json:"every_minutes,omitempty"`
	Cron         string                     `json:"cron,omitempty"`
	Timezone     string                     `json:"timezone,omitempty"`
	Action       string                     `json:"action"` // script | chat | task
	AgentID      string                     `json:"agent_id,omitempty"`
	Prompt       string                     `json:"prompt,omitempty"`
	Script       storage.AutomationScript   `json:"script"`
	Escalate     storage.AutomationEscalate `json:"escalate"`
}

// Check rejects what could not run: a bad schedule, an unknown language, a
// script too large or too slow, an unknown escalation.
func (s *Spec) Check() error {
	s.Name = strings.TrimSpace(s.Name)
	switch {
	case s.Name == "":
		return errors.New("tự động hóa cần tên")
	case s.Source != "schedule" && s.Source != "webhook":
		return errors.New("nguồn phải là schedule hoặc webhook")
	case s.Action != "script" && s.Action != "chat" && s.Action != "task":
		return errors.New("hành động phải là script, chat hoặc task")
	}
	if s.Source == "schedule" {
		if err := Validate(s.config()); err != nil {
			return err
		}
	}
	if s.Action == "script" {
		if !ValidLang(s.Script.Lang) {
			return fmt.Errorf("ngôn ngữ script %q không hỗ trợ (bash, node, python)", s.Script.Lang)
		}
		if strings.TrimSpace(s.Script.Body) == "" || len(s.Script.Body) > MaxScript {
			return errors.New("script trống hoặc quá 64KB")
		}
		if s.Script.TimeoutS < 0 || time.Duration(s.Script.TimeoutS)*time.Second > MaxScriptTimeout {
			return errors.New("timeout của script từ 1 tới 3600 giây")
		}
		switch s.Escalate.When {
		case "", "never", "failure", "signal":
		default:
			return errors.New("escalate.when phải là never, failure hoặc signal")
		}
		switch s.Escalate.Action {
		case "", "chat", "task":
		default:
			return errors.New("escalate.action phải là chat hoặc task")
		}
	}
	return nil
}

func (s Spec) config() storage.AutomationConfig {
	c := storage.AutomationConfig{EveryMinutes: s.EveryMinutes, Cron: strings.TrimSpace(s.Cron), Timezone: strings.TrimSpace(s.Timezone)}
	if c.Cron != "" {
		c.EveryMinutes = 0
	}
	return c
}

// Apply puts the spec on a (its id, project, secret and state stay) and
// works out the next run.
func (s Spec) Apply(a *storage.Automation, now time.Time) {
	cfg := s.config()
	cfg.Auth, cfg.AuthName, cfg.SecretHash, cfg.ConversationID = a.Config.Auth, a.Config.AuthName, a.Config.SecretHash, a.Config.ConversationID
	if s.Source == "webhook" && cfg.Auth == "" {
		cfg.Auth = "bearer"
	}
	a.Name, a.Source, a.Action, a.AgentID, a.Prompt, a.Config = s.Name, s.Source, s.Action, s.AgentID, s.Prompt, cfg
	a.Script, a.Escalate = s.Script, s.Escalate
	if a.Escalate.When == "" && a.Action == "script" {
		a.Escalate.When = "failure"
	}
	a.NextRunAt = nil
	if a.Source == "schedule" {
		if next, err := Next(a.Config, now); err == nil {
			a.NextRunAt = &next
		}
	}
}
