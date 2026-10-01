// Package events tells the dashboard what changed (ADR-072): every write to
// a table people look at (messages, jobs, automations, bots…) whoever made
// it (a bot's message, an agent, a run in the background), gathered for a
// moment so a burst is one notice.
package events

import (
	"sort"
	"sync"
	"time"
)

// Followed are the tables a dashboard page shows.
var Followed = map[string]bool{
	"conversations": true, "messages": true, "conversation_agents": true, "patches": true, "actions": true,
	"jobs": true, "automations": true, "channels": true, "repos": true, "org_models": true, "agents": true,
	"processes": true, "monitors": true, "agent_memories": true, "providers": true, "users": true,
	"runs": true, "org_revisions": true, "audit_log": true, "monitor_events": true, "settings": true, "conversation_reads": true,
}

// Bus fans notices out to the dashboard's open pages.
type Bus struct {
	wait    time.Duration
	mu      sync.Mutex
	subs    map[int]chan []string
	next    int
	pending map[string]bool
	timer   *time.Timer
}

func New(wait time.Duration) *Bus {
	return &Bus{wait: wait, subs: map[int]chan []string{}, pending: map[string]bool{}}
}

// Wrote is the store's OnWrite: a table changed.
func (b *Bus) Wrote(table string) {
	if !Followed[table] {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) == 0 {
		return // nobody looking
	}
	b.pending[table] = true
	if b.timer == nil {
		b.timer = time.AfterFunc(b.wait, b.flush)
	}
}

func (b *Bus) flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timer = nil
	tables := make([]string, 0, len(b.pending))
	for t := range b.pending {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	b.pending = map[string]bool{}
	for _, ch := range b.subs {
		select {
		case ch <- tables:
		default: // a slow page catches up on the next notice
		}
	}
}

// Subscribe follows the notices until stop.
func (b *Bus) Subscribe() (<-chan []string, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan []string, 4)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
	}
}
