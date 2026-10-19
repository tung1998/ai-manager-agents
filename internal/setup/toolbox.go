package setup

import (
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/perm"
)

// Tool is one thing of a library the setup may pick for a project: a
// library agent (Name is its id), a workflow or automation template (its
// key), a skill, an MCP server of the catalog or library.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Needs says what a person must still do before an MCP works: "key"
	// (a required input such as a token), "login" (OAuth on first use), "".
	Needs string `json:"needs,omitempty"`
}

// Toolbox is what the setup may pick from.
type Toolbox struct {
	Agents      []Tool `json:"agents"`
	Workflows   []Tool `json:"workflows"`
	Automations []Tool `json:"automations"`
	Skills      []Tool `json:"skills"`
	MCP         []Tool `json:"mcp"`
}

// Pick is one workflow, automation, skill or MCP the model chose, with why.
type Pick struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
	Needs  string `json:"needs,omitempty"` // filled from the toolbox, not the model
}

// maxPicks caps each list the model returns.
const maxPicks = 6

// cleanPicks keeps the picks found in the toolbox (deduped, at most
// maxPicks), filling Needs from it; the rest become warnings.
func cleanPicks(kind string, picks []Pick, tools []Tool) ([]Pick, []string) {
	out := []Pick{}
	var warnings []string
	seen := map[string]bool{}
	for _, p := range picks {
		name := strings.TrimSpace(p.Name)
		if name == "" || seen[name] {
			continue
		}
		var tool *Tool
		for i := range tools {
			if tools[i].Name == name {
				tool = &tools[i]
				break
			}
		}
		if tool == nil {
			warnings = append(warnings, fmt.Sprintf("%s %q không có trong kho, đã bỏ", kind, name))
			continue
		}
		if len(out) == maxPicks {
			break
		}
		seen[name] = true
		out = append(out, Pick{Name: name, Reason: strings.TrimSpace(p.Reason), Needs: tool.Needs})
	}
	return out, warnings
}

// cleanQuickChecks keeps the proposed quick checks only when they parse.
func cleanQuickChecks(s string) (string, []string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if _, err := perm.ParseQuickChecks(s); err != nil {
		return "", []string{"kiểm tra nhanh đề xuất không hợp lệ, đã bỏ: " + err.Error()}
	}
	return s, nil
}
