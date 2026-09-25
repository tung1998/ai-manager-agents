package storage

import (
	"context"
	"time"
)

// ProviderKind is how agent-office reaches a model.
type ProviderKind string

const (
	ProviderAnthropic        ProviderKind = "anthropic"         // Anthropic Messages API
	ProviderOpenAI           ProviderKind = "openai"            // OpenAI API
	ProviderOpenAICompatible ProviderKind = "openai_compatible" // any /v1/chat/completions endpoint
	ProviderClaudeCLI        ProviderKind = "claude_cli"        // local `claude -p`
	ProviderCodexCLI         ProviderKind = "codex_cli"         // local `codex exec`
)

// Valid reports whether k is known.
func (k ProviderKind) Valid() bool {
	switch k {
	case ProviderAnthropic, ProviderOpenAI, ProviderOpenAICompatible, ProviderClaudeCLI, ProviderCodexCLI:
		return true
	}
	return false
}

// IsCLI reports whether the provider runs a local binary instead of an HTTP API.
func (k ProviderKind) IsCLI() bool { return k == ProviderClaudeCLI || k == ProviderCodexCLI }

// Model tiers let templates say "strong model" without naming a vendor model.
const (
	TierStrong   = "strong"
	TierBalanced = "balanced"
	TierFast     = "fast"
)

// ValidTier reports whether t is a model tier.
func ValidTier(t string) bool { return t == TierStrong || t == TierBalanced || t == TierFast }

