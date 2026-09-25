package tasks

import (
	"context"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// TaskDTO is a task for the dashboard.
type TaskDTO struct {
	ID          string               `json:"id"`
	ProjectID   string               `json:"project_id"`
	Title       string               `json:"title"`
	Goal        string               `json:"goal"`
	Mode        string               `json:"mode"`
	Status      string               `json:"status"`
	Result      string               `json:"result"`
	Detail      string               `json:"detail"`
	BudgetUSD   float64              `json:"budget_usd"`
	CostUSD     float64              `json:"cost_usd"`
	Attachments []storage.Attachment `json:"attachments"`
	// diffs waiting for approval / applied: a "done" task with pending ones is awaiting review
	PendingPatches int        `json:"pending_patches"`
	AppliedPatches int        `json:"applied_patches"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

// StepDTO is a step for the dashboard.
type StepDTO struct {
	ID          string             `json:"id"`
	Seq         int                `json:"seq"`
	Phase       string             `json:"phase"`
	AgentKey    string             `json:"agent_key"`
	AgentName   string             `json:"agent_name"`
	Instruction string             `json:"instruction"`
	Output      string             `json:"output"`
	Data        map[string]any     `json:"data"`
	Tools       []storage.ToolCall `json:"tools"`
	Status      string             `json:"status"`
	Error       string             `json:"error"`
	CostUSD     *float64           `json:"cost_usd"`
	StartedAt   time.Time          `json:"started_at"`
	FinishedAt  *time.Time         `json:"finished_at"`
}

func toTaskDTO(t storage.Task) TaskDTO {
	return TaskDTO{ID: t.ID, ProjectID: t.ProjectID, Title: t.Title, Goal: t.Goal, Mode: t.Mode, Status: t.Status, Result: t.Result,
		Detail: t.Detail, BudgetUSD: t.BudgetUSD, CostUSD: t.CostUSD, Attachments: atts(t.Attachments), CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, FinishedAt: t.FinishedAt}
}

func toStepDTO(s storage.TaskStep) StepDTO {
	tools := s.Tools
	if tools == nil {
		tools = []storage.ToolCall{}
	}
	data := s.Data
	if data == nil {
		data = map[string]any{}
	}
	return StepDTO{ID: s.ID, Seq: s.Seq, Phase: s.Phase, AgentKey: s.AgentKey, AgentName: s.AgentName, Instruction: s.Instruction,
		Output: s.Output, Data: data, Tools: tools, Status: s.Status, Error: s.Error, CostUSD: s.CostUSD, StartedAt: s.StartedAt, FinishedAt: s.FinishedAt}
}

// Detail is a task with its steps and patches.
type Detail struct {
	Task    TaskDTO          `json:"task"`
	Steps   []StepDTO        `json:"steps"`
	Patches []chat.PatchDTO  `json:"patches"`
	Actions []chat.ActionDTO `json:"actions"`
	Running bool             `json:"running"`
}

// List returns a project's tasks, newest first.
func (s *Service) List(ctx context.Context, projectID string) ([]TaskDTO, error) {
	list, err := s.store.Tasks().List(ctx, projectID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]TaskDTO, 0, len(list))
	for _, t := range list {
		d := toTaskDTO(t)
		if patches, err := s.store.Tasks().ListPatches(ctx, t.ID); err == nil {
			d.PendingPatches, d.AppliedPatches = countPatches(patches)
		}
		out = append(out, d)
	}
	return out, nil
}

// Get returns a task with steps and patches.
func (s *Service) Get(ctx context.Context, id string) (Detail, error) {
	t, err := s.store.Tasks().Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	steps, err := s.store.Tasks().ListSteps(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	patches, err := s.store.Tasks().ListPatches(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Task: toTaskDTO(t), Steps: []StepDTO{}, Patches: []chat.PatchDTO{}, Actions: []chat.ActionDTO{}}
	if acts, err := s.store.Actions().List(ctx, "", id, ""); err == nil {
		for _, a := range acts {
			d.Actions = append(d.Actions, chat.ToActionDTO(a))
		}
	}
	for _, st := range steps {
		d.Steps = append(d.Steps, toStepDTO(st))
	}
	for _, p := range patches {
		d.Patches = append(d.Patches, chat.PatchDTO{ID: p.ID, Diff: p.Diff, Files: p.Files, Status: p.Status, Detail: p.Detail, DecidedBy: p.DecidedBy, DecidedAt: p.DecidedAt})
	}
	d.Task.PendingPatches, d.Task.AppliedPatches = countPatches(patches)
	if l, ok := s.Live(id); ok {
		_, done, _ := l.Since(0)
		d.Running = !done
	}
	return d, nil
}

func atts(a []storage.Attachment) []storage.Attachment {
	if a == nil {
		return []storage.Attachment{}
	}
	return a
}

func countPatches(ps []storage.Patch) (pending, applied int) {
	for _, p := range ps {
		switch p.Status {
		case "pending":
			pending++
		case "applied":
			applied++
		}
	}
	return
}
