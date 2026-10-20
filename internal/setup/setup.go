// Package setup is the AI setup assistant for a project (ADR-166): it sends
// the scan summary and office's libraries to a model, gets back a
// description and what the project needs from them (library agents tailored
// to it, workflows, automations, skills, MCP servers), and adds the accepted
// agents and workflows to the project. It only adds: agents the project has
// stay as they are.
package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// ErrNoProvider means no AI connection is available: the UI sends the user to set one up.
var ErrNoProvider = errors.New("chưa có kết nối AI hoạt động")

// AgentPick is a library agent the setup adds, tailored to the project; the
// person may edit it before it is added.
type AgentPick struct {
	ID           string `json:"id"`            // library id: office/devops, agency/frontend-developer
	Key          string `json:"key,omitempty"` // "": the library's
	Name         string `json:"name,omitempty"`
	Role         string `json:"role,omitempty"`
	Description  string `json:"description,omitempty"`
	ModelTier    string `json:"model_tier,omitempty"`
	Instructions string `json:"instructions,omitempty"` // project context added to the library's instructions
	// Level is the agent's permission package (internal/perm.Read..Operate);
	// nil keeps the library's.
	Level *string `json:"level,omitempty"`
	// Caps are the agent's own picks of capabilities (internal/perm.Caps);
	// unknown ids are dropped and reported in Warnings.
	Caps *[]string `json:"caps,omitempty"`
	// Commands narrow the project's command catalog for this agent; ones
	// outside it are reported in Warnings (Resolve ignores them at run time).
	Commands *[]string `json:"commands,omitempty"`
	Reason   string    `json:"reason"`
}

// Proposal is the model's recommendation.
type Proposal struct {
	Description string  `json:"description"`
	Reason      string  `json:"reason"`
	Confidence  float64 `json:"confidence"`
	// Default is the key of the agent answering when nobody is named, for a
	// project with no agents yet (a project's own default is kept).
	Default string      `json:"default_agent"`
	Agents  []AgentPick `json:"agents"`
	// Workflows, Automations, Skills and MCP are picked from the Toolbox
	// (unknown names dropped); QuickChecks are the project's own checks by
	// file ending (ADR-135).
	Workflows   []Pick   `json:"workflows"`
	Automations []Pick   `json:"automations"`
	Skills      []Pick   `json:"skills"`
	MCP         []Pick   `json:"mcp"`
	QuickChecks string   `json:"quick_checks"`
	Notes       []string `json:"notes"`
}

// Plan is the agents the picks add to a project.
type Plan struct {
	Default string           `json:"default"` // "": the project's own stays
	Agents  []team.AgentSpec `json:"agents"`
}

// Result is a proposal plus the agents it adds and how it was made.
type Result struct {
	Proposal Proposal `json:"proposal"`
	Plan     Plan     `json:"plan"`
	// Problems block Accept (e.g. team.Validate failures, no agent at all).
	Problems []string `json:"problems"`
	// Warnings don't block: an unknown pick or level/cap was dropped, or a
	// command isn't in the project's catalog.
	Warnings []string   `json:"warnings"`
	Provider string     `json:"provider"`
	Model    string     `json:"model"`
	Usage    llm.Result `json:"usage"`
}

// Assistant runs the setup flow.
type Assistant struct {
	store     storage.Store
	providers *provider.Service
	team      *team.Service
	// Toolbox lists the workflows, skills and MCP the setup may pick (nil:
	// none); hint is the project's scan and goal, to pick from big catalogs.
	Toolbox func(hint string) Toolbox
}

// New builds an Assistant.
func New(store storage.Store, providers *provider.Service, tm *team.Service) *Assistant {
	return &Assistant{store: store, providers: providers, team: tm}
}

// maxAgents caps the agents the model adds.
const maxAgents = 8

