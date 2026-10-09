// Package setup is the AI setup assistant for a project: it sends the scan
// summary and the starter packs to a model, gets back a description, the
// best-fitting pack and tailored agent changes, and turns the accepted
// changes into the project's agents and workflows.
package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/scan"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// ErrNoProvider means no AI connection is available: the UI sends the user to set one up.
var ErrNoProvider = errors.New("chưa có kết nối AI hoạt động")

// AgentChange is one tailored edit on top of the chosen pack.
type AgentChange struct {
	Action       string `json:"action"` // update | add | remove
	Key          string `json:"key"`
	Name         string `json:"name,omitempty"`
	Role         string `json:"role,omitempty"`
	Description  string `json:"description,omitempty"`
	ModelTier    string `json:"model_tier,omitempty"`
	Instructions string `json:"instructions,omitempty"` // add: full prompt; update: project context appended
	ReadOnly     *bool  `json:"read_only,omitempty"`
	Source       string `json:"source,omitempty"` // existing agent file it came from
	Reason       string `json:"reason"`
}

// Proposal is the model's recommendation.
type Proposal struct {
	Description string        `json:"description"`
	PackKey     string        `json:"pack_key"`
	Reason      string        `json:"reason"`
	Confidence  float64       `json:"confidence"`
	Changes     []AgentChange `json:"agent_changes"`
	Notes       []string      `json:"notes"`
}

// Result is a proposal plus the pack it produces and how it was made.
type Result struct {
	Proposal           Proposal   `json:"proposal"`
	Pack               team.Pack  `json:"pack"`
	Problems           []string   `json:"problems"`
	Provider           string     `json:"provider"`
	Model              string     `json:"model"`
	Usage              llm.Result `json:"usage"`
	SuggestedWorkflows []string   `json:"suggested_workflows"`
}

// Assistant runs the setup flow.
type Assistant struct {
	store     storage.Store
	providers *provider.Service
	team      *team.Service
}

// New builds an Assistant.
func New(store storage.Store, providers *provider.Service, tm *team.Service) *Assistant {
	return &Assistant{store: store, providers: providers, team: tm}
}

