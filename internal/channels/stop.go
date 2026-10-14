package channels

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// stopEngine is the part of *chat.Engine that stopping a chat needs; a test
// can swap it for a fake that lands a racing Reply exactly inside the window
// a real goroutine race would only hit by chance.
type stopEngine interface {
	StopAll(conversationID string) int
	Active(conversationID string) (*chat.Turn, bool)
}

// stopper is what stop.go uses to stop/poll a conversation: the manager's
// real engine, unless a test substituted a fake (m.stopEngine).
func (m *Manager) stopper() stopEngine {
	if m.stopEngine != nil {
		return m.stopEngine
	}
	if m.engine == nil { // an untyped nil interface, not a non-nil one holding a nil *chat.Engine
		return nil
	}
	return m.engine
}

// stopChat stops what a chat's messages run (/stop, /push): the answers in
// progress and the messages still waiting; how many, and the conversations
// whose answers were stopped. What it stops answers nothing more.
func (m *Manager) stopChat(ctx context.Context, key string) (int, []string) {
	m.mu.Lock()
	var ids []string
	for id, w := range m.waiting {
		if w.key == key && !w.stopped {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	n := 0
	var convs []string
	for _, id := range ids {
		j, err := m.store.Jobs().Get(ctx, id)
		if err != nil {
			continue
		}
		if j.Status == "pending" { // not started: it never runs, so nothing answers it; Reply frees its place
			nj, err := m.store.Jobs().FinishIfPending(ctx, id, "cancelled", "cancelled", "dừng từ bot", time.Now().UTC())
			if err != nil {
				if !errors.Is(err, storage.ErrConflict) {
					continue
				}
				// claimed between Get and the write: it is now actually running,
				// fall through to the running case so it gets stopped, not
				// marked cancelled while it keeps executing.
				if cur, gerr := m.store.Jobs().Get(ctx, id); gerr == nil {
					j = cur
				}
			} else {
				m.setStopped(id, true)
				m.Reply(ctx, nj, "", nil, true)
				n++
				continue
			}
		}
		switch j.Status {
		case "running":
			var p trigger.ChannelPayload
			if json.Unmarshal([]byte(j.Payload), &p) != nil || p.ConversationID == "" || m.stopper() == nil {
				continue // a script: it runs to its end
			}
			if m.stopper().StopAll(p.ConversationID) == 0 {
				continue // already done by the time it was stopped: its answer still gets posted
			}
			// StopAll found something to cancel: between it returning and this
			// line, the job could in theory still finish and call Reply on its
			// own with a cut-short answer before setStopped below lands — that
			// narrow window is accepted so a finished answer is never dropped;
			// worst case a partial answer gets sent instead of being held back.
			m.setStopped(id, true) // stopped for real: its answer (cut short) is not posted
			convs = append(convs, p.ConversationID)
			n++
		}
	}
	return n, convs
}

// setStopped marks a waiting message stopped (or not, when nothing stopped).
func (m *Manager) setStopped(jobID string, v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.waiting[jobID]; ok {
		w.stopped = v
		m.waiting[jobID] = w
	}
}

// waitIdle waits (a little) for the stopped answers to end.
func (m *Manager) waitIdle(ctx context.Context, convs []string) {
	if m.stopper() == nil || len(convs) == 0 {
		return
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		busy := false
		for _, c := range convs {
			if _, ok := m.stopper().Active(c); ok {
				busy = true
			}
		}
		if !busy {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
