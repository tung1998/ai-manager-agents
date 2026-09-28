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

func agentsOf(q []queued) []storage.Agent {
	out := make([]storage.Agent, 0, len(q))
	for _, x := range q {
		out = append(out, x.agent)
	}
	return out
}

// RunningTurn is an answer in progress in a chat.
type RunningTurn struct {
	TurnID     string `json:"turn_id"`
	AgentName  string `json:"agent_name"`
	Background bool   `json:"background"` // a hand-off the person does not wait for
}

// Running lists the answers in progress in a chat: the one the person waits
// for and the hand-offs working in the background.
func (e *Engine) Running(conversationID string) []RunningTurn {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []RunningTurn{}
	if t, ok := e.active[conversationID]; ok {
		out = append(out, RunningTurn{t.ID, t.agentName, false})
	}
	for k, t := range e.bg {
		if strings.HasPrefix(k, conversationID+"/") {
			out = append(out, RunningTurn{t.ID, t.agentName, true})
		}
	}
	return out
}

// turnSpec is a turn to start in a chat.
type turnSpec struct {
	agent      storage.Agent
	prompt     string
	title      string
	background bool
	delegator  string
	queue      []queued
	hops       int
	answered   int
	actor      string
	replace    *Turn // the finishing turn it takes the chat from ("" = the chat must be free)
}

// startTurn starts an agent's answer; nil when the chat (or, in the
// background, that agent) is busy.
func (e *Engine) startTurn(conv storage.Conversation, project storage.Repo, s turnSpec) *Turn {
	runCtx, cancel := context.WithTimeout(actor.With(context.Background(), s.actor), 20*time.Minute)
	t := &Turn{ID: fmt.Sprintf("%s-%d", conv.ID, time.Now().UnixNano()), ConversationID: conv.ID, wake: make(chan struct{}), cancel: cancel,
		queue: s.queue, hops: s.hops, answered: s.answered, actor: s.actor,
		agentID: s.agent.ID, agentName: s.agent.Name, background: s.background, delegator: s.delegator}
	e.mu.Lock()
	key := conv.ID + "/" + s.agent.ID
	if s.background {
		if _, working := e.bg[key]; working {
			e.mu.Unlock()
			cancel()
			return nil
		}
		e.bg[key] = t
	} else {
		if cur, busy := e.active[conv.ID]; busy && cur != s.replace {
			e.mu.Unlock()
			cancel()
			return nil
		}
		e.active[conv.ID] = t // held before the previous turn lets go
	}
	e.turns[t.ID] = t
	e.mu.Unlock()
	release := func() {
		cancel()
		e.finish(t)
	}
	history, err := e.store.Chat().ListMessages(runCtx, conv.ID)
	if err != nil {
		release()
		return nil
	}
	job, err := e.beginJob(runCtx, conv, s.agent.ID, truncate(s.title, 80))
	if err != nil {
		release()
		return nil
	}
	t.JobID = job.ID
	runCtx = usage.WithJob(runCtx, job.ID)
	go e.run(runCtx, t, conv, project, s.agent, history, s.prompt, nil)
	return t
}

// nextTurn runs what follows an answer (ADR-044) and returns the turn the
// person keeps watching ("" = none):
//   - @tags in the reply hand off in the background, like subagents, within
//     maxHops per message of the person;
//   - a background agent that is done: the one who asked reports back, when
//     the chat is free (otherwise it sees the result on its next turn);
//   - the next agent the person tagged answers.
func (e *Engine) nextTurn(ctx context.Context, prev *Turn, conv storage.Conversation, project storage.Repo, agent storage.Agent, reply string) string {
	if ctx.Err() != nil {
		return ""
	}
	hops := prev.hops
	agents, _ := e.Agents(ctx, conv.ProjectID)
	if reply != "" && conv.TaskID == "" && conv.Purpose == "" {
		cut := false
		for _, a := range Mentions(reply, agents) {
			if a.ID == agent.ID || slices.ContainsFunc(prev.queue, func(q queued) bool { return q.agent.ID == a.ID }) {
				continue
			}
			if prev.background && a.ID == prev.delegator {
				continue // tagging the one who asked is the report
			}
			if hops >= maxHops {
				cut = true
				break
			}
			hops++
			e.startTurn(conv, project, turnSpec{agent: a, background: true, delegator: agent.ID, hops: hops, actor: prev.actor,
				title:  agent.Name + " → " + a.Name,
				prompt: fmt.Sprintf("%s giao việc cho bạn trong cuộc chat (xem tin gần nhất của %s). Làm phần được giao rồi báo kết quả ngắn gọn.", agent.Name, agent.Name)})
		}
		if cut {
			_, _ = e.store.Chat().AddMessage(context.Background(), storage.Message{ConversationID: conv.ID, Role: "error",
				Content: fmt.Sprintf("Đã dừng chuyển tiếp: quá %d lượt agent tag nhau cho một tin nhắn. Hãy tag lại nếu cần.", maxHops)})
		}
	}
	if prev.background {
		if i := slices.IndexFunc(agents, func(a storage.Agent) bool { return a.ID == prev.delegator }); i >= 0 {
			e.startTurn(conv, project, turnSpec{agent: agents[i], hops: hops, actor: prev.actor, title: agent.Name + " → " + agents[i].Name,
				prompt: fmt.Sprintf("%s đã làm xong phần việc bạn giao (xem tin gần nhất). Báo lại kết quả cho người dùng ngắn gọn và làm tiếp nếu cần.", agent.Name)})
		}
		return ""
	}
	if len(prev.queue) == 0 {
		return ""
	}
	q := prev.queue[0]
	next := e.startTurn(conv, project, turnSpec{agent: q.agent, queue: prev.queue[1:], hops: hops, answered: prev.answered + 1, actor: prev.actor, replace: prev,
		title:  q.from + " → " + q.agent.Name,
		prompt: fmt.Sprintf("%s vừa tag bạn trong cuộc chat. Trả lời phần dành cho bạn trong tin gần nhất.", q.from)})
	if next == nil {
		return ""
	}
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

// chatTree: the chat's first agent keeps the chat's worktree; agents that
// joined later get their own, so hand-offs running at once never collide.
func (e *Engine) chatTree(ctx context.Context, conv storage.Conversation, agent storage.Agent) string {
	if list, err := e.store.Chat().Members(ctx, conv.ID); err == nil && len(list) > 0 && list[0].AgentID != agent.ID {
		return ChatAgentTree(conv.ID, agent.ID)
	}
	return ChatTree(conv.ID)
}
