package tasks

import (
	"context"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// BackfillJobs gives each task from before jobs existed its job, dated as the
// task was, so the Jobs page shows the whole history. It is safe to run again.
func BackfillJobs(ctx context.Context, store storage.Store) error {
	list, err := store.Tasks().List(ctx, "", 100000)
	if err != nil {
		return err
	}
	for _, t := range list {
		if has, err := store.Jobs().List(ctx, storage.JobFilter{TaskID: t.ID, Limit: 1}); err != nil || len(has) > 0 {
			continue
		}
		status, msg := t.Status, ""
		switch t.Status {
		case "done", "failed", "cancelled":
		case "rejected":
			status, msg = "failed", "bị từ chối"
		default: // still "running" from before: it ended with the office that ran it
			status, msg = "failed", "dừng khi office khởi động lại"
		}
		origin := "user"
		if strings.HasPrefix(t.CreatedBy, "auto:") {
			origin = "automation"
		}
		created := t.CreatedAt
		if _, err := store.Jobs().Create(ctx, storage.Job{ProjectID: t.ProjectID, Kind: "task", Origin: origin, TaskID: t.ID, Trigger: "ui",
			CreatedBy: t.CreatedBy, Title: t.Title, Status: status, Error: msg, CostUSD: t.CostUSD, AgentID: t.AssigneeID,
			CreatedAt: created, StartedAt: &created, FinishedAt: t.FinishedAt}); err != nil {
			return err
		}
	}
	return nil
}
