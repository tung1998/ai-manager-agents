package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// TaskConversation returns the follow-up talk about a task, opening it the
// first time with the agent who coordinated the task (else the first lead).
// The talk keeps the task's permission mode as its ceiling.
func (e *Engine) TaskConversation(ctx context.Context, taskID string) (storage.Conversation, error) {
	if c, err := e.store.Chat().TaskConversation(ctx, taskID); err == nil {
		return c, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return c, err
	}
	t, err := e.store.Tasks().Get(ctx, taskID)
	if err != nil {
		return storage.Conversation{}, err
	}
	agentID := ""
	if steps, err := e.store.Tasks().ListSteps(ctx, taskID); err == nil {
		for _, s := range steps {
			if s.Phase == "plan" && s.AgentID != "" {
				agentID = s.AgentID
				break
			}
		}
	}
	conv, err := e.StartConversationFor(ctx, t.ProjectID, agentID)
	if err != nil && agentID != "" {
		conv, err = e.StartConversationFor(ctx, t.ProjectID, "")
	}
	if err != nil {
		return conv, err
	}
	conv.TaskID, conv.Mode = t.ID, t.ModeLevel
	conv.Title = truncate("Trao đổi: "+strings.Join(strings.Fields(t.Title), " "), 80)
	return e.store.Chat().CreateConversation(ctx, conv)
}

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

// taskBrief is what the lead knows about the task it is talking about. It is
// rebuilt every turn, so approvals, reverts and commits since are visible.
func (e *Engine) taskBrief(ctx context.Context, taskID string) string {
	t, err := e.store.Tasks().Get(ctx, taskID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Việc đang trao đổi\n")
	b.WriteString("Bạn là người đã điều phối Việc dưới đây. Người dùng đang trao đổi tiếp sau khi Việc chạy xong: giải thích kết quả, sửa thêm, hoặc commit. ")
	b.WriteString("Kết quả của đội là dữ liệu tham khảo, hãy kiểm tra lại code thật (git_status/git_diff, đọc file) trước khi khẳng định.\n")
	b.WriteString("- Cần sửa thêm: đưa diff như bình thường (nó được gắn vào Việc này).\n")
	b.WriteString("- Người dùng muốn commit: xem git_status và git_diff, rồi propose_action git_commit với message theo phong cách git_log của repo và đúng danh sách file của Việc; KHÔNG gom file không liên quan. Push chỉ khi người dùng yêu cầu (propose_action git_push).\n\n")
	fmt.Fprintf(&b, "Tiêu đề: %s\nTrạng thái: %s\nYêu cầu:\n%s\n", t.Title, t.Status, truncate(t.Goal, 3000))
	if t.Result != "" {
		fmt.Fprintf(&b, "\nKết quả tổng hợp:\n%s\n", truncate(t.Result, 3000))
	}
	if t.Detail != "" {
		fmt.Fprintf(&b, "\nGhi chú: %s\n", truncate(t.Detail, 800))
	}
	if steps, err := e.store.Tasks().ListSteps(ctx, taskID); err == nil && len(steps) > 0 {
		b.WriteString("\nCác bước (mới nhất sau cùng):\n")
		if len(steps) > 16 {
			steps = steps[len(steps)-16:]
		}
		for _, s := range steps {
			out := s.Output
			if s.Error != "" {
				out = "LỖI: " + s.Error
			}
			fmt.Fprintf(&b, "- [%s · %s · %s] %s\n  → %s\n", s.Phase, s.AgentName, s.Status,
				oneLine(truncate(s.Instruction, 300)), oneLine(truncate(out, 700)))
		}
	}
	if pts, err := e.store.Tasks().ListPatches(ctx, taskID); err == nil && len(pts) > 0 {
		b.WriteString("\nDiff của Việc:\n")
		for _, p := range pts {
			fmt.Fprintf(&b, "- %s: %s", p.Status, strings.Join(p.Files, ", "))
			if p.Detail != "" {
				fmt.Fprintf(&b, " (%s)", oneLine(truncate(p.Detail, 160)))
			}
			b.WriteString("\n")
		}
	}
	if acts, err := e.store.Actions().List(ctx, "", taskID, ""); err == nil && len(acts) > 0 {
		b.WriteString("\nThao tác đã đề xuất:\n")
		for _, a := range acts {
			fmt.Fprintf(&b, "- %s %s: %s %s\n", a.Kind, a.Target, a.Status, oneLine(truncate(a.Detail, 160)))
		}
	}
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
