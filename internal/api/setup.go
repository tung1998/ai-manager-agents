package api

import (
	"errors"
	"net/http"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/scan"
	"bitbucket.org/senprints/agent-office/internal/setup"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// scanProject reads the project folder; machine-wide projects have nothing to scan.
func (s *server) scanProject(w http.ResponseWriter, r *http.Request) (storage.Repo, *scan.Summary, bool) {
	x, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return x, nil, false
	}
	if x.Path == "" {
		return x, nil, true
	}
	sum, err := scan.Scan(x.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Không quét được thư mục project: "+err.Error())
		return x, nil, false
	}
	return x, sum, true
}

func (s *server) setupScan(w http.ResponseWriter, r *http.Request) {
	_, sum, ok := s.scanProject(w, r)
	if !ok {
		return
	}
	size := 0
	if sum != nil {
		size = len(sum.Text())
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": sum, "prompt_chars": size})
}

func (s *server) setupPropose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Goal string `json:"goal"`
		// the automation templates the dashboard offers (they live there)
		Automations []setup.Tool `json:"automations"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	x, sum, ok := s.scanProject(w, r)
	if !ok {
		return
	}
	text := ""
	if sum != nil {
		text = sum.Text()
	} else if strings.TrimSpace(in.Goal) == "" {
		writeError(w, http.StatusBadRequest, "Project toàn máy không có thư mục để quét: hãy mô tả bạn muốn helper làm gì")
		return
	}
	goal := strings.TrimSpace(in.Goal)
	if goal == "" { // a project made from an idea (ADR-139) has it there: the setup keeps it, never writes it over
		goal = x.Description
	}
	res, err := s.cfg.Setup.Propose(r.Context(), x.ID, x.Name, text, goal, in.Automations)
	var be *usage.BudgetError
	if errors.As(err, &be) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": be.Error(), "code": "budget"})
		return
	}
	if errors.Is(err, setup.ErrNoProvider) {
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": "Cần kết nối AI trước khi thiết lập bằng AI", "code": "no_provider"})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.auditAction(r, "project.setup_propose", x.ID, map[string]any{"agents": len(res.Proposal.Agents), "model": res.Model,
		"input_tokens": res.Usage.InputTokens, "output_tokens": res.Usage.OutputTokens})
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "summary": sum})
}

type setupChoice struct {
	Default     string            `json:"default_agent"`
	Agents      []setup.AgentPick `json:"agents"` // the library agents the person kept, as they edited them
	Description *string           `json:"description"`
	// what the person kept of the proposal's workflows, skills, MCP servers
	// and quick checks (nil quick checks: leave the project's as they are);
	// the dashboard makes the automations (their templates live there)
	Workflows   []string `json:"workflows,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	MCP         []string `json:"mcp,omitempty"`
	QuickChecks *string  `json:"quick_checks,omitempty"`
}

// setupBuild previews the agents a choice adds, with what blocks it.
func (s *server) setupBuild(w http.ResponseWriter, r *http.Request) {
	var in setupChoice
	if !decode(w, r, &in) {
		return
	}
	existing, err := s.cfg.Store.Agents().List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	policy := perm.LoadPolicy(r.Context(), s.cfg.Store, r.PathValue("id"))
	plan, problems, warnings := setup.Build(in.Agents, in.Default, existing, policy.Catalog)
	if problems == nil {
		problems = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "problems": problems, "warnings": warnings})
}

func (s *server) setupApply(w http.ResponseWriter, r *http.Request) {
	var in setupChoice
	if !decode(w, r, &in) {
		return
	}
	x, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if _, err := s.cfg.Setup.Accept(r.Context(), x.ID, in.Agents, in.Default, in.Workflows); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	x, _ = s.cfg.Store.Repos().Get(r.Context(), x.ID) // its default agent is set
	if in.Description != nil {
		x.Description = strings.TrimSpace(*in.Description)
		if err := s.cfg.Store.Repos().UpdateInfo(r.Context(), x.ID, x.Name, x.Description); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	ready := append(s.setupExtras(r, x, in), s.readiness(r.Context(), x.ID)...)
	ids := make([]string, 0, len(in.Agents))
	for _, a := range in.Agents {
		ids = append(ids, a.ID)
	}
	s.auditAction(r, "project.setup_apply", x.ID, map[string]any{"agents": ids, "workflows": in.Workflows,
		"skills": in.Skills, "mcp": in.MCP})
	d, _ := s.repoDTO(r, x, true)
	writeJSON(w, http.StatusOK, map[string]any{"project": d, "ready": ready})
}
