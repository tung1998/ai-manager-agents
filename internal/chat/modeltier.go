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

type instructionsKey struct{}

// WithInstructions adds instructions to the system prompt of the runs of ctx:
// an automation's own, set by its admin (a bot's command, ADR-049). They sit
// where no one writing to the bot can reach, unlike a message.
func WithInstructions(ctx context.Context, s string) context.Context {
	if s == "" {
		return ctx
	}
	return context.WithValue(ctx, instructionsKey{}, s)
}

func instructionsOf(ctx context.Context) string {
	s, _ := ctx.Value(instructionsKey{}).(string)
	return s
}

type skillKey struct{}

// WithSkill lets a bot's conversation expand this one skill (a command made
// from it); a bot's conversation expands no other "/name" (review I1).
func WithSkill(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, skillKey{}, name)
}

func skillOf(ctx context.Context) string { s, _ := ctx.Value(skillKey{}).(string); return s }

type noToolsKey struct{}

// WithNoTools makes the runs of ctx tool-less (untrusted text drives them,
// e.g. a channel's scope filter).
func WithNoTools(ctx context.Context) context.Context {
	return context.WithValue(ctx, noToolsKey{}, true)
}

func noTools(ctx context.Context) bool { v, _ := ctx.Value(noToolsKey{}).(bool); return v }

type fullAccessKey struct{}

// WithFullAccess runs the turn with the machine (bypassPermissions, every
// tool): a bot's chat in administrator mode, for someone who may approve there.
func WithFullAccess(ctx context.Context) context.Context {
	return context.WithValue(ctx, fullAccessKey{}, true)
}

func fullAccess(ctx context.Context) bool { v, _ := ctx.Value(fullAccessKey{}).(bool); return v }

type extraDirsKey struct{}

// WithExtraDirs allows the runs of ctx to read additional directories (ADR-074).
func WithExtraDirs(ctx context.Context, dirs []string) context.Context {
	if len(dirs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, extraDirsKey{}, dirs)
}

func extraDirsOf(ctx context.Context) []string {
	dirs, _ := ctx.Value(extraDirsKey{}).([]string)
	return dirs
}
