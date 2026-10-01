package storage

import (
	"context"
	"time"
)

// BurnSession is a project's Burn: its settings and whether it runs.
type BurnSession struct {
	ID, ProjectID, ConversationID, AgentID string
	ModelTier                              string // strong | balanced | fast
	MaxSubagents                           int
	ResultMode                             string // branch | patch
	Focus                                  string
	Order                                  string // roadmap | bugs | auto: what it looks for first
	EndsAt                                 *time.Time
	State                                  string // running | stopped | waiting_limit
	WaitingUntil                           *time.Time
	StartedBy                              string
	StartedAt                              *time.Time
	CreatedAt, UpdatedAt                   time.Time
}

// BurnItem is a piece of work a Burn found, and how it went.
type BurnItem struct {
	ID, SessionID, Title, Kind, Detail string
	Status                             string // found | queued | doing | paused | done | failed | skipped
	Priority                           int
	Branch, Worktree, Summary          string
	Attempts, Subagents                int
	CostUSD                            float64
	CreatedAt, UpdatedAt               time.Time
}

// BurnRepo stores Burn sessions (one a project) and their items.
type BurnRepo interface {
	// Session is a project's (ErrNotFound: none yet).
	Session(ctx context.Context, projectID string) (BurnSession, error)
	SessionByConversation(ctx context.Context, conversationID string) (BurnSession, error)
	SessionByID(ctx context.Context, id string) (BurnSession, error)
	SaveSession(ctx context.Context, s BurnSession) (BurnSession, error)
	Running(ctx context.Context) ([]BurnSession, error)
	AddItem(ctx context.Context, it BurnItem) (BurnItem, error)
	UpdateItem(ctx context.Context, it BurnItem) error
	Item(ctx context.Context, id string) (BurnItem, error)
	Items(ctx context.Context, sessionID string) ([]BurnItem, error)
}
