package chat

import (
	"context"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// StartConversationFor picks the agent of a new conversation without storing it.
func (e *Engine) StartConversationFor(ctx context.Context, projectID, agentID string) (storage.Conversation, error) {
	agents, err := e.Agents(ctx, projectID)
	if err != nil {
		return storage.Conversation{}, err
	}
	for _, a := range agents {
		if (agentID == "" && a.Tier == storage.TierLead) || a.ID == agentID {
			return storage.Conversation{ProjectID: projectID, AgentID: a.ID, AgentName: a.Name, CreatedBy: actor.From(ctx)}, nil
		}
	}
	return storage.Conversation{}, ErrNoAgent
}

// StartConversationPurpose opens a thread for a purpose ("automation" = one
// that builds an automation, not listed with the project's chats).
func (e *Engine) StartConversationPurpose(ctx context.Context, projectID, agentID, purpose string) (storage.Conversation, error) {
	c, err := e.StartConversationFor(ctx, projectID, agentID)
	if err != nil {
		return c, err
	}
	c.Purpose = purpose
	return e.store.Chat().CreateConversation(ctx, c)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
