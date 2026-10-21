package storage

import (
	"context"
	"time"
)

// The support pack's data (ADR-171): sources, tickets and their history.
// Only internal/support reads and writes it.

// SupportSource is where customers' messages come from.
type SupportSource struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id"`
	Kind         string         `json:"kind"` // api | form | crisp | bot
	Name         string         `json:"name"`
	AutomationID string         `json:"automation_id"` // answers: a chat or workflow automation
	Mode         string         `json:"mode"`          // auto: the answer goes out; approve: a person approves it first
	TokenHash    string         `json:"-"`
	SecretEnc    string         `json:"-"` // sealed: Crisp key, SMTP password
	Config       map[string]any `json:"config"`
	RatePerMin   int            `json:"rate_per_min"`
	Enabled      bool           `json:"enabled"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// SupportTicket is one matter of a customer.
type SupportTicket struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id"`
	SourceID        string     `json:"source_id"`
	ExternalThread  string     `json:"external_thread"`
	CustomerID      string     `json:"customer_id"`
	Subject         string     `json:"subject"`
	Status          string     `json:"status"`
	CaseType        string     `json:"case_type"`
	Assignee        string     `json:"assignee"`
	Priority        string     `json:"priority"`
	Tags            []string   `json:"tags"`
	FirstResponseAt *time.Time `json:"first_response_at"`
	ResolvedAt      *time.Time `json:"resolved_at"`
	SLADueAt        *time.Time `json:"sla_due_at"` // the first answer is due by then
	SLABreached     bool       `json:"sla_breached"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// SupportEvent is one line of a ticket's history.
type SupportEvent struct {
	ID             string         `json:"id"`
	TicketID       string         `json:"ticket_id"`
	SourceID       string         `json:"-"`
	Kind           string         `json:"kind"` // customer_msg ai_draft sent note status
	Body           string         `json:"body"`
	Actor          string         `json:"actor"`
	ExternalID     string         `json:"external_id,omitempty"`
	JobID          string         `json:"job_id,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	ReplyTo        string         `json:"reply_to,omitempty"`
	State          string         `json:"state,omitempty"`
	Meta           map[string]any `json:"meta,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// SupportOut is one thing to deliver: a reply to the customer, or a hook.
type SupportOut struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Kind      string         `json:"kind"`   // reply | hook
	RefID     string         `json:"ref_id"` // reply: the sent event; hook: the hook
	TicketID  string         `json:"ticket_id"`
	Event     string         `json:"event"`
	Payload   map[string]any `json:"payload"`
	Status    string         `json:"status"` // pending sent failed
	Attempts  int            `json:"attempts"`
	LastError string         `json:"last_error"`
	NextAt    time.Time      `json:"next_at"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// SupportHook is told of a ticket's events.
type SupportHook struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"` // webhook | automation
	URL          string    `json:"url"`
	AutomationID string    `json:"automation_id"`
	Events       []string  `json:"events"` // none = every event
	SecretEnc    string    `json:"-"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SupportRepo keeps the support pack's data.
type SupportRepo interface {
	CreateSource(ctx context.Context, s SupportSource) (SupportSource, error)
	UpdateSource(ctx context.Context, s SupportSource) error // name, automation, mode, token, config, rate, enabled
	Source(ctx context.Context, id string) (SupportSource, error)
	Sources(ctx context.Context, projectID string) ([]SupportSource, error)

	CreateTicket(ctx context.Context, t SupportTicket) (SupportTicket, error)
	UpdateTicket(ctx context.Context, t SupportTicket) error
	Ticket(ctx context.Context, id string) (SupportTicket, error)
	// OpenTicket: the source's ticket on thread that is not resolved or closed.
	OpenTicket(ctx context.Context, sourceID, thread string) (SupportTicket, error)
	Tickets(ctx context.Context, projectID, status string, limit int) ([]SupportTicket, error)
	TicketsSince(ctx context.Context, projectID string, since time.Time) ([]SupportTicket, error)
	// Overdue: tickets past their SLA with no answer yet, not told so.
	Overdue(ctx context.Context, now time.Time) ([]SupportTicket, error)

	AddEvent(ctx context.Context, e SupportEvent) (SupportEvent, error) // ErrConflict: the source sent this external id already
	UpdateEvent(ctx context.Context, e SupportEvent) error              // body, state, conversation, meta
	Event(ctx context.Context, id string) (SupportEvent, error)
	EventByExternal(ctx context.Context, sourceID, externalID string) (SupportEvent, error)
	EventByJob(ctx context.Context, jobID string) (SupportEvent, error)
	Events(ctx context.Context, ticketID string) ([]SupportEvent, error)

	AddOut(ctx context.Context, o SupportOut) (SupportOut, error)
	UpdateOut(ctx context.Context, o SupportOut) error // status, attempts, error, next
	DueOut(ctx context.Context, now time.Time, limit int) ([]SupportOut, error)
	Outs(ctx context.Context, projectID string, limit int) ([]SupportOut, error)

	CreateHook(ctx context.Context, h SupportHook) (SupportHook, error)
	UpdateHook(ctx context.Context, h SupportHook) error
	DeleteHook(ctx context.Context, id string) error
	Hook(ctx context.Context, id string) (SupportHook, error)
	Hooks(ctx context.Context, projectID string) ([]SupportHook, error)
}
