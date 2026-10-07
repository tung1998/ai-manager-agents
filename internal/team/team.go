// Package team manages a project's agents (ADR-099): the agents themselves,
// the default one that answers when nobody is named, the starter packs a new
// project begins with, and a snapshot before every change so it can be rolled
// back. How agents work together is not here: that is a workflow
// (internal/workflow).
package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/templates/packs"
)

// KeepRevisions is how many snapshots are kept per project.
const KeepRevisions = 50

// WithActor records who is making changes, for the revision history.
func WithActor(ctx context.Context, who string) context.Context { return actor.With(ctx, who) }

// AgentSpec is the portable form of an agent (packs, snapshots, export).
type AgentSpec struct {
	Key           string              `json:"key"`
	Name          string              `json:"name"`
	Role          string              `json:"role,omitempty"`
	Description   string              `json:"description,omitempty"`
	ModelTier     string              `json:"model_tier"`
	LLMModel      string              `json:"llm_model,omitempty"`
	Effort        string              `json:"effort,omitempty"`                // thinking level ("" = the CLI's own)
	ProviderID    string              `json:"provider_id,omitempty"`           // this office's connection id
	Provider      string              `json:"provider,omitempty"`              // connection name, used by export/import between offices
	Fallbacks     []string            `json:"fallback_provider_ids,omitempty"` // this office's connection ids, tried next in order
	FallbackNames []string            `json:"fallback_providers,omitempty"`    // their names, used by export/import between offices
	Instructions  string              `json:"instructions,omitempty"`
	Permissions   storage.Permissions `json:"permissions"`
	Avatar        storage.Avatar      `json:"avatar,omitzero"`
}

// Snapshot is a project's agents at one moment (a revision, an export).
type Snapshot struct {
	Default string      `json:"default,omitempty"` // the default agent's key
	Agents  []AgentSpec `json:"agents"`
}

// Pack is a starter pack: the agents a new project begins with, which of
// them is the default, and the workflows installed for it.
type Pack struct {
	Key         string      `json:"key"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Default     string      `json:"default,omitempty"`
	Workflows   []string    `json:"workflows,omitempty"`
	Agents      []AgentSpec `json:"agents"`
}

// Packs returns the shipped starter packs, smallest first.
func Packs() ([]Pack, error) {
	order := map[string]int{"solo": 0, "team": 1, "council": 2}
	entries, err := fs.ReadDir(packs.FS, ".")
	if err != nil {
		return nil, err
	}
	var out []Pack
	for _, e := range entries {
		raw, err := fs.ReadFile(packs.FS, e.Name())
		if err != nil {
			return nil, err
		}
		var p Pack
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("team: %s: %w", e.Name(), err)
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Key] < order[out[j].Key] })
	return out, nil
}

// ErrNoPack: no starter pack has that key.
var ErrNoPack = errors.New("không có gói khởi tạo này")

// PackByKey finds a shipped starter pack.
func PackByKey(key string) (Pack, error) {
	list, err := Packs()
	if err != nil {
		return Pack{}, err
	}
	for _, p := range list {
		if p.Key == key {
			return p, nil
		}
	}
	return Pack{}, fmt.Errorf("%w: %q", ErrNoPack, key)
}

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

// ValidKey reports whether k may be an agent key.
func ValidKey(k string) bool { return keyRe.MatchString(k) }

// ValidationError lists every problem of a set of agents.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "agent không hợp lệ: " + strings.Join(e.Problems, "; ")
}

// Validate checks a project's agents as a whole: keys, names, model tiers.
func Validate(agents []AgentSpec) error {
	var p []string
	add := func(f string, a ...any) { p = append(p, fmt.Sprintf(f, a...)) }
	seen := map[string]bool{}
	for _, a := range agents {
		if !keyRe.MatchString(a.Key) {
			add("agent key %q phải là chữ thường, số, dấu gạch ngang", a.Key)
		}
		if seen[a.Key] {
			add("agent key %q bị trùng", a.Key)
		}
		seen[a.Key] = true
		if strings.TrimSpace(a.Name) == "" {
			add("agent %q thiếu tên", a.Key)
		}
		if !storage.ValidTier(a.ModelTier) {
			add("agent %q có hạng model %q không hợp lệ (strong|balanced|fast)", a.Key, a.ModelTier)
		}
	}
	if len(p) > 0 {
		return &ValidationError{Problems: p}
	}
	return nil
}

// SpecOf is an agent in its portable form.
func SpecOf(a storage.Agent) AgentSpec {
	return AgentSpec{
		Key: a.Key, Name: a.Name, Role: a.Role, Description: a.Description,
		ModelTier: a.ModelTier, LLMModel: a.LLMModel, Instructions: a.Instructions, Permissions: a.Permissions,
		ProviderID: a.ProviderID, Fallbacks: a.FallbackProviderIDs, Effort: a.Effort, Avatar: a.Avatar,
	}
}

// Agent is the spec as an agent of the project.
func (s AgentSpec) Agent(projectID string, sort int) storage.Agent {
	// ADR-074 security: FullAccess/ExtraDirs never come from a pack, an
	// import or a restore: only the admin's agent edit may turn them on.
	perms := s.Permissions
	perms.FullAccess, perms.FullAccessBy, perms.ExtraDirs = false, "", nil
	effort := s.Effort
	if !storage.ValidEffort(effort) {
		effort = ""
	}
	tier := s.ModelTier
	if tier == "" {
		tier = storage.TierBalanced
	}
	return storage.Agent{
		ProjectID: projectID, Key: s.Key, Name: s.Name, Role: s.Role, Description: s.Description,
		ModelTier: tier, LLMModel: s.LLMModel, Instructions: s.Instructions,
		Permissions: perms, Sort: sort, ProviderID: s.ProviderID, FallbackProviderIDs: s.Fallbacks, Effort: effort, Avatar: s.Avatar,
	}
}

// Export is a project's agents in portable form.
func Export(r storage.Repo, agents []storage.Agent) Snapshot {
	snap := Snapshot{Agents: []AgentSpec{}}
	if d, ok := storage.DefaultAgent(r, agents); ok {
		snap.Default = d.Key
	}
	for _, a := range agents {
		snap.Agents = append(snap.Agents, SpecOf(a))
	}
	return snap
}

// RevisionSnapshot decodes a revision.
func RevisionSnapshot(r storage.Revision) (Snapshot, error) {
	var s Snapshot
	err := json.Unmarshal(r.Snapshot, &s)
	return s, err
}
