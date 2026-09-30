package events_test

import (
	"slices"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/events"
)

// Writes close together reach every subscriber once, as one set of tables;
// tables nobody follows (sessions…) are not told.
func TestBus(t *testing.T) {
	b := events.New(30 * time.Millisecond)
	a, stopA := b.Subscribe()
	c, stopC := b.Subscribe()
	defer stopC()
	b.Wrote("messages")
	b.Wrote("jobs")
	b.Wrote("messages")
	b.Wrote("sessions")
	for _, ch := range []<-chan []string{a, c} {
		select {
		case got := <-ch:
			if len(got) != 2 || !slices.Contains(got, "messages") || !slices.Contains(got, "jobs") {
				t.Fatalf("got %v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("nothing told")
		}
	}
	stopA()
	b.Wrote("sessions")
	select {
	case got := <-c:
		t.Fatalf("an unfollowed table was told: %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}
