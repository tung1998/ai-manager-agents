package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// Group chat limits (ADR-044): hand-offs agents make per message of the
// person, and replies in all.
const (
	maxHops    = 2
	maxAnswers = 4
)

// queued is an agent still to answer, and who tagged it.
type queued struct {
	agent storage.Agent
	from  string
}

// nextTurn starts the next tagged agent's answer, if any, and returns its
// turn id ("" = none). reply is what agent just said: its @tags hand off,
// within the limits; a stopped turn drops the queue.
func (e *Engine) nextTurn(ctx context.Context, prev *Turn, conv storage.Conversation, project storage.Repo, agent storage.Agent, reply string) string {
	if ctx.Err() != nil {
		return ""
	}
	queue, hops := prev.queue, prev.hops
	if reply != "" && conv.TaskID == "" && conv.Purpose == "" {
		agents, _ := e.Agents(ctx, conv.ProjectID)
		cut := false
		for _, a := range Mentions(reply, agents) {
			if a.ID == agent.ID || slices.ContainsFunc(queue, func(q queued) bool { return q.agent.ID == a.ID }) {
				continue
			}
			if hops >= maxHops || prev.answered+1+len(queue) >= maxAnswers {
				cut = true
				break
			}
			queue = append(queue, queued{agent: a, from: agent.Name})
			hops++
		}
		if cut {
			_, _ = e.store.Chat().AddMessage(context.Background(), storage.Message{ConversationID: conv.ID, Role: "error",
				Content: fmt.Sprintf("Đã dừng chuyển tiếp: quá %d lượt agent tag nhau cho một tin nhắn. Hãy tag lại nếu cần.", maxHops)})
		}
	}
	if len(queue) == 0 {
		return ""
	}
	q := queue[0]
	runCtx, cancel := context.WithTimeout(actor.With(context.Background(), prev.actor), 20*time.Minute)
	next := &Turn{ID: fmt.Sprintf("%s-%d", conv.ID, time.Now().UnixNano()), ConversationID: conv.ID, wake: make(chan struct{}), cancel: cancel,
		queue: queue[1:], hops: hops, answered: prev.answered + 1, actor: prev.actor}
	history, err := e.store.Chat().ListMessages(runCtx, conv.ID)
	if err != nil {
		cancel()
		return ""
	}
	job, err := e.beginJob(runCtx, conv, q.agent.ID, truncate(q.from+" → "+q.agent.Name, 80))
	if err != nil {
		cancel()
		return ""
	}
	next.JobID = job.ID
	runCtx = usage.WithJob(runCtx, job.ID)
	e.mu.Lock()
	e.active[conv.ID], e.turns[next.ID] = next, next // held before the previous turn lets go
	e.mu.Unlock()
	prompt := fmt.Sprintf("%s vừa tag bạn trong cuộc chat. Trả lời phần dành cho bạn trong tin gần nhất.", q.from)
	go e.run(runCtx, next, conv, project, q.agent, history, prompt, nil)
	return next.ID
}

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

// groupBrief tells an agent who is in the chat and when to tag another.
func (e *Engine) groupBrief(ctx context.Context, conv storage.Conversation, self storage.Agent) string {
	agents, err := e.Agents(ctx, conv.ProjectID)
	if err != nil || len(agents) < 2 {
		return ""
	}
	in := map[string]bool{}
	if list, err := e.store.Chat().Members(ctx, conv.ID); err == nil {
		for _, m := range list {
			in[m.AgentID] = true
		}
	}
	var b strings.Builder
	b.WriteString("\n## Cuộc chat có thể có nhiều agent\nNgười dùng tag @Tên để kéo agent vào. Các agent của project (quyền):\n") // i18n-ignore
	for _, a := range agents {
		mark := ""
		if a.ID == self.ID {
			mark = " (bạn)"
		} else if in[a.ID] {
			mark = " (đang trong cuộc chat)"
		}
		fmt.Fprintf(&b, "- @%s: %s, quyền %s%s\n", a.Name, firstNonEmpty(a.Role, string(a.Tier)), perm.Label(perm.Agent(a)), mark)
	}
	b.WriteString("Chỉ tag @agent khác khi thật sự cần (việc cần quyền hay chuyên môn bạn không có) và ghi rõ cần họ làm gì; việc tự làm được thì tự làm. Không tag chỉ để báo tin.\n")
	return b.String()
}
