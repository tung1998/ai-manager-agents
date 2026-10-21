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

	AddEvent(ctx context.Context, e SupportEvent) (SupportEvent, error) // ErrConflict: the source sent this external id already
	UpdateEvent(ctx context.Context, e SupportEvent) error              // body, state, conversation, meta
	Event(ctx context.Context, id string) (SupportEvent, error)
	EventByExternal(ctx context.Context, sourceID, externalID string) (SupportEvent, error)
	EventByJob(ctx context.Context, jobID string) (SupportEvent, error)
	Events(ctx context.Context, ticketID string) ([]SupportEvent, error)
}
