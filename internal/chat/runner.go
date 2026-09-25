package chat

import (
	"bitbucket.org/senprints/agent-office/internal/attach"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"context"

	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Event is streamed to the dashboard while an agent answers.
type Event struct {
	Seq     int               `json:"seq"`
	Type    string            `json:"type"` // text | tool | status | patch | action | done | error
	Text    string            `json:"text,omitempty"`
	Tool    *storage.ToolCall `json:"tool,omitempty"`
	Patch   *PatchDTO         `json:"patch,omitempty"`
	Action  *ActionDTO        `json:"action,omitempty"`
	Message *MessageDTO       `json:"message,omitempty"`
}

// HistoryItem is a previous turn given to runtimes that do not keep sessions.
type HistoryItem struct {
	Role    string // user | assistant
	Content string
}

// RunRequest is one agent turn.
type RunRequest struct {
	Provider  storage.Provider
	APIKey    string
	Bin       string // CLI binary for CLI providers
	Model     string
	System    string
	History   []HistoryItem
	Prompt    string
	WorkDir   string
	SessionID string
	// Files attached to this turn's prompt (see attachments.go).
	Attachments []attach.File
	// Office tools (build/run/monitoring info) for this run; nil = none.
	Office *OfficeAccess
}

// OfficeAccess lets one run read the project's operations data: Claude Code
// through the office MCP server, API agents through the same tools directly.
type OfficeAccess struct {
	MCPURL string
	Token  string
	Scope  officetools.Scope
	Tools  *officetools.Toolbox
}

// RunResult is what the runtime produced.
type RunResult struct {
	Text      string
	SessionID string
	Usage     llm.Result
	Tools     []storage.ToolCall
}

// Runner executes a turn on one kind of provider.
type Runner interface {
	Run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error)
}

func runnerFor(kind storage.ProviderKind) Runner {
	switch kind {
	case storage.ProviderClaudeCLI:
		return claudeRunner{}
	case storage.ProviderCodexCLI:
		return codexRunner{}
	case storage.ProviderAnthropic:
		return anthropicRunner{}
	default:
		return openAIRunner{official: kind == storage.ProviderOpenAI}
	}
}
