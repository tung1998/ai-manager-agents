package chat

import (
	"bitbucket.org/senprints/agent-office/internal/mcpserver"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// claudeRunner drives Claude Code headless, isolated from the user's personal
// setup (no user settings, hooks, plugins, MCP servers or skills) and limited
// to read-only tools. Sessions are resumed across turns.
type claudeRunner struct{}

var claudeReadTools = []string{"Read", "Glob", "Grep"}

func (claudeRunner) args(req RunRequest, resume bool) []string {
	a := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--setting-sources", "project,local", "--strict-mcp-config", "--disable-slash-commands",
		"--tools", strings.Join(claudeReadTools, ","),
		"--allowedTools", strings.Join(claudeReadTools, " "),
		"--disallowedTools", "Read(./.env) Read(./.env.*) Read(**/.env) Read(**/*.pem) Read(**/*.key)",
	}
	if req.Model != "" {
		a = append(a, "--model", req.Model)
	}
	if req.System != "" {
		a = append(a, "--append-system-prompt", req.System)
	}
	if resume && req.SessionID != "" {
		a = append(a, "--resume", req.SessionID)
	}
	return a
}

func (r claudeRunner) Run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error) {
	res, err := r.run(ctx, req, emit, true)
	// A session the CLI no longer has: start over with the transcript instead.
	if err != nil && req.SessionID != "" && strings.Contains(strings.ToLower(err.Error()), "no conversation found") {
		req.Prompt = transcript(req.History, req.Prompt)
		req.SessionID = ""
		return r.run(ctx, req, emit, false)
	}
	return res, err
}

func (r claudeRunner) run(ctx context.Context, req RunRequest, emit func(Event), resume bool) (RunResult, error) {
	bin := req.Bin
	if bin == "" {
		bin = "claude"
	}
	start := time.Now()
	prompt, dirs := claudePrompt(req.Prompt, req.Attachments)
	if !resume || req.SessionID == "" {
		prompt = transcript(req.History, prompt)
	}
	args := r.args(req, resume)
	for _, d := range dirs {
		args = append(args, "--add-dir", d)
	}
	if req.Office != nil {
		// the office MCP server; the token goes in a private temp file, not argv
		cfg, err := writeMCPConfig(req.Office)
		if err != nil {
			return RunResult{}, err
		}
		defer os.Remove(cfg)
		args = append(args, "--mcp-config", cfg)
		for i := range args {
			if args[i] == "--allowedTools" && i+1 < len(args) {
				args[i+1] += " mcp__" + mcpserver.ServerName
			}
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = strings.NewReader(prompt)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return RunResult{}, fmt.Errorf("không tìm thấy lệnh %q", bin)
		}
		return RunResult{}, err
	}
	var (
		res      RunResult
		resErr   error
		gotFinal bool
	)
	pending := map[string]int{} // tool_use id → index in res.Tools
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var ev struct {
			Type      string `json:"type"`
			Subtype   string `json:"subtype"`
			SessionID string `json:"session_id"`
			Event     struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
			Message struct {
				Content []struct {
					Type      string          `json:"type"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input"`
					ToolUseID string          `json:"tool_use_id"`
					IsError   bool            `json:"is_error"`
				} `json:"content"`
			} `json:"message"`
			Result       string  `json:"result"`
			IsError      bool    `json:"is_error"`
			TotalCostUSD float64 `json:"total_cost_usd"`
			Usage        struct {
				InputTokens         int `json:"input_tokens"`
				OutputTokens        int `json:"output_tokens"`
				CacheReadTokens     int `json:"cache_read_input_tokens"`
				CacheCreationTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.SessionID != "" {
			res.SessionID = ev.SessionID
		}
		switch ev.Type {
		case "stream_event":
			if ev.Event.Type == "content_block_delta" && ev.Event.Delta.Type == "text_delta" && ev.Event.Delta.Text != "" {
				emit(Event{Type: "text", Text: ev.Event.Delta.Text})
			}
		case "assistant":
			for _, c := range ev.Message.Content {
				if c.Type == "tool_use" {
					tc := storage.ToolCall{Name: c.Name, Summary: toolSummary(c.Name, c.Input)}
					pending[c.ID] = len(res.Tools)
					res.Tools = append(res.Tools, tc)
					emit(Event{Type: "tool", Tool: &tc})
				}
			}
		case "user":
			for _, c := range ev.Message.Content {
				if c.Type == "tool_result" && c.IsError {
					if i, ok := pending[c.ToolUseID]; ok {
						res.Tools[i].Error = true
					}
				}
			}
		case "result":
			gotFinal = true
			res.Text = ev.Result
			res.Usage.InputTokens = ev.Usage.InputTokens + ev.Usage.CacheReadTokens + ev.Usage.CacheCreationTokens
			res.Usage.OutputTokens = ev.Usage.OutputTokens
			res.Usage.CostUSD = ev.TotalCostUSD
			res.Usage.Model = req.Model
			if ev.IsError || ev.Subtype != "success" {
				resErr = fmt.Errorf("claude: %s", firstNonEmpty(ev.Result, ev.Subtype))
			}
		}
	}
	werr := cmd.Wait()
	res.Usage.DurationMS = time.Since(start).Milliseconds()
	switch {
	case resErr != nil:
		return res, resErr
	case ctx.Err() != nil:
		return res, ctx.Err()
	case werr != nil && !gotFinal:
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = werr.Error()
		}
		return res, fmt.Errorf("claude: %s", truncate(msg, 500))
	}
	return res, nil
}

// toolSummary turns a tool input into a short label for the chat ("Đọc src/app.vue").
func toolSummary(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := in[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch strings.ToLower(strings.TrimPrefix(name, "mcp__"+mcpserver.ServerName+"__")) {
	case "ops_overview":
		return "Xem tổng quan vận hành"
	case "process_logs":
		return "Đọc log tiến trình " + str("name")
	case "container_logs":
		return "Đọc log container " + str("service")
	case "monitor_detail":
		return "Xem giám sát " + str("name")
	case "git_status":
		return "Xem git status"
	case "git_diff":
		return "Xem git diff"
	case "git_log":
		return "Xem git log"
	case "run_command":
		return "Chạy lệnh " + str("command")
	case "propose_action":
		return "Đề xuất " + strings.ReplaceAll(str("action"), "_", " ") + " " + str("target")
	case "read", "read_file":
		return "Đọc " + str("file_path", "path")
	case "glob":
		return "Tìm file " + str("pattern")
	case "grep", "search", "search_text":
		return "Tìm \"" + str("pattern") + "\""
	case "list_dir", "ls":
		p := str("path")
		if p == "" {
			p = "."
		}
		return "Xem thư mục " + p
	case "bash", "command_execution":
		return "Chạy " + str("command")
	}
	return name
}

// transcript folds earlier turns into one prompt for runtimes without sessions.
func transcript(history []HistoryItem, prompt string) string {
	if len(history) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString("Cuộc trò chuyện trước đó:\n")
	for _, h := range history {
		who := "Người dùng"
		if h.Role == "assistant" {
			who = "Bạn"
		}
		fmt.Fprintf(&b, "\n[%s]\n%s\n", who, truncate(h.Content, 6000))
	}
	b.WriteString("\n---\nTin nhắn mới của người dùng:\n")
	b.WriteString(prompt)
	return b.String()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func writeMCPConfig(o *OfficeAccess) (string, error) {
	raw, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		mcpserver.ServerName: map[string]any{"type": "http", "url": o.MCPURL, "headers": map[string]string{"Authorization": "Bearer " + o.Token}},
	}})
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "office-mcp-*.json") // created 0600
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(raw); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
