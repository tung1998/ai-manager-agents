// Package library embeds the agent library (ADR-153, ADR-154): ready-made
// agents a person adds to a project, or reads as a model when writing their
// own. One Markdown file per agent: office frontmatter, then the instructions.
// office/ is written for office; agency/ is imported from agency-agents by
// scripts/sync-agency-library.mjs. Never edit agency/ by hand.
package library

import "embed"

// FS holds office/*.md and agency/*.md.
//
//go:embed office/*.md agency/*.md
var FS embed.FS
