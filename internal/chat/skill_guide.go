package chat

import "bitbucket.org/senprints/agent-office/internal/prompts"

// skillGuide tells the agent of a skill-writing chat how to fill the editor
// next to it: a fenced "skill" block the dashboard merges into the draft;
// nothing is saved until the person saves.
var skillGuide = "\n" + prompts.Text("guide/skill") + "\n"

// skillHandoff: a chat that cannot write .claude/skills (read-only, or no
// guard hook) drafts the skill in its answer; a person opens it in the editor.
var skillHandoff = "\n\n" + prompts.Text("guide/skill-handoff")
