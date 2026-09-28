package chat

import (
	"context"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

type modelTierKey struct{}

// WithModelTier makes the runs of ctx use this model tier (strong | balanced |
// fast) instead of the agent's: an automation's cheaper choice.
func WithModelTier(ctx context.Context, tier string) context.Context {
	if tier == "" {
		return ctx
	}
	return context.WithValue(ctx, modelTierKey{}, tier)
}

// ModelTierFrom is the tier ctx asks for ("" = the agent's).
func ModelTierFrom(ctx context.Context) string {
	s, _ := ctx.Value(modelTierKey{}).(string)
	return s
}

// withTier is the agent as it runs under ctx's model tier.
func withTier(ctx context.Context, a storage.Agent) storage.Agent {
	switch tier := ModelTierFrom(ctx); tier {
	case storage.TierStrong, storage.TierBalanced, storage.TierFast:
		a.ModelTier, a.LLMModel = tier, ""
	}
	return a
}

type noToolsKey struct{}

// WithNoTools makes the runs of ctx tool-less (untrusted text drives them,
// e.g. a channel's scope filter).
func WithNoTools(ctx context.Context) context.Context {
	return context.WithValue(ctx, noToolsKey{}, true)
}

func noTools(ctx context.Context) bool { v, _ := ctx.Value(noToolsKey{}).(bool); return v }
