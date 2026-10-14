package channels

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// editorNoStatus can edit/delete but its Send always fails, as if the
// earlier status message in step() never made it out: w.status stays "".
type editorNoStatus struct {
	sent []string
}

func (e *editorNoStatus) Run(context.Context, func(string), func(Incoming)) error { return nil }
func (e *editorNoStatus) Send(_ context.Context, _, text string) ([]string, error) {
	e.sent = append(e.sent, text)
	return nil, errors.New("fake: send failure")
}
func (e *editorNoStatus) SetCommands(context.Context, []Command)             {}
func (e *editorNoStatus) Typing(context.Context, string)                     {}
func (e *editorNoStatus) Edit(context.Context, string, string, string) error { return nil }
func (e *editorNoStatus) Delete(context.Context, string, string) error       { return nil }

// settle must not silently drop the accumulated step lines when the adapter
// is an Editor but no status message was ever sent (w.status == "").
func TestSettleSendsStepsWhenEditorHasNoStatus(t *testing.T) {
	ad := &editorNoStatus{}
	w := waiter{chat: "c1", verbose: true, lines: []string{"• bước 1", "• bước 2"}}
	m := &Manager{}
	m.settle(context.Background(), ad, w)

	if len(ad.sent) != 1 {
		t.Fatalf("settle() sent = %v, want 1 message via ad.Send", ad.sent)
	}
	want := stepsText(w.lines, true)
	if ad.sent[0] != want {
		t.Fatalf("settle() sent %q, want %q", ad.sent[0], want)
	}
}

// When a step's Send is still in flight (w.status == "" but w.sending == true),
// settle must not send its own message: step()'s in-flight Send will finish
// the message itself once it returns (threads.go:384-386).
func TestSettleSkipsSendWhileStepSendInFlight(t *testing.T) {
	ad := &editorNoStatus{}
	w := waiter{chat: "c1", verbose: true, lines: []string{"• bước 1"}, sending: true}
	m := &Manager{}
	m.settle(context.Background(), ad, w)

	if len(ad.sent) != 0 {
		t.Fatalf("settle() sent = %v, want no message while a step Send is in flight", ad.sent)
	}
}

// blockingEditor holds each Send until release says how it ends, so a test
// can answer the job while step()'s status Send is in flight.
type blockingEditor struct {
	mu      sync.Mutex
	started chan struct{}
	release chan error
	sent    []string
	edits   []string
}

func (b *blockingEditor) Run(context.Context, func(string), func(Incoming)) error { return nil }
func (b *blockingEditor) Send(_ context.Context, _, text string) ([]string, error) {
	b.mu.Lock()
	b.sent = append(b.sent, text)
	first := len(b.sent) == 1
	b.mu.Unlock()
	if !first { // only the status Send blocks; a fallback goes through
		return []string{"m2"}, nil
	}
	b.started <- struct{}{}
	if err := <-b.release; err != nil {
		return nil, err
	}
	return []string{"m1"}, nil
}
func (b *blockingEditor) SetCommands(context.Context, []Command) {}
func (b *blockingEditor) Typing(context.Context, string)         {}
func (b *blockingEditor) Edit(_ context.Context, _, _, text string) error {
	b.mu.Lock()
	b.edits = append(b.edits, text)
	b.mu.Unlock()
	return nil
}
func (b *blockingEditor) Delete(context.Context, string, string) error { return nil }

// The job is answered while step()'s status Send is in flight: settle leaves
// the steps to step(), which must show them exactly once whether that Send
// fails (a fresh message) or lands (one edit to the finished list).
func TestStepFinishesStepsAnsweredDuringSend(t *testing.T) {
	for _, fail := range []bool{true, false} {
		ad := &blockingEditor{started: make(chan struct{}), release: make(chan error)}
		m := &Manager{waiting: map[string]waiter{}}
		m.waiting["j"] = waiter{chat: "c1", verbose: true}
		done := make(chan struct{})
		go func() {
			m.mu.Lock()
			m.step(context.Background(), "j", ad, m.waiting["j"], "bước 1")
			close(done)
		}()
		<-ad.started
		m.mu.Lock() // Reply: the waiter goes, then settle
		w := m.waiting["j"]
		delete(m.waiting, "j")
		m.mu.Unlock()
		m.settle(context.Background(), ad, w)
		var err error
		if fail {
			err = errors.New("fake: rate limited")
		}
		ad.release <- err
		<-done
		want := stepsText([]string{"• bước 1"}, true)
		if fail {
			if len(ad.sent) != 2 || ad.sent[1] != want || len(ad.edits) != 0 {
				t.Fatalf("failed Send: sent=%q edits=%q, want the steps sent once more", ad.sent, ad.edits)
			}
		} else if len(ad.sent) != 1 || len(ad.edits) != 1 || ad.edits[0] != want {
			t.Fatalf("landed Send: sent=%q edits=%q, want one edit to the finished list", ad.sent, ad.edits)
		}
	}
}
