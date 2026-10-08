package main

import (
	"context"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/memory"
	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// compactNotes rewrites an agent's notes shorter with a fast model, no tools.
func compactNotes(st storage.Store, engine *chat.Engine) memory.Compactor {
	return func(ctx context.Context, projectID, agentID string, items []storage.Memory) ([]string, error) {
		project, err := st.Repos().Get(ctx, projectID)
		if err != nil {
			return nil, err
		}
		agent, err := st.Agents().Get(ctx, agentID)
		if err != nil {
			return nil, err
		}
		notes := make([]string, 0, len(items))
		for _, m := range items {
			notes = append(notes, strings.ReplaceAll(m.Text, "\n", " "))
		}
		prompt := prompts.Render("memory/compact", map[string]any{"Notes": notes}) + "\n"
		res, err := engine.Invoke(chat.WithNoTools(chat.WithModelTier(ctx, storage.TierFast)), project, agent, prompt, nil, "memory_compact", nil)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, l := range strings.Split(res.Text, "\n") {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, "- ") {
				out = append(out, strings.TrimPrefix(l, "- "))
			}
		}
		return out, nil
	}
}
