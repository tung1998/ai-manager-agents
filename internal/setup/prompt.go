package setup

import (
	"encoding/json"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/prompts"
)

var (
	systemPrompt = prompts.Text("setup/system")
	// languageNote precedes the language rule: which fields the person reads.
	languageNote = "\n\n" + prompts.Text("setup/language") + "\n"
	outputSchema = prompts.Text("setup/schema")
)

func userPrompt(name, projectText, goal string, library []libraryEntry) string {
	lib, _ := json.MarshalIndent(library, "", "  ")
	text := projectText
	if strings.TrimSpace(projectText) == "" {
		text = ""
	}
	return prompts.Render("setup/project", map[string]any{"Packs": string(lib), "Name": name, "Text": text,
		"Goal": strings.TrimSpace(goal), "Schema": outputSchema}) + "\n"
}
