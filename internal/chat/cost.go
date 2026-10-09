package chat

import (
	"context"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// sessionCostKey keeps what a Claude Code session reported it cost so far.
func sessionCostKey(sessionID string) string { return "session_cost:" + sessionID }

// turnCost is what one turn cost (ADR-125). Claude Code's total_cost_usd is
// the session's total so far, across --resume: taken as is, a long chat was
// counted again every turn (a Burn's coordinator: $388 recorded for ~$21).
// A resumed turn costs what it added to the total kept for its session; one
// resuming a session with nothing kept (from before) is 0, priced from its
// tokens instead. The new total is kept for the next turn.
func (e *Engine) turnCost(ctx context.Context, kind storage.ProviderKind, resumed, session string, total float64) float64 {
	if kind != storage.ProviderClaudeCLI || total <= 0 {
		return total
	}
	cost := total
	if resumed != "" {
		var prev float64
		ok, _ := e.store.Settings().Get(ctx, sessionCostKey(resumed), &prev)
		switch {
		case ok && total >= prev:
			cost = total - prev
		case ok: // below what was kept: the resume failed, a new session ran
		default:
			cost = 0
		}
	}
	if session != "" {
		_ = e.store.Settings().Set(context.WithoutCancel(ctx), sessionCostKey(session), total)
	}
	return cost
}
