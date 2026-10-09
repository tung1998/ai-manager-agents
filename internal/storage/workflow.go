package storage

import (
	"context"
	"time"
)

// Workflow is a workflow installed in a project: a copy of a library one
// (spec 2026-10-07-workflows-design), changed there on its own.
type Workflow struct {
	ID, ProjectID string
	Key, Name     string
	Description   string
	Source        string            // the whole file (YAML header + the coordinator's instructions)
	SourceKey     string            // the library workflow it was copied from ("" = made here)
	SourceHash    string            // that one's hash when copied (a newer one shows "có bản mới")
	Bindings      map[string]string // role → agent id ("" = the coordinator picks)
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// WorkflowRepo stores a project's workflows.
type WorkflowRepo interface {
	Create(ctx context.Context, w Workflow) (Workflow, error) // ErrConflict: the key is taken in the project
	Update(ctx context.Context, w Workflow) error
	Get(ctx context.Context, id string) (Workflow, error)
	GetByKey(ctx context.Context, projectID, key string) (Workflow, error)
	List(ctx context.Context, projectID string) ([]Workflow, error) // "" = every project
	Delete(ctx context.Context, id string) error
}

// Workflow run statuses.
const (
	RunRunning = "running"
	RunDone    = "done"
	RunFailed  = "failed"
	RunStopped = "stopped"
)

// WorkflowRun is one run of a workflow. It runs in a chat of its own
// (ConversationID, purpose "workflow_run"); the chat that called it
// (CallerConversationID) shows only its input and its output.
type WorkflowRun struct {
	ID, ProjectID, ConversationID string
	CallerConversationID          string // "" = a run from before, held in ConversationID itself
	ParentRunID                   string // the run whose role called this one ("" = a chat called it)
	Depth                         int    // 0 = called from a chat, 1 = by a run called from one…
	WorkflowID, WorkflowKey       string
	WorkflowName                  string
	BodyHash                      string // the workflow as it was when run
	CoordinatorID                 string
	CoordinatorName               string
	Input                         string // what the person asked
	Status                        string // RunRunning | RunDone | RunFailed | RunStopped
	Turns                         int    // turns of the roles so far
	CostUSD                       float64
	Result, Error                 string
	Outputs                       map[string]string // its declared outputs, by key (ADR-103)
	Roles                         []RunRole
	Gates                         []RunGate
	Log                           []RunLog
	Actor                         string
	StartedAt                     time.Time
	FinishedAt                    *time.Time
}

// RunRole is a role of a run: who fills it and how far it got.
type RunRole struct {
	Role      string  `json:"role"`
	Name      string  `json:"name"`
	Access    string  `json:"access"`
	AgentID   string  `json:"agent_id"`
	AgentName string  `json:"agent_name"`
	Status    string  `json:"status"` // idle | working | done | failed
	Rounds    int     `json:"rounds"` // follow-ups sent
	Turns     int     `json:"turns"`
	SessionID string  `json:"session_id,omitempty"` // its own session, resumed by follow-ups
	Runtime   string  `json:"runtime,omitempty"`
	Result    string  `json:"result,omitempty"`   // its last answer (cut)
	Workflow  string  `json:"workflow,omitempty"` // a sub-workflow fills it (its key)
	RunID     string  `json:"run_id,omitempty"`   // that one's latest run
	CostUSD   float64 `json:"cost_usd"`
	// Objection: what it pushed back on in its last answer (PHẢN BIỆN), until
	// it answers again or the coordinator overrules it in workflow_done
	Objection string `json:"objection,omitempty"`
}

// RunGate is a gate of a run.
type RunGate struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
	Status   string `json:"status"` // open | waiting | passed | failed
	ActionID string `json:"action_id,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// RunLog is one line of what a run did.
type RunLog struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// WorkflowRunRepo stores runs.
type WorkflowRunRepo interface {
	Create(ctx context.Context, r WorkflowRun) (WorkflowRun, error)
	Update(ctx context.Context, r WorkflowRun) error
	Get(ctx context.Context, id string) (WorkflowRun, error)
	// List is newest first; empty ids match everything. conversationID
	// matches the run's own chat or the chat that called it.
	List(ctx context.Context, projectID, conversationID string, limit int) ([]WorkflowRun, error)
	// ListByWorkflow is List further filtered to one workflow, newest first; the
	// filter is applied in SQL before the limit, so a rarely-run workflow's
	// history is not pushed out by other workflows' runs.
	ListByWorkflow(ctx context.Context, projectID, conversationID, workflowID string, limit int) ([]WorkflowRun, error)
	// FailRunning ends the runs left running (the office restarted under them).
	FailRunning(ctx context.Context, detail string, at time.Time) (int64, error)
}
