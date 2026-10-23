package api

import (
	"net/http"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// providerLimits lists the latest usage windows each connection reported
// (Claude Code subscriptions: 5 hours, the week, per model).
func (s *server) providerLimits(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Providers().List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	type dto struct {
		ProviderID   string                      `json:"provider_id"`
		ProviderName string                      `json:"provider_name"`
		IsDefault    bool                        `json:"is_default"`
		Status       string                      `json:"status"`
		Windows      map[string]chat.LimitWindow `json:"windows"`
		UpdatedAt    time.Time                   `json:"updated_at"`
	}
	out := []dto{}
	for _, p := range list {
		var l chat.Limits
		ok, err := s.cfg.Store.Settings().Get(r.Context(), chat.LimitsKey(p.ID), &l)
		if p.Kind == storage.ProviderCodexCLI {
			// Codex keeps its quota in its session files: the newest one may
			// be from the person's own use, newer than office's last run
			if c := chat.CodexLatestLimits(llm.CodexHomeIn(p.CLIEnv())); c != nil && c.UpdatedAt.After(l.UpdatedAt) {
				l, ok, err = *c, true, nil
			}
		}
		if err != nil || !ok || len(l.Windows) == 0 {
			continue
		}
		// A stored report cannot tell us the new utilization after a reset.
		// Hide expired windows until the connection reports its new cycle.
		active := make(map[string]chat.LimitWindow, len(l.Windows))
		now := time.Now()
		for name, window := range l.Windows {
			if window.ResetsAt.After(now) {
				active[name] = window
			}
		}
		if len(active) == 0 {
			continue
		}
		out = append(out, dto{p.ID, p.Name, p.IsDefault, l.Status, active, l.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"limits": out})
}