// Propose asks the default AI connection (strong tier) for a setup.
// projectText is the scan summary (empty for a machine-wide helper); sum is
// the same scan, used to suggest workflows by signal (nil for a
// machine-wide helper); goal is optional free text from the user.
func (a *Assistant) Propose(ctx context.Context, projectID, projectName, projectText string, sum *scan.Summary, goal string) (Result, error) {
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
	packs, err := library()
	if err != nil {
		return Result{}, err
	}
	out, err := a.providers.Call(ctx, p, llm.Request{
		Model: model, System: systemPrompt + languageNote + chat.LoadLanguage(ctx, a.store).Rule(), MaxTokens: 6000,
		Prompt: userPrompt(projectName, projectText, goal, packs),
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
	p2, problems, err := Build(prop.PackKey, prop.Changes)
	if err != nil {
		return res, err
	}
	if problems == nil {
		problems = []string{}
	}
	if res.Proposal.Changes == nil {
		res.Proposal.Changes = []AgentChange{}
	}
	if res.Proposal.Notes == nil {
		res.Proposal.Notes = []string{}
	}
	res.Pack, res.Problems = p2, problems
	res.SuggestedWorkflows = SuggestWorkflows(sum, p2.Workflows)
	if res.SuggestedWorkflows == nil {
		res.SuggestedWorkflows = []string{}
	}
	return res, nil
}

// SuggestWorkflows looks at the scan for signals (tests, CI) and suggests
// shipped workflows not already in the pack. The user may untick any of
// them; nothing here is installed on its own.
func SuggestWorkflows(sum *scan.Summary, packWorkflows []string) []string {
	if sum == nil {
		return nil
	}
	has := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	var out []string
	add := func(key string) {
		if !has(packWorkflows, key) && !has(out, key) {
			out = append(out, key)
		}
	}
	if len(sum.Tests) > 0 {
		add("fix-tests")
	}
	for _, i := range sum.Infra {
		if i == "GitHub Actions" || i == "bitbucket-pipelines.yml" || i == ".gitlab-ci.yml" || i == "Jenkinsfile" {
			add("review-pr")
			break
		}
	}
	return out
}

// Build applies changes to a starter pack and validates the result.
// Problems are returned (not as an error) so the user can untick the
// offending change. extraWorkflows are keys the user kept from
// SuggestWorkflows; unknown keys are kept as-is and skipped at install time.
func Build(packKey string, changes []AgentChange, extraWorkflows ...string) (team.Pack, []string, error) {
	t, err := team.PackByKey(packKey)
	if err != nil {
		return t, nil, err
	}
	for _, k := range extraWorkflows {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		found := false
		for _, w := range t.Workflows {
			if w == k {
				found = true
				break
			}
		}
		if !found {
			t.Workflows = append(t.Workflows, k)
		}
	}
	t = Apply(t, changes)
	var problems []string
	if len(t.Agents) == 0 {
		problems = append(problems, "cần ít nhất một agent")
	}
	if err := team.Validate(t.Agents); err != nil {
		var ve *team.ValidationError
		if !errors.As(err, &ve) {
			return t, nil, err
		}
		problems = append(problems, ve.Problems...)
	}
	return t, problems, nil
}

// Apply edits a pack copy. Unknown keys in update/remove are ignored.
func Apply(t team.Pack, changes []AgentChange) team.Pack {
	agents := append([]team.AgentSpec(nil), t.Agents...)
	idx := func(key string) int {
		for i, a := range agents {
			if a.Key == key {
				return i
			}
		}
		return -1
	}
	for _, c := range changes {
		key := strings.TrimSpace(c.Key)
		switch c.Action {
		case "update":
			i := idx(key)
			if i < 0 {
				continue
			}
			a := &agents[i]
			setIf(&a.Name, c.Name)
			setIf(&a.Role, c.Role)
			setIf(&a.Description, c.Description)
			if storage.ValidTier(c.ModelTier) {
				a.ModelTier = c.ModelTier
			}
			if strings.TrimSpace(c.Instructions) != "" {
				a.Instructions = strings.TrimSpace(a.Instructions + "\n\nBối cảnh project:\n" + strings.TrimSpace(c.Instructions))
			}
			if c.ReadOnly != nil {
				a.Permissions.ReadOnly = *c.ReadOnly
				if !a.Permissions.ReadOnly {
					a.Permissions.RequiresApproval = true // writes always need a human
				}
			}
		case "add":
			if key == "" || idx(key) >= 0 {
				continue
			}
			spec := team.AgentSpec{
				Key: key, Name: orDefault(c.Name, key), Role: c.Role,
				Description: c.Description, ModelTier: c.ModelTier, Instructions: strings.TrimSpace(c.Instructions),
				Permissions: storage.Permissions{ReadOnly: true},
			}
			if !storage.ValidTier(spec.ModelTier) {
				spec.ModelTier = storage.TierFast
			}
			if c.ReadOnly != nil {
				spec.Permissions.ReadOnly = *c.ReadOnly
			}
			if !spec.Permissions.ReadOnly {
				spec.Permissions.RequiresApproval = true // writes always need a human
			}
			agents = append(agents, spec)
		case "remove":
			i := idx(key)
			if i < 0 {
				continue
			}
			agents = append(agents[:i], agents[i+1:]...)
			if t.Default == key && len(agents) > 0 {
				t.Default = agents[0].Key
			}
		}
	}
	t.Agents = agents
	return t
}

// Accept applies the accepted changes and gives the project the pack's
// agents and workflows. A project with no agents yet gets the pack in
// place (as before); a project that already has agents only gets the
// pack's keys it's missing, so a rescan's setup never overwrites or
// removes an agent an admin has hand-edited.
func (a *Assistant) Accept(ctx context.Context, repoID, packKey string, changes []AgentChange, extraWorkflows ...string) (team.Pack, error) {
	p, problems, err := Build(packKey, changes, extraWorkflows...)
	if err != nil {
		return p, err
	}
	if len(problems) > 0 {
		return p, &team.ValidationError{Problems: problems}
	}
	snap, err := a.team.Load(ctx, repoID)
	if err != nil {
		return p, err
	}
	if len(snap.Agents) > 0 {
		if err := a.team.Merge(ctx, repoID, team.Snapshot{Default: p.Default, Agents: p.Agents}, "pack:"+p.Key); err != nil {
			return p, err
		}
		return p, a.team.InstallWorkflows(ctx, repoID, p.Workflows)
	}
	return p, a.team.ApplyPack(ctx, repoID, p, true)
}

type libraryEntry struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Workflows   []string       `json:"workflows,omitempty"`
	Agents      []libraryAgent `json:"agents"`
}

type libraryAgent struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func library() ([]libraryEntry, error) {
	list, err := team.Packs()
	if err != nil {
		return nil, err
	}
	var out []libraryEntry
	for _, p := range list {
		e := libraryEntry{Key: p.Key, Name: p.Name, Description: p.Description, Workflows: p.Workflows}
		for _, ag := range p.Agents {
			e.Agents = append(e.Agents, libraryAgent{Key: ag.Key, Name: ag.Name, Role: ag.Role})
		}
		out = append(out, e)
	}
	return out, nil
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

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return strings.TrimSpace(v)
}
