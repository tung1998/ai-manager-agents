package storage

import (
	"context"
	"time"
)

// Consult is one question to an expert (ADR-177): an agent of a linked
// project, in this office or another, answering from its own knowledge.
type Consult struct {
	ID                string
	Side              string // ConsultOut, ConsultIn, ConsultLocal
	PeerID            string // the other office ("" = this one)
	RemoteID          string // in: the asking office's id of it
	ConversationID    string // out/local: the asking chat
	ProjectID         string // out/local: the asking project
	CallerAgentID     string // out/local: the agent that asked
	Actor             string // the person it runs for
	Ceiling           string // the asking turn's ceiling ("" = none)
	RootMessageID     string // the person's message the chain started from
	ExpertProject     string
	ExpertAgentID     string
	ExpertName        string
	ExpertProjectName string
	FromLabel         string // in: who asks
	Question          string
	ConsultConvID     string // in/local: the expert's hidden chat
	Status            string // ConsultPending, ConsultAnswered, ConsultFailed
	Answer            string
	Error             string
	Deadline          time.Time
	CreatedAt         time.Time
	DoneAt            *time.Time
}

const (
	ConsultOut   = "out"
	ConsultIn    = "in"
	ConsultLocal = "local"

	ConsultPending  = "pending"
	ConsultAnswered = "answered"
	ConsultFailed   = "failed"
)

// ConsultRepo keeps the questions to experts.
type ConsultRepo interface {
	Create(ctx context.Context, c Consult) (Consult, error)
	Get(ctx context.Context, id string) (Consult, error)
	// GetIn finds a question another office asked here by its id there.
	GetIn(ctx context.Context, peerID, remoteID string) (Consult, error)
	// Finish ends a pending question once: false when it was not pending.
	Finish(ctx context.Context, id, status, answer, errText string) (bool, error)
	SetConsultConversation(ctx context.Context, id, conversationID string) error
	// Asked: the pending questions an asking chat is waiting on.
	Asked(ctx context.Context, conversationID string) ([]Consult, error)
	// Waiting: the pending questions an expert's chat is to answer, oldest first.
	Waiting(ctx context.Context, consultConversationID string) ([]Consult, error)
	// CountRoot: the questions a chain from one person's message asked.
	CountRoot(ctx context.Context, conversationID, rootMessageID string) (int, error)
	// CountInSince: questions office peerID asked here since.
	CountInSince(ctx context.Context, peerID string, since time.Time) (int, error)
	// Expired: pending questions past their deadline.
	Expired(ctx context.Context, now time.Time) ([]Consult, error)
}
