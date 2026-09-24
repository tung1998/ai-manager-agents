package orgmodel

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/templates/models"
)

// Template is the portable form of an org model (built-in JSON, export, import).
type Template struct {
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Kind        string             `json:"kind"`
	Description string             `json:"description"`
	Governance  storage.Governance `json:"governance"`
	Agents      []AgentSpec        `json:"agents"`
}

// AgentSpec is an agent inside a Template.
type AgentSpec struct {
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Tier         string              `json:"tier"`
	Role         string              `json:"role,omitempty"`
	Description  string              `json:"description,omitempty"`
	ReportsTo    []string            `json:"reports_to,omitempty"`
	ModelTier    string              `json:"model_tier"`
	LLMModel     string              `json:"llm_model,omitempty"`
	ProviderID   string              `json:"provider_id,omitempty"` // this office's connection id
	Provider     string              `json:"provider,omitempty"`    // connection name, used by export/import between offices
	Instructions string              `json:"instructions,omitempty"`
	Permissions  storage.Permissions `json:"permissions"`
}

// Builtins returns the embedded templates in display order.
func Builtins() ([]Template, error) {
	order := map[string]int{"solo": 0, "team": 1, "council": 2}
	entries, err := fs.ReadDir(models.FS, ".")
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, e := range entries {
		raw, err := fs.ReadFile(models.FS, e.Name())
		if err != nil {
			return nil, err
		}
		var t Template
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("orgmodel: %s: %w", e.Name(), err)
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Key] < order[out[j].Key] })
	return out, nil
}

func (s AgentSpec) toAgent(orgID string, sort int) storage.Agent {
	return storage.Agent{
		OrgModelID: orgID, Key: s.Key, Name: s.Name, Tier: s.Tier, Role: s.Role, Description: s.Description,
		ReportsTo: s.ReportsTo, ModelTier: s.ModelTier, LLMModel: s.LLMModel, Instructions: s.Instructions,
		Permissions: s.Permissions, Sort: sort, ProviderID: s.ProviderID,
	}
}

func specFromAgent(a storage.Agent) AgentSpec {
	return AgentSpec{
		Key: a.Key, Name: a.Name, Tier: a.Tier, Role: a.Role, Description: a.Description, ReportsTo: a.ReportsTo,
		ModelTier: a.ModelTier, LLMModel: a.LLMModel, Instructions: a.Instructions, Permissions: a.Permissions,
		ProviderID: a.ProviderID,
	}
}

// Export turns a stored model and its agents into a Template.
func Export(m storage.OrgModel, agents []storage.Agent) Template {
	t := Template{Key: m.Key, Name: m.Name, Kind: m.Kind, Description: m.Description, Governance: m.Governance}
	for _, a := range agents {
		t.Agents = append(t.Agents, specFromAgent(a))
	}
	return t
}
