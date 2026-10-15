package chat

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A chat's proposals (actions and diffs) are decided in the order its agents
// made them: approving one while an earlier one still waits would run the
// steps out of order (a "git add" before the edit it stages, a diff from
// before the agent changed course). Rejecting or skipping is never held back.
// Its agent goes on only once nothing of the chat waits any more.

// OrderError: an earlier proposal of the chat still waits for a person (or
// still runs).
type OrderError struct {
	Earlier string // what it is
	ID      string
	Seq     int // its number in the chat (0: not shown yet)
}

func (e *OrderError) Error() string {
	what := e.Earlier
	if e.Seq > 0 {
		what = fmt.Sprintf("#%d %s", e.Seq, e.Earlier)
	}
	return fmt.Sprintf("còn đề xuất tạo trước chưa xử lý hoặc đang chạy: %s. Duyệt hoặc từ chối nó trước (đề xuất chạy theo thứ tự agent đưa ra)", what)
}

type waitingCard struct {
	id, label string
	seq       int
	at        time.Time
}

// cardSeqs numbers a chat's proposals as numberCards does for its history,
// from the store: answer by answer, an answer's actions before its diff.
func (e *Engine) cardSeqs(ctx context.Context, conversationID string, acts []storage.Action, ps []storage.Patch) (map[string]int, error) {
	msgs, err := e.store.Chat().ListMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	byMsg := map[string][]string{}
	for _, a := range acts {
		byMsg[a.MessageID] = append(byMsg[a.MessageID], a.ID)
	}
	for _, p := range ps {
		byMsg[p.MessageID] = append(byMsg[p.MessageID], p.ID)
	}
	seq := map[string]int{}
	for _, m := range msgs {
		for _, id := range byMsg[m.ID] {
			seq[id] = len(seq) + 1
		}
	}
	return seq, nil
}

// waiting lists a chat's proposals waiting for a person in the order they
// are shown (and decided): by number, then those of an answer not over yet.
func (e *Engine) waiting(ctx context.Context, conversationID string) ([]waitingCard, error) {
	acts, err := e.store.Actions().List(ctx, conversationID, "", "")
	if err != nil {
		return nil, err
	}
	ps, err := e.store.Chat().PatchRefs(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	seq, err := e.cardSeqs(ctx, conversationID, acts, ps)
	if err != nil {
		return nil, err
	}
	var out []waitingCard
	for _, a := range acts {
		if a.Status == "pending" {
			out = append(out, waitingCard{a.ID, cmp.Or(actions.Kinds[a.Kind], a.Kind) + ": " + truncate(a.Target, 80), seq[a.ID], a.CreatedAt})
		}
	}
	for _, p := range ps {
		if p.Status == "pending" {
			out = append(out, waitingCard{p.ID, fmt.Sprintf("diff %d file", len(p.Files)), seq[p.ID], p.CreatedAt})
		}
	}
	unseen := func(n int) int { // not numbered yet: after every numbered one
		if n == 0 {
			return int(^uint(0) >> 1)
		}
		return n
	}
	slices.SortStableFunc(out, func(a, b waitingCard) int {
		return cmp.Or(cmp.Compare(unseen(a.seq), unseen(b.seq)), a.at.Compare(b.at), cmp.Compare(a.id, b.id))
	})
	return out, nil
}

// CheckOrder: approving the proposal id of a chat is refused (OrderError)
// while one before it still waits.
func (e *Engine) CheckOrder(ctx context.Context, conversationID, id string) error {
	if conversationID == "" {
		return nil
	}
	list, err := e.waiting(ctx, conversationID)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(list, func(c waitingCard) bool { return c.id == id }); i > 0 {
		return &OrderError{Earlier: list[0].label, ID: list[0].id, Seq: list[0].seq}
	}
	return nil
}

// Waiting: something of the chat still waits for a person; when that can't
// be told, it is taken as waiting (the next decision looks again).
func (e *Engine) Waiting(ctx context.Context, conversationID string) bool {
	list, err := e.waiting(ctx, conversationID)
	return err != nil || len(list) > 0
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

// numberPending gives a chat's waiting proposals (shown elsewhere, as a
// linked chat's) the numbers they have in that chat.
func (e *Engine) numberPending(ctx context.Context, conversationID string, acts []ActionDTO, ps []PatchDTO) {
	list, err := e.waiting(ctx, conversationID)
	if err != nil {
		return
	}
	seq := map[string]int{}
	for _, c := range list {
		seq[c.id] = c.seq
	}
	for i := range acts {
		acts[i].Seq = seq[acts[i].ID]
	}
	for i := range ps {
		ps[i].Seq = seq[ps[i].ID]
	}
}

// WaitingOrder lists the ids of a chat's waiting proposals in the order they
// must be decided.
func (e *Engine) WaitingOrder(ctx context.Context, conversationID string) []string {
	list, _ := e.waiting(ctx, conversationID)
	ids := make([]string, len(list))
	for i, c := range list {
		ids[i] = c.id
	}
	return ids
}
