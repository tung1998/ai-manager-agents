// Package deciding knows the proposals (actions, diffs) being decided right
// now, whoever decides them: a person on the dashboard or a bot, or office on
// its own (an agent's permission, a bot's direct mode, a diff applied at
// once). A decision can take a while (a test, a merge), and the row says
// pending until it ends: "Cần xử lý", the cards and the bots ask here so they
// show it as being decided instead of asking someone to approve it again.
package deciding

import "sync"

var (
	now      sync.Map // id → by
	mu       sync.RWMutex
	onChange func(table string)
)

// OnChange tells fn when a decision starts or ends, with its table (actions |
// patches): the dashboard is told the row changed.
func OnChange(fn func(table string)) {
	mu.Lock()
	onChange = fn
	mu.Unlock()
}

func changed(table string) {
	mu.RLock()
	fn := onChange
	mu.RUnlock()
	if fn != nil {
		fn(table)
	}
}

// Begin marks id (in table) as being decided by by; false: it already is,
// and this decision must not run.
func Begin(table, id, by string) bool {
	if _, busy := now.LoadOrStore(id, by); busy {
		return false
	}
	changed(table)
	return true
}

// End: the decision of id is over (its row says how it went).
func End(table, id string) {
	now.Delete(id)
	changed(table)
}

// Is reports whether id is being decided, and by whom.
func Is(id string) (by string, ok bool) {
	v, ok := now.Load(id)
	if !ok {
		return "", false
	}
	return v.(string), true
}
