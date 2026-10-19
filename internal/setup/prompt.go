package setup

import (
	"strings"

	"bitbucket.org/senprints/agent-office/internal/prompts"
)

var (
	systemPrompt = prompts.Text("setup/system")
	// languageNote precedes the language rule: which fields the person reads.
	languageNote = "\n\n" + prompts.Text("setup/language") + "\n"
	outputSchema = prompts.Text("setup/schema")
)

func userPrompt(name, projectText, goal string, agents []Tool, catalog []string, box Toolbox) string {
	list := func(tools []Tool) string {
		if len(tools) == 0 {
			return ""
		}
		var b strings.Builder
		for _, t := range tools {
			b.WriteString("- " + t.Name)
			if t.Description != "" {
				b.WriteString(": " + strings.Join(strings.Fields(t.Description), " "))
			}
			if t.Needs != "" {
				b.WriteString(" [needs " + t.Needs + "]")
			}
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n")
	}
	return prompts.Render("setup/project", map[string]any{"Name": name, "Text": strings.TrimSpace(projectText),
		"Goal": strings.TrimSpace(goal), "Schema": outputSchema, "Commands": strings.Join(catalog, "\n"),
		"Current": list(agents), "Agents": list(box.Agents), "Workflows": list(box.Workflows),
		"Automations": list(box.Automations), "Skills": list(box.Skills), "MCP": list(box.MCP)}) + "\n"
}
