package chat

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actions"
)

// A chat's proposals (actions and diffs) are decided in the order its agents
// made them: approving one while an earlier one still waits would run the
// steps out of order (a "git add" before the edit it stages, a diff from
// before the agent changed course). Rejecting or skipping is never held back.
// Its agent goes on only once nothing of the chat waits any more.

// OrderError: an earlier proposal of the chat still waits for a person.
type OrderError struct{ Earlier string }

func (e *OrderError) Error() string {
	return fmt.Sprintf("còn đề xuất tạo trước chưa xử lý: %s. Duyệt hoặc từ chối nó trước (đề xuất chạy theo thứ tự agent đưa ra)", e.Earlier)
}

type waitingCard struct {
	id, label string
	at        time.Time
}

// waiting lists a chat's proposals waiting for a person, oldest first.
func (e *Engine) waiting(ctx context.Context, conversationID string) ([]waitingCard, error) {
	acts, err := e.store.Actions().List(ctx, conversationID, "", "")
	if err != nil {
		return nil, err
	}
	var out []waitingCard
	for _, a := range acts {
		if a.Status == "pending" {
			out = append(out, waitingCard{a.ID, cmp.Or(actions.Kinds[a.Kind], a.Kind) + ": " + truncate(a.Target, 80), a.CreatedAt})
		}
	}
	ps, err := e.store.Chat().PatchRefs(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if p.Status != "pending" {
			continue
		}
		out = append(out, waitingCard{p.ID, fmt.Sprintf("diff %d file", len(p.Files)), p.CreatedAt})
	}
	slices.SortStableFunc(out, func(a, b waitingCard) int {
		return cmp.Or(a.at.Compare(b.at), cmp.Compare(a.id, b.id))
	})
	return out, nil
}

// CheckOrder: approving the proposal id of a chat is refused (OrderError)
// while one made before it still waits.
func (e *Engine) CheckOrder(ctx context.Context, conversationID, id string) error {
	if conversationID == "" {
		return nil
	}
	list, err := e.waiting(ctx, conversationID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(c waitingCard) bool { return c.id == id })
	if i > 0 {
		return &OrderError{Earlier: list[0].label}
	}
	return nil
}

// Waiting: something of the chat still waits for a person.
func (e *Engine) Waiting(ctx context.Context, conversationID string) bool {
	list, err := e.waiting(ctx, conversationID)
	return err == nil && len(list) > 0
}

// numberCards numbers a chat's proposals in the order they were made, as the
// chat shows them: answer by answer, an answer's actions before its diff.
func numberCards(msgs []MessageDTO) {
	n := 0
	for _, m := range msgs {
		for i := range m.Actions {
			n++
			m.Actions[i].Seq = n
		}
		for i := range m.Patches {
			n++
			m.Patches[i].Seq = n
		}
	}
}

// cardsBefore: how many proposals of a chat belong to answers other than
// msgID (a new answer's are numbered after them).
func (e *Engine) cardsBefore(ctx context.Context, conversationID, msgID string) int {
	n := 0
	if acts, err := e.store.Actions().List(ctx, conversationID, "", ""); err == nil {
		for _, a := range acts {
			if a.MessageID != "" && a.MessageID != msgID {
				n++
			}
		}
	}
	if ps, err := e.store.Chat().PatchRefs(ctx, conversationID); err == nil {
		for _, p := range ps {
			if p.MessageID != "" && p.MessageID != msgID {
				n++
			}
		}
	}
	return n
}
