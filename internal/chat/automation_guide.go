package chat

import "bitbucket.org/senprints/agent-office/internal/prompts"

// automationGuide tells the agent of an automation-building chat how to fill
// the form next to it (ADR-042): a fenced "automation" block the dashboard
// merges into the draft; nothing is saved until the person saves.
var automationGuide = "\n" + prompts.Text("guide/automation") + "\n"
