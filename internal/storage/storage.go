// Package storage defines the persistence contract shared by every driver.
//
// Drivers live in sub-packages (sqlite now, postgres later) and must pass the
// same contract tests. Only the repositories needed by the current milestone
// exist; the rest of docs/DATA_MODEL.md is added table by table.
package storage

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a lookup matches no row.
	ErrNotFound = errors.New("storage: not found")
	// ErrConflict is returned when a unique constraint would be violated.
	ErrConflict = errors.New("storage: conflict")
)

// Role is a coarse permission level for dashboard users.
type Role string

const (
	RoleAdmin  Role = "admin"  // manage users, config, approvals
	RoleMember Role = "member" // read everything, ask, comment
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleMember }

// User is a dashboard account.
type User struct {
	ID           string
	Email        string
	Name         string
	Role         Role
	PasswordHash string
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

// Session is a logged-in browser. Only the SHA-256 of the token is stored.
type Session struct {
	ID         string
	UserID     string
	TokenHash  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	IP         string
}

// AuditEntry is one append-only audit record (ADR-043: who, who approved,
// where from, which channel, before/after).
type AuditEntry struct {
	ID     string
	Actor  string // older "human:<email>" form, kept for compatibility
	Action string
	Target string
	Detail map[string]any
	At     time.Time

	ActorKind  string // human | agent | automation | system
	ActorID    string
	ActorName  string
	ApprovedBy string // the person who approved an agent's proposal
	Via        string // ui | chat | task | assistant | mcp | automation | api

	ProjectID, ConversationID, JobID, TaskID, ActionID string

	Resource, ResourceID string
	Before, After        map[string]any // nil = no snapshot
	OK                   bool
}

// AuditFilter selects audit rows; empty fields match everything.
type AuditFilter struct {
	ProjectID, Resource, ResourceID, ActorKind, ActorID, ActorName, Via, ConversationID, JobID, TaskID string
	From, To                                                                                           time.Time // zero = unbounded
	BeforeID                                                                                           string    // cursor: the id of the last row of the previous page
	Limit                                                                                              int       // default 100, at most 500
}

// AuditCount is one group of a Count.
type AuditCount struct {
	Key           string
	Count, Failed int
}

// Store is the root handle a driver returns.
type Store interface {
	Migrate(ctx context.Context) error
	Close() error

	Users() UserRepo
	Sessions() SessionRepo
	Audit() AuditRepo

	Providers() ProviderRepo
	OrgModels() OrgModelRepo
	Agents() AgentRepo
	Repos() RepoRepo
	Revisions() RevisionRepo
	Runs() RunRepo
	Settings() SettingRepo
	Chat() ChatRepo
	Tasks() TaskRepo
	Processes() ProcessRepo
	Monitors() MonitorRepo
	Actions() ActionRepo
	Jobs() JobRepo
	Automations() AutomationRepo

	// InTx runs fn in one transaction; the Store passed to fn is bound to it.
	InTx(ctx context.Context, fn func(Store) error) error
}

// UserRepo manages dashboard accounts.
type UserRepo interface {
	Create(ctx context.Context, u User) (User, error)
	GetByID(ctx context.Context, id string) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	List(ctx context.Context) ([]User, error)
	Count(ctx context.Context) (int, error)
	UpdatePassword(ctx context.Context, id, hash string) error
	SetDisabled(ctx context.Context, id string, disabled bool) error
	TouchLogin(ctx context.Context, id string, at time.Time) error
}

// SessionRepo manages login sessions.
type SessionRepo interface {
	Create(ctx context.Context, s Session) (Session, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	Touch(ctx context.Context, id string, at time.Time) error
	Delete(ctx context.Context, id string) error
	DeleteForUser(ctx context.Context, userID string) error
	// DeleteForUserExcept revokes every session of userID but keepID.
	DeleteForUserExcept(ctx context.Context, userID, keepID string) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// AuditRepo appends audit records. There is deliberately no update or delete.
type AuditRepo interface {
	Append(ctx context.Context, e AuditEntry) error
	List(ctx context.Context, f AuditFilter) ([]AuditEntry, error)
	// Count groups by "day" (UTC), "kind", "actor" (kind:name), "resource", "via" or "project".
	Count(ctx context.Context, f AuditFilter, by string) ([]AuditCount, error)
}
