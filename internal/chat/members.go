package chat

import (
	"context"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// member is the agent's place in a chat (ADR-044): its own session there and
// the last message it has seen; a new one when it has not answered yet.
func (e *Engine) member(ctx context.Context, conv storage.Conversation, agent storage.Agent) storage.ChatMember {
	if list, err := e.store.Chat().Members(ctx, conv.ID); err == nil {
		for _, m := range list {
			if m.AgentID == agent.ID {
				m.AgentName = agent.Name
				return m
			}
		}
	}
	return storage.ChatMember{ConversationID: conv.ID, AgentID: agent.ID, AgentName: agent.Name}
}

// newSince is what was said after the message an agent last saw, by others
// (the person or other agents), labelled with who said it; "" = nothing.
func newSince(history []storage.Message, lastID, self string) string {
	start := -1
	for i, m := range history {
		if m.ID == lastID {
			start = i
		}
	}
	if start < 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range history[start+1:] {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		who := "Người dùng"
		if m.Role == "assistant" {
			if m.Author == self {
				continue
			}
			who = m.Author
		}
		fmt.Fprintf(&b, "\n[%s]\n%s\n", who, truncate(m.Content, 6000))
	}
	if b.Len() == 0 {
		return ""
	}
	return "Trong cuộc chat, từ lượt trước của bạn đã có thêm:\n" + b.String() + "\n---\n"
}