// Propose asks the default AI connection (strong tier) for a setup.
// projectText is the scan summary (empty for a machine-wide helper); goal is
// optional free text from the user; automations are the templates the
// dashboard offers (they live there).
func (a *Assistant) Propose(ctx context.Context, projectID, projectName, projectText, goal string, automations []Tool) (Result, error) {
	p, model, err := a.providers.ResolveModel(ctx, storage.Agent{ModelTier: storage.TierStrong})
	if errors.Is(err, storage.ErrNotFound) || (err == nil && (!p.Enabled || p.Status == "error")) {
		return Result{}, ErrNoProvider
	}
	if err != nil {
		return Result{}, err
	}
	if _, err := a.providers.Client(p); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNoProvider, err)
	}
	existing, err := a.store.Agents().List(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	agents, err := libraryAgents()
	if err != nil {
		return Result{}, err
	}
	policy := perm.LoadPolicy(ctx, a.store, projectID)
	var box Toolbox
	if a.Toolbox != nil {
		box = a.Toolbox(projectText + "\n" + goal)
	}
	box.Agents, box.Automations = agents, automations
	out, err := a.providers.Call(ctx, p, llm.Request{
		Model: model, System: systemPrompt + languageNote + chat.LoadLanguage(ctx, a.store).Rule(), MaxTokens: 8000,
		Prompt: userPrompt(projectName, projectText, goal, current(existing), policy.Catalog, box),
	}, usage.Meta{Kind: "setup_propose", ProjectID: projectID})
	if err != nil {
		return Result{}, fmt.Errorf("gọi AI lỗi: %w", err)
	}
	var prop Proposal
	if err := json.Unmarshal([]byte(extractJSON(out.Text)), &prop); err != nil {
		return Result{}, fmt.Errorf("AI trả về không đúng định dạng JSON: %w", err)
	}
	res := Result{Proposal: prop, Provider: p.Name, Model: out.Model, Usage: out}
	if res.Model == "" {
		res.Model = model
	}
	res.Usage.Text = ""
	if len(prop.Agents) > maxAgents {
		prop.Agents = prop.Agents[:maxAgents]
	}
	res.Proposal.Agents = prop.Agents
	if res.Proposal.Agents == nil {
		res.Proposal.Agents = []AgentPick{}
	}
	if res.Proposal.Notes == nil {
		res.Proposal.Notes = []string{}
	}
	plan, problems, warnings := Build(res.Proposal.Agents, prop.Default, existing, policy.Catalog)
	var w []string
	for _, l := range []struct {
		kind  string
		picks *[]Pick
		tools []Tool
	}{{"quy trình", &res.Proposal.Workflows, box.Workflows}, {"tự động hóa", &res.Proposal.Automations, box.Automations},
		{"skill", &res.Proposal.Skills, box.Skills}, {"MCP", &res.Proposal.MCP, box.MCP}} {
		*l.picks, w = cleanPicks(l.kind, *l.picks, l.tools)
		warnings = append(warnings, w...)
	}
	res.Proposal.QuickChecks, w = cleanQuickChecks(prop.QuickChecks)
	warnings = append(warnings, w...)
	res.Plan, res.Problems, res.Warnings = plan, orEmpty(problems), orEmpty(warnings)
	return res, nil
}

// Build turns picks into the agents they add next to existing ones, and
// checks them. A pick whose key the project (or an earlier pick) has is
// left out with a warning: the setup only adds. def is the default for a
// project with no agents (else the first pick). Problems block Accept;
// Warnings don't (an unknown pick, level or cap was dropped, or a command
// isn't in catalog, the project's command catalog).
func Build(picks []AgentPick, def string, existing []storage.Agent, catalog []string) (Plan, []string, []string) {
	plan := Plan{Agents: []team.AgentSpec{}}
	var problems, warnings []string
	have := map[string]bool{}
	all := make([]team.AgentSpec, 0, len(existing)+len(picks))
	for _, a := range existing {
		have[a.Key] = true
		all = append(all, team.SpecOf(a))
	}
	for _, p := range picks {
		lib, err := team.LibraryAgentByID(strings.TrimSpace(p.ID))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("agent %q không có trong thư viện, đã bỏ", p.ID))
			continue
		}
		spec := lib.Spec()
		if k := strings.TrimSpace(p.Key); k != "" {
			spec.Key = k
		}
		if have[spec.Key] {
			warnings = append(warnings, fmt.Sprintf("project đã có agent %q, bỏ qua", spec.Key))
			continue
		}
		have[spec.Key] = true
		setIf(&spec.Name, p.Name)
		setIf(&spec.Role, p.Role)
		setIf(&spec.Description, p.Description)
		if storage.ValidTier(p.ModelTier) {
			spec.ModelTier = p.ModelTier
		}
		if c := strings.TrimSpace(p.Instructions); c != "" {
			spec.Instructions = strings.TrimSpace(spec.Instructions + "\n\nBối cảnh project:\n" + c)
		}
		warnings = append(warnings, applyPermissions(&spec.Permissions, p, catalog)...)
		plan.Agents = append(plan.Agents, spec)
		all = append(all, spec)
	}
	if len(existing) == 0 {
		switch {
		case len(plan.Agents) == 0:
			problems = append(problems, "cần ít nhất một agent")
		case slices.ContainsFunc(plan.Agents, func(a team.AgentSpec) bool { return a.Key == def }):
			plan.Default = def
		default:
			plan.Default = plan.Agents[0].Key
		}
	}
	if err := team.Validate(all); err != nil {
		var ve *team.ValidationError
		if errors.As(err, &ve) {
			problems = append(problems, ve.Problems...)
		} else {
			problems = append(problems, err.Error())
		}
	}
	return plan, problems, warnings
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// validCaps keeps only the known capability ids, reporting the rest as warnings.
func validCaps(ids []string) (ok []string, warnings []string) {
	for _, id := range ids {
		found := false
		for _, c := range perm.Caps {
			if c.ID == id {
				found = true
				break
			}
		}
		if found {
			ok = append(ok, id)
		} else {
			warnings = append(warnings, fmt.Sprintf("quyền %q không có", id))
		}
	}
	return ok, warnings
}

