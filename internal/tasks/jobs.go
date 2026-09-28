package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// QueuedTask is the payload of a task waiting for its project to be free.
type QueuedTask struct {
	Goal        string   `json:"goal"`
	BudgetUSD   float64  `json:"budget_usd"`
	Attachments []string `json:"attachments"`
	Mode        string   `json:"mode"`
	EditMode    string   `json:"edit_mode"`
}

// Queue records a task to start once the project is free: a pending job the
// trigger runner starts (ADR-040).
func (s *Service) Queue(ctx context.Context, projectID, goal string, budgetUSD float64, attachmentIDs []string, permMode, editMode string) (storage.Job, error) {
	raw, _ := json.Marshal(QueuedTask{Goal: goal, BudgetUSD: budgetUSD, Attachments: attachmentIDs, Mode: permMode, EditMode: editMode})
	now := time.Now().UTC()
	return s.store.Jobs().Create(ctx, storage.Job{ProjectID: projectID, Kind: "task", Origin: "user", Trigger: "ui", CreatedBy: actor.From(ctx),
		Title: truncate(strings.Join(strings.Fields(goal), " "), 90), Status: "pending", Payload: string(raw), NextAttemptAt: &now})
}

// beginJob returns the job a task runs as: the one ctx carries (queued or an
// automation's), marked running, or a new one for the person who gave it.
func (s *Service) beginJob(ctx context.Context, t storage.Task) (storage.Job, error) {
	now := time.Now().UTC()
	if id := usage.JobFrom(ctx); id != "" {
		j, err := s.store.Jobs().Get(ctx, id)
		if err != nil {
			return j, err
		}
		j.Status, j.TaskID, j.StartedAt = "running", t.ID, &now
		if j.Title == "" {
			j.Title = t.Title
		}
		return j, s.store.Jobs().Update(ctx, j)
	}
	return s.store.Jobs().Create(ctx, storage.Job{ProjectID: t.ProjectID, Kind: "task", Origin: "user", Trigger: "ui", CreatedBy: t.CreatedBy,
		TaskID: t.ID, Title: t.Title, Status: "running", StartedAt: &now})
}

// jobStatus maps how a task ended to its job.
func jobStatus(taskStatus string) (status, code string) {
	switch taskStatus {
	case "done":
		return "done", ""
	case "needs_input":
		return "needs_input", ""
	case "cancelled":
		return "cancelled", "cancelled"
	default:
		return "failed", "agent_error"
	}
}
