package chat

import (
	"context"
	"errors"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// beginJob returns the job a chat answer runs as: the one ctx carries (an
// automation's), marked running, or a new one for the person who sent it.
func (e *Engine) beginJob(ctx context.Context, conv storage.Conversation, agentID, title string) (storage.Job, error) {
	now := time.Now().UTC()
	if id := usage.JobFrom(ctx); id != "" {
		j, err := e.store.Jobs().Get(ctx, id)
		if err != nil {
			return j, err
		}
		j.Status, j.ConversationID, j.AgentID, j.StartedAt = "running", conv.ID, agentID, &now
		if j.Title == "" {
			j.Title = title
		}
		return j, e.store.Jobs().Update(ctx, j)
	}
	return e.store.Jobs().Create(ctx, storage.Job{ProjectID: conv.ProjectID, Kind: "chat_turn", Origin: "user", Trigger: "ui",
		CreatedBy: actor.From(ctx), ConversationID: conv.ID, AgentID: agentID, Title: title, Status: "running", StartedAt: &now})
}

// endJob finishes the job of an answer (cost and tokens come from its runs).
func (e *Engine) endJob(jobID, messageID string, runErr, ctxErr error) {
	ctx := context.Background()
	if messageID != "" {
		if j, err := e.store.Jobs().Get(ctx, jobID); err == nil {
			j.MessageID = messageID
			_ = e.store.Jobs().Update(ctx, j)
		}
	}
	status, code, msg := "done", "", ""
	var be *usage.BudgetError
	switch {
	case errors.Is(ctxErr, context.Canceled):
		status, code = "cancelled", "cancelled"
	case runErr != nil && errors.As(runErr, &be):
		status, code, msg = "failed", "budget", runErr.Error()
	case runErr != nil:
		status, code, msg = "failed", "agent_error", runErr.Error()
	}
	_, _ = e.store.Jobs().Finish(ctx, jobID, status, code, msg, time.Now().UTC())
}
