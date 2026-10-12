package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Anthropic: every round keeps asking for a tool (read_file) and never stops
// on its own; after maxToolRounds the runner must not report success as if
// the turn finished cleanly.
func TestAnthropicRunExhaustedNoText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"claude","stop_reason":"tool_use","content":[
			{"type":"tool_use","id":"t1","name":"read_file","input":{"path":"go.mod"}}
		]}`))
	}))
	defer srv.Close()

	req := RunRequest{Provider: storage.Provider{BaseURL: srv.URL}, Model: "claude", WorkDir: t.TempDir()}
	res, err := (anthropicRunner{}).Run(context.Background(), req, func(Event) {})
	if err == nil {
		t.Fatalf("expected error when loop exhausts with no text, got res=%+v", res)
	}
}

// Same exhaustion, but the model also wrote some text along the way: the
// turn must still be flagged, not silently saved as a normal finish.
func TestAnthropicRunExhaustedWithTextGetsNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"claude","stop_reason":"tool_use","content":[
			{"type":"text","text":"working on it"},
			{"type":"tool_use","id":"t1","name":"read_file","input":{"path":"go.mod"}}
		]}`))
	}))
	defer srv.Close()

	req := RunRequest{Provider: storage.Provider{BaseURL: srv.URL}, Model: "claude", WorkDir: t.TempDir()}
	res, err := (anthropicRunner{}).Run(context.Background(), req, func(Event) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "hết") && !strings.Contains(res.Text, "vòng gọi công cụ") {
		t.Fatalf("expected an exhaustion note in res.Text, got %q", res.Text)
	}
}

// A clean finish (no tool_use on the last round) must not be flagged.
func TestAnthropicRunCleanFinishNoNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"claude","stop_reason":"end_turn","content":[
			{"type":"text","text":"all done"}
		]}`))
	}))
	defer srv.Close()

	req := RunRequest{Provider: storage.Provider{BaseURL: srv.URL}, Model: "claude", WorkDir: t.TempDir()}
	res, err := (anthropicRunner{}).Run(context.Background(), req, func(Event) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "all done" {
		t.Fatalf("expected clean text, got %q", res.Text)
	}
}

func TestOpenAIRunExhaustedNoText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"gpt","choices":[{"message":{"content":"","tool_calls":[
			{"id":"t1","function":{"name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}
		]}}]}`))
	}))
	defer srv.Close()

	req := RunRequest{Provider: storage.Provider{BaseURL: srv.URL}, Model: "gpt", WorkDir: t.TempDir()}
	res, err := (openAIRunner{}).Run(context.Background(), req, func(Event) {})
	if err == nil {
		t.Fatalf("expected error when loop exhausts with no text, got res=%+v", res)
	}
}

func TestOpenAIRunExhaustedWithTextGetsNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"gpt","choices":[{"message":{"content":"working on it","tool_calls":[
			{"id":"t1","function":{"name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}
		]}}]}`))
	}))
	defer srv.Close()

	req := RunRequest{Provider: storage.Provider{BaseURL: srv.URL}, Model: "gpt", WorkDir: t.TempDir()}
	res, err := (openAIRunner{}).Run(context.Background(), req, func(Event) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "vòng gọi công cụ") {
		t.Fatalf("expected an exhaustion note in res.Text, got %q", res.Text)
	}
}

func TestOpenAIRunCleanFinishNoNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"gpt","choices":[{"message":{"content":"all done"}}]}`))
	}))
	defer srv.Close()

	req := RunRequest{Provider: storage.Provider{BaseURL: srv.URL}, Model: "gpt", WorkDir: t.TempDir()}
	res, err := (openAIRunner{}).Run(context.Background(), req, func(Event) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "all done" {
		t.Fatalf("expected clean text, got %q", res.Text)
	}
}
