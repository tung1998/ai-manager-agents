package chat

import (
	"context"

	"bitbucket.org/senprints/agent-office/internal/features"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Prompt keywords: a line of a prompt template under [[if .On.<keyword>]]
// is there only when the project has it, so an agent is told about git,
// operations or build/test only where they exist (a docs project is not
// asked about commits or tests). A new pack adds its keyword and its lines.
// Every keyword is in the map (a template naming an unknown one fails: a
// typo shows in the tests).
const (
	KeyBuild = "build" // the project has build/test commands (perm.HasBuild)
)

// promptKeywords: the features the project has on (internal/features) and
// what office found in it.
func promptKeywords(ctx context.Context, st storage.Store, projectID string, policy perm.Policy) map[string]bool {
	on := keywords(features.For(ctx, st, projectID)...)
	on[KeyBuild] = policy.Build
	return on
}

// keywords: every prompt keyword, those given on.
func keywords(on ...string) map[string]bool {
	m := map[string]bool{KeyBuild: false}
	for _, f := range features.All() {
		m[f] = false
	}
	for _, k := range on {
		m[k] = true
	}
	return m
}