// Provider is an AI connection.
type Provider struct {
	ID           string
	Name         string
	Kind         ProviderKind
	Preset       string // catalog entry it was made from ("" = by hand)
	BaseURL      string
	APIKeyEnc    string // encrypted; only internal/secrets decrypts it
	APIKeyEnv    string
	APIKeyHint   string
	TierModels   map[string]string
	Models       []string
	IsDefault    bool
	Enabled      bool
	Status       string // unknown | ok | error
	StatusDetail string
	CheckedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Org model kinds.
const (
	KindSolo    = "solo"
	KindTeam    = "team"
	KindCouncil = "council"
	KindCustom  = "custom"
)

// Governance says how the agents of an org model reach a decision.
type Governance struct {
	Mode   string   `json:"mode"`             // single | hierarchy | council
	Quorum int      `json:"quorum,omitempty"` // council: votes needed
	Veto   []string `json:"veto,omitempty"`   // agent keys that can block side effects
	Notes  string   `json:"notes,omitempty"`
}

// OrgModel is a template (RepoID empty) or a repo's instance.
type OrgModel struct {
	ID               string
	RepoID           string
	SourceTemplateID string
	Key              string
	Name             string
	Description      string
	Kind             string
	Governance       Governance
	Builtin          bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// IsTemplate reports whether m lives in the library.
func (m OrgModel) IsTemplate() bool { return m.RepoID == "" }

// Agent tiers.
const (
	TierLead    = "lead"
	TierManager = "manager"
	TierWorker  = "worker"
)

// Permissions bound what an agent may do.
type Permissions struct {
	// Level is the agent's permission package (see internal/perm); empty
	// means derived from ReadOnly (read / propose).
	Level    string `json:"level,omitempty"`
	ReadOnly bool   `json:"read_only"`
	// Caps are the agent's own picks of capabilities; nil = its package's preset.
	Caps *[]string `json:"caps,omitempty"`
	// Commands narrow the project's commands for this agent; nil = all of them.
	Commands         *[]string `json:"commands,omitempty"`
	Tools            []string  `json:"tools,omitempty"`
	RequiresApproval bool      `json:"requires_approval,omitempty"` // side effects need a human
}

// Agent belongs to one org model.
type Agent struct {
	ID           string
	OrgModelID   string
	Key          string
	Name         string
	Tier         string // lead | manager | worker
	Role         string
	Description  string
	ReportsTo    []string // agent keys in the same org model
	ProviderID   string
	ModelTier    string
	LLMModel     string
	Instructions string
	Permissions  Permissions
	Sort         int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Repo is a codebase agent-office manages.
type Repo struct {
	ID          string
	Name        string
	Path        string
	GitRemote   string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProviderRepo manages AI connections.
type ProviderRepo interface {
	Create(ctx context.Context, p Provider) (Provider, error)
	Update(ctx context.Context, p Provider) error
	Get(ctx context.Context, id string) (Provider, error)
	List(ctx context.Context) ([]Provider, error)
	Delete(ctx context.Context, id string) error
	SetDefault(ctx context.Context, id string) error
	SetStatus(ctx context.Context, id, status, detail string, models []string, at time.Time) error
}

// OrgModelRepo manages templates and repo instances.
type OrgModelRepo interface {
	Create(ctx context.Context, m OrgModel) (OrgModel, error)
	Update(ctx context.Context, m OrgModel) error
	Get(ctx context.Context, id string) (OrgModel, error)
	GetTemplateByKey(ctx context.Context, key string) (OrgModel, error)
	GetForRepo(ctx context.Context, repoID string) (OrgModel, error)
	ListTemplates(ctx context.Context) ([]OrgModel, error)
	Delete(ctx context.Context, id string) error
}

// AgentRepo manages agents of an org model.
type AgentRepo interface {
	Create(ctx context.Context, a Agent) (Agent, error)
	Update(ctx context.Context, a Agent) error
	Get(ctx context.Context, id string) (Agent, error)
	List(ctx context.Context, orgModelID string) ([]Agent, error)
	Delete(ctx context.Context, id string) error
}

// Revision is a snapshot of an org model taken before a change.
type Revision struct {
	ID         string
	OrgModelID string
	Action     string // what was about to happen: agent.update, model.update, restore…
	Actor      string
	AgentCount int
	Snapshot   []byte // JSON of orgmodel.Template
	CreatedAt  time.Time
}

// RevisionRepo stores org model snapshots.
type RevisionRepo interface {
	Create(ctx context.Context, r Revision) (Revision, error)
	Get(ctx context.Context, id string) (Revision, error)
	List(ctx context.Context, orgModelID string, limit int) ([]Revision, error)
	// Prune keeps the newest keep revisions of a model.
	Prune(ctx context.Context, orgModelID string, keep int) error
}

// Run is one model call.
type Run struct {
	ID           string
	Kind         string
	ProjectID    string
	AgentID      string
	ProviderID   string
	ProviderName string
	Model        string
	Status       string // ok | error | blocked
	InputTokens  int
	OutputTokens int
	CostUSD      *float64
	CostSource   string // provider | estimate | unknown
	DurationMS   int64
	Error        string
	Actor        string
	CreatedAt    time.Time
}

// RunFilter narrows a run listing.
type RunFilter struct {
	ProjectID string
	Since     time.Time
	Limit     int
}

// UsageRow is spend aggregated by one dimension.
type UsageRow struct {
	Key          string  `json:"key"` // day (YYYY-MM-DD), project id, or model
	Label        string  `json:"label"`
	Runs         int     `json:"runs"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	UnknownCost  int     `json:"unknown_cost"` // runs whose cost is unknown
}

// RunRepo stores model calls.
type RunRepo interface {
	Create(ctx context.Context, r Run) (Run, error)
	List(ctx context.Context, f RunFilter) ([]Run, error)
	// Spent sums known cost since t (optionally for one project).
	Spent(ctx context.Context, since time.Time, projectID string) (float64, error)
	// Since returns every run since t, newest first (for in-Go statistics).
	Since(ctx context.Context, t time.Time) ([]Run, error)
	// Aggregate groups runs since t by "day" (in loc), "project" or "model".
	Aggregate(ctx context.Context, since time.Time, by string, loc *time.Location) ([]UsageRow, error)
}

// SettingRepo stores JSON settings.
type SettingRepo interface {
	Get(ctx context.Context, key string, dst any) (bool, error)
	Set(ctx context.Context, key string, v any) error
}

// Conversation is a chat thread with one agent of a project.
type Conversation struct {
	ID        string
	ProjectID string
	AgentID   string
	AgentName string
	Title     string
	SessionID string
	Runtime   string
	Mode      string // permission mode (internal/perm level), a ceiling for this chat
	TaskID    string // set for the follow-up talk about one task
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ToolCall summarises one tool use while answering.
type ToolCall struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Error   bool   `json:"error,omitempty"`
}

// Message is one turn of a conversation.
type Message struct {
	ID             string
	ConversationID string
	Role           string // user | assistant | error
	Content        string
	Tools          []ToolCall
	Attachments    []Attachment
	RunID          string
	Author         string
	CreatedAt      time.Time
}

// Attachment references a file a person attached (see internal/attach).
type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // image | pdf | text
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// Patch is a proposed code change awaiting approval.
type Patch struct {
	ID             string
	ConversationID string
	MessageID      string
	TaskID         string
	StepID         string
	Diff           string
	Files          []string
	Status         string // pending | applied | rejected | failed
	Detail         string
	DecidedBy      string
	DecidedAt      *time.Time
	CreatedAt      time.Time
}

// ChatRepo stores conversations, messages and patches.
type ChatRepo interface {
	CreateConversation(ctx context.Context, c Conversation) (Conversation, error)
	UpdateConversation(ctx context.Context, c Conversation) error
	GetConversation(ctx context.Context, id string) (Conversation, error)
	// ListConversations lists a project's own chats (not the talks about tasks).
	ListConversations(ctx context.Context, projectID string, limit int) ([]Conversation, error)
	TaskConversation(ctx context.Context, taskID string) (Conversation, error)
	DeleteConversation(ctx context.Context, id string) error

	AddMessage(ctx context.Context, m Message) (Message, error)
	ListMessages(ctx context.Context, conversationID string) ([]Message, error)

	AddPatch(ctx context.Context, p Patch) (Patch, error)
	GetPatch(ctx context.Context, id string) (Patch, error)
	ListPatches(ctx context.Context, conversationID string) ([]Patch, error)
	DecidePatch(ctx context.Context, id, status, detail, by string, at time.Time) error
}

// Task is a goal given to a project's whole org model.
type Task struct {
	ID          string
	ProjectID   string
	Title       string
	Goal        string
	Mode        string // single | hierarchy | council
	Status      string // running | done | failed | cancelled | rejected
	Result      string
	Detail      string
	BudgetUSD   float64
	CostUSD     float64
	ModeLevel   string // permission mode (internal/perm level), a ceiling for this task
	Attachments []Attachment
	CreatedBy   string
	CreatedAt   time.Time
	FinishedAt  *time.Time
}

// TaskStep is one agent's turn inside a task.
type TaskStep struct {
	ID          string
	TaskID      string
	Seq         int
	Phase       string // plan | vote | revise | work | review | synthesize
	AgentID     string
	AgentKey    string
	AgentName   string
	Instruction string
	Output      string
	Data        map[string]any
	Tools       []ToolCall
	Status      string // running | done | failed | skipped
	Error       string
	CostUSD     *float64
	RunID       string
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// TaskRepo stores tasks and their steps.
type TaskRepo interface {
	Create(ctx context.Context, t Task) (Task, error)
	Update(ctx context.Context, t Task) error
	Get(ctx context.Context, id string) (Task, error)
	List(ctx context.Context, projectID string, limit int) ([]Task, error) // "" = all projects
	Delete(ctx context.Context, id string) error
	AddStep(ctx context.Context, s TaskStep) (TaskStep, error)
	UpdateStep(ctx context.Context, s TaskStep) error
	ListSteps(ctx context.Context, taskID string) ([]TaskStep, error)
	ListPatches(ctx context.Context, taskID string) ([]Patch, error)
}

// RepoRepo manages registered repositories.
type RepoRepo interface {
	Create(ctx context.Context, r Repo) (Repo, error)
	Update(ctx context.Context, r Repo) error
	Get(ctx context.Context, id string) (Repo, error)
	GetByPath(ctx context.Context, path string) (Repo, error)
	List(ctx context.Context) ([]Repo, error)
	Delete(ctx context.Context, id string) error
}

// Process is a command office runs for a project (dev server, build, test).
type Process struct {
	ID          string
	ProjectID   string
	Name        string
	Command     string
	Cwd         string // relative to the project folder
	Kind        string // service | job
	Source      string
	Autostart   bool
	Autorestart bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProcessRepo stores process definitions.
type ProcessRepo interface {
	Create(ctx context.Context, p Process) (Process, error)
	Update(ctx context.Context, p Process) error
	Get(ctx context.Context, id string) (Process, error)
	List(ctx context.Context, projectID string) ([]Process, error) // "" = all projects
	Delete(ctx context.Context, id string) error
}

// Monitor is a health check of a project.
type Monitor struct {
	ID            string
	ProjectID     string
	Name          string
	Type          string // http | tcp | heartbeat | process | container
	Target        string
	Config        MonitorConfig
	IntervalS     int
	Enabled       bool
	AIEnabled     bool
	AIBudgetUSD   float64
	Token         string
	Status        string // pending | up | down
	Fails         int
	LastCheckedAt *time.Time
	LastChangeAt  *time.Time
	LastLatencyMS int
	LastMessage   string
	LastPingAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// MonitorConfig holds type-specific options.
type MonitorConfig struct {
	ExpectStatus string `json:"expect_status,omitempty"` // "200-399" (http)
	Keyword      string `json:"keyword,omitempty"`       // must appear in the body (http)
	TimeoutMS    int    `json:"timeout_ms,omitempty"`
	File         string `json:"file,omitempty"` // compose file (container)
}

// MonitorCheck is one check result.
type MonitorCheck struct {
	MonitorID string
	At        time.Time
	OK        bool
	LatencyMS int
	Message   string
}

// MonitorEvent is a status change.
type MonitorEvent struct {
	ID             string
	MonitorID      string
	ProjectID      string
	Kind           string // up | down
	Message        string
	Analysis       string
	AnalysisStatus string
	CostUSD        float64
	At             time.Time
}

// MonitorRepo stores monitors, their checks and events.
type MonitorRepo interface {
	Create(ctx context.Context, m Monitor) (Monitor, error)
	Update(ctx context.Context, m Monitor) error     // definition fields
	SaveStatus(ctx context.Context, m Monitor) error // status fields
	Get(ctx context.Context, id string) (Monitor, error)
	GetByToken(ctx context.Context, token string) (Monitor, error)
	List(ctx context.Context, projectID string) ([]Monitor, error) // "" = all
	Delete(ctx context.Context, id string) error

	AddCheck(ctx context.Context, c MonitorCheck) error
	Checks(ctx context.Context, monitorID string, since time.Time) ([]MonitorCheck, error)
	PruneChecks(ctx context.Context, before time.Time) error

	AddEvent(ctx context.Context, e MonitorEvent) (MonitorEvent, error)
	UpdateEvent(ctx context.Context, e MonitorEvent) error
	Events(ctx context.Context, projectID string, limit int) ([]MonitorEvent, error) // "" = all
	AICostSince(ctx context.Context, monitorID string, since time.Time) (float64, error)
}

// Action is an operation an agent proposed; it runs only after approval.
type Action struct {
	ID             string
	ProjectID      string
	ConversationID string
	MessageID      string
	TaskID         string
	RunRef         string
	Kind           string
	Target         string
	TargetID       string
	Reason         string
	Args           ActionArgs
	Status         string // pending | done | failed | rejected
	Detail         string
	ProposedBy     string
	DecidedBy      string
	DecidedAt      *time.Time
	CreatedAt      time.Time
}

// ActionArgs are extra inputs of an action.
type ActionArgs struct {
	Message string   `json:"message,omitempty"` // git commit
	Files   []string `json:"files,omitempty"`   // git commit
	Branch  string   `json:"branch,omitempty"`  // git branch
}

// ActionRepo stores proposed actions.
type ActionRepo interface {
	Create(ctx context.Context, a Action) (Action, error)
	Update(ctx context.Context, a Action) error
	Get(ctx context.Context, id string) (Action, error)
	// List filters by conversation, task or run (the first non-empty one).
	List(ctx context.Context, conversationID, taskID, runRef string) ([]Action, error)
}
