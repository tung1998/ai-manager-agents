package chat

import "bitbucket.org/senprints/agent-office/internal/prompts"

// workflowGuide tells the agent of a workflow-writing chat how to fill the
// editor next to it: a fenced "workflow" block the dashboard takes as the
// whole file; nothing is saved until the person saves (guide/workflow.md: a
// template only to write the YAML's "[[a, b]]" as it is).
var workflowGuide = "\n\n" + prompts.Render("guide/workflow", nil)
