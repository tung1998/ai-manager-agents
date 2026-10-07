// Package packs embeds the starter packs (ADR-099): the agents a new project
// starts with and the workflows installed for it. A pack is only used when a
// project is set up; once copied, the project's agents are its own.
package packs

import "embed"

// FS holds one JSON file per pack.
//
//go:embed *.json
var FS embed.FS
