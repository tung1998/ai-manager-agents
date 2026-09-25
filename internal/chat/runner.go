package chat

import (
	"context"

	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Event is streamed to the dashboard while an agent answers.
type Event struct {
	Seq     int               `json:"seq"`
	Type    string            `json:"type"` // text | tool | status | patch | done | error
	Text    string            `json:"text,omitempty"`
	Tool    *storage.ToolCall `json:"tool,omitempty"`
	Patch   *PatchDTO         `json:"patch,omitempty"`
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
