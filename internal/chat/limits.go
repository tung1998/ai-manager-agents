package chat

import (
	"context"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// LimitWindow is one usage window of a subscription (Claude Code reports
// them: five_hour, seven_day, seven_day_<model>…).
type LimitWindow struct {
	Utilization float64   `json:"utilization"` // 0..1
	ResetsAt    time.Time `json:"resets_at"`
}

// Limits is the latest usage a provider reported.
type Limits struct {
	Status    string                 `json:"status"` // allowed | allowed_warning | rejected
	Windows   map[string]LimitWindow `json:"windows"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// ContextUse is how full the model's context was after a turn.
type ContextUse struct {
	Tokens int `json:"tokens"`
	Window int `json:"window"`
}

// LimitsKey is where a provider's latest Limits are kept (settings).
func LimitsKey(providerID string) string { return "provider_limits:" + providerID }

// keepLimits stores the usage windows a run reported for its provider.
func (e *Engine) keepLimits(p storage.Provider, l *Limits) {
	if l == nil || (l.Status == "" && len(l.Windows) == 0) {
		return
	}
	if len(l.Windows) == 0 && l.Status != "rejected" {
		// Status-only report with no window detail: nothing new to persist,
		// and saving it would wipe out whatever window data we already had.
		return
	}
	_ = e.store.Settings().Set(context.Background(), LimitsKey(p.ID), l)
	if fn := e.onLimits; fn != nil { // the limit alerts
		go fn(p, *l)
	}
}

// SetOnLimits hears every usage report of a provider (the limit alerts).
func (e *Engine) SetOnLimits(fn func(p storage.Provider, l Limits)) { e.onLimits = fn }
