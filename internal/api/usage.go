package api

import (
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/senprints/agent-office/internal/usage"
)

func (s *server) usageSummary(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	sum, err := s.cfg.Usage.Summarize(r.Context(), days)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

type runDTO struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	ProjectID    string    `json:"project_id"`
	ProjectName  string    `json:"project_name"`
	AgentID      string    `json:"agent_id"`
	ProviderName string    `json:"provider_name"`
	Model        string    `json:"model"`
	Status       string    `json:"status"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CostUSD      *float64  `json:"cost_usd"`
	CostSource   string    `json:"cost_source"`
	DurationMS   int64     `json:"duration_ms"`
	Error        string    `json:"error"`
	Actor        string    `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *server) usageRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 || days > 90 {
		days = 30
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	runs, err := s.cfg.Usage.Runs(r.Context(), q.Get("project"), days, limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	names := map[string]string{}
	out := make([]runDTO, 0, len(runs))
	for _, x := range runs {
		name, ok := names[x.ProjectID]
		if !ok && x.ProjectID != "" {
			if p, err := s.cfg.Store.Repos().Get(r.Context(), x.ProjectID); err == nil {
				name = p.Name
			}
			names[x.ProjectID] = name
		}
		out = append(out, runDTO{ID: x.ID, Kind: x.Kind, ProjectID: x.ProjectID, ProjectName: name, AgentID: x.AgentID,
			ProviderName: x.ProviderName, Model: x.Model, Status: x.Status, InputTokens: x.InputTokens, OutputTokens: x.OutputTokens,
			CostUSD: x.CostUSD, CostSource: x.CostSource, DurationMS: x.DurationMS, Error: x.Error, Actor: x.Actor, CreatedAt: x.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (s *server) usageSettings(w http.ResponseWriter, r *http.Request) {
	var in usage.Settings
	if !decode(w, r, &in) {
		return
	}
	if err := s.cfg.Usage.SaveSettings(r.Context(), in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditAction(r, "usage.settings", "", map[string]any{"daily_limit_usd": in.DailyLimitUSD, "project_limits": len(in.ProjectLimits), "prices": len(in.Prices)})
	st, _ := s.cfg.Usage.Settings(r.Context())
	writeJSON(w, http.StatusOK, st)
}
