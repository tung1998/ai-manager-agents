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
	ReviewProfileID                        string // its review profile (ADR-113); "" = none
	EndsAt                                 *time.Time
	State                                  string // running | stopped | waiting_limit
	WaitingUntil                           *time.Time
	StartedBy                              string
	StartedAt                              *time.Time
	CreatedAt, UpdatedAt                   time.Time
}

// BurnReviewProfile is a saved review setup of a project (ADR-113): the
// stages reviewed, each with its own reviewer.
type BurnReviewProfile struct {
	ID, ProjectID, Name  string
	Stages               map[string]BurnReviewStage // issue | plan | result; absent = not reviewed
	CreatedAt, UpdatedAt time.Time
}

// BurnReviewStage is who reviews a stage: an agent ("" = the Burn's), and
// the workflow it runs ("" = it answers itself).
type BurnReviewStage struct {
	AgentID  string `json:"agent_id"`
	Workflow string `json:"workflow"`
}

// BurnItem is a piece of work a Burn found, and how it went.
type BurnItem struct {
	ID, SessionID, Title, Kind, Detail string
	Status                             string // found | queued | doing | paused | done | failed | skipped
	Priority                           int
	Branch, Worktree, Summary          string
	Attempts, Subagents                int
	Reviewed                           []string          // review stages passed
	ReviewNote                         string            // the reviewer's last word: guidance, or why not
	ReviewConversations                map[string]string // stage → its hidden review chat (ADR-114)
	CostUSD                            float64
	CreatedAt, UpdatedAt               time.Time
}

// BurnRepo stores Burn sessions (one a project) and their items.
type BurnRepo interface {
	// Session is a project's (ErrNotFound: none yet).
	Session(ctx context.Context, projectID string) (BurnSession, error)
	// SessionByConversation: the Burn's own chat, or one of its pieces' review chats.
	SessionByConversation(ctx context.Context, conversationID string) (BurnSession, error)
	SessionByID(ctx context.Context, id string) (BurnSession, error)
	SaveSession(ctx context.Context, s BurnSession) (BurnSession, error)
	Running(ctx context.Context) ([]BurnSession, error)
	AddItem(ctx context.Context, it BurnItem) (BurnItem, error)
	UpdateItem(ctx context.Context, it BurnItem) error
	// UpdateItemFrom saves it only if the item's current status still equals
	// fromStatus (compare-and-swap); ErrConflict when it has since changed —
	// two burn_pick (or other status change) calls racing for the same item
	// must not both succeed.
	UpdateItemFrom(ctx context.Context, it BurnItem, fromStatus string) error
	Item(ctx context.Context, id string) (BurnItem, error)
	Items(ctx context.Context, sessionID string) ([]BurnItem, error)

	ReviewProfiles(ctx context.Context, projectID string) ([]BurnReviewProfile, error)
	ReviewProfile(ctx context.Context, id string) (BurnReviewProfile, error)
	SaveReviewProfile(ctx context.Context, p BurnReviewProfile) (BurnReviewProfile, error)
	// DeleteReviewProfile removes it; the Burns that followed it review no more.
	DeleteReviewProfile(ctx context.Context, id string) error
}