// applyPermissions sets Level/Caps/Commands from a pick onto an agent's
// permissions (the library's stay where the pick says nothing). Problems
// here never block: bad input is dropped and reported as a warning.
func applyPermissions(perms *storage.Permissions, c AgentPick, catalog []string) []string {
	var warnings []string
	if c.Level != nil {
		if perm.Valid(*c.Level) {
			perms.Level, perms.ReadOnly = *c.Level, *c.Level == perm.Read
		} else {
			warnings = append(warnings, fmt.Sprintf("mức quyền %q không hợp lệ", *c.Level))
		}
	}
	if c.Caps != nil {
		ok, probs := validCaps(*c.Caps)
		perms.Caps = &ok
		warnings = append(warnings, probs...)
	}
	if c.Commands != nil {
		cmds := append([]string(nil), *c.Commands...)
		perms.Commands = &cmds
		if len(catalog) > 0 {
			for _, cmd := range cmds {
				if !slices.Contains(catalog, cmd) {
					warnings = append(warnings, fmt.Sprintf("lệnh %q không có trong catalog project", cmd))
				}
			}
		}
	}
	return warnings
}

// Accept adds the picked agents (and the workflows the person kept) to a
// project; the agents it has are never changed or removed. Warnings (see
// Build) don't block.
func (a *Assistant) Accept(ctx context.Context, repoID string, picks []AgentPick, def string, workflows []string) (Plan, error) {
	existing, err := a.store.Agents().List(ctx, repoID)
	if err != nil {
		return Plan{}, err
	}
	policy := perm.LoadPolicy(ctx, a.store, repoID)
	plan, problems, _ := Build(picks, def, existing, policy.Catalog)
	if len(problems) > 0 {
		return plan, &team.ValidationError{Problems: problems}
	}
	if len(plan.Agents) > 0 {
		ids := make([]string, 0, len(picks))
		for _, p := range picks {
			ids = append(ids, p.ID)
		}
		if err := a.team.Merge(ctx, repoID, team.Snapshot{Default: plan.Default, Agents: plan.Agents}, "setup:"+strings.Join(ids, ",")); err != nil {
			return plan, err
		}
	}
	return plan, a.team.InstallWorkflows(ctx, repoID, workflows)
}

// libraryAgents is the agent library as the model sees it: id, name, role.
func libraryAgents() ([]Tool, error) {
	list, err := team.Library()
	if err != nil {
		return nil, err
	}
	out := make([]Tool, 0, len(list))
	for _, e := range list {
		out = append(out, Tool{Name: e.ID, Description: e.Name + ": " + firstNonEmpty(e.Role, e.Description)})
	}
	return out, nil
}

// current is a project's agents as the model sees them (it adds no twin).
func current(agents []storage.Agent) []Tool {
	out := make([]Tool, 0, len(agents))
	for _, a := range agents {
		out = append(out, Tool{Name: a.Key, Description: a.Name + ": " + firstNonEmpty(a.Role, a.Description)})
	}
	return out
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// extractJSON takes the ```json block, or the outermost {...}.
func extractJSON(s string) string {
	if i := strings.Index(s, "```json"); i >= 0 {
		rest := s[i+7:]
		if j := strings.Index(rest, "```"); j >= 0 {
			return strings.TrimSpace(rest[:j])
		}
	}
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func setIf(dst *string, v string) {
	if strings.TrimSpace(v) != "" {
		*dst = strings.TrimSpace(v)
	}
}
