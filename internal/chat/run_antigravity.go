package chat

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/proctrack"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// antigravityRunner drives Antigravity CLI (`agy`) in print mode: the prompt
// on stdin, stream-json out, and the chat's conversation resumed with
// --conversation, like the CLI itself (ADR-106).
type antigravityRunner struct{}

// agyEvent is one stream-json line: {"event": "<name>", "<name>": {...}}.
type agyEvent struct {
	Event string `json:"event"`
	Init  struct {
		Model string `json:"model"`
	} `json:"init"`
	StepUpdate struct {
		ConversationID string          `json:"conversation_id"`
		StepIndex      int             `json:"step_index"`
		State          string          `json:"state"`
		StepType       string          `json:"step_type"`
		TextDelta      string          `json:"text_delta"`
		ToolName       string          `json:"tool_name"`
		ToolInfo       json.RawMessage `json:"tool_info"`
	} `json:"step_update"`
	Result struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
		Usage          struct {
			InputTokens     int `json:"input_tokens"`
			OutputTokens    int `json:"output_tokens"`
			ThinkingTokens  int `json:"thinking_tokens"`
			CacheReadTokens int `json:"cache_read_tokens"`
		} `json:"usage"`
	} `json:"result"`
}

// agyEffort maps office's effort onto agy's (low|medium|high|xhigh|max).
func agyEffort(e string) string {
	if slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, e) {
		return e
	}
	return ""
}

func (r antigravityRunner) Run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error) {
	res, err := r.run(ctx, req, emit)
	// a conversation agy no longer has: start over with the transcript
	if err != nil && req.SessionID != "" && res.Text == "" {
		req.SessionID = ""
		return r.run(ctx, req, emit)
	}
	return res, err
}

func (antigravityRunner) run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error) {
	bin := firstNonEmpty(req.Bin, "agy")
	args := []string{"--output-format", "stream-json", "--disable-slash-commands"}
	switch {
	case req.FullAccess:
		args = append(args, "--dangerously-skip-permissions")
	case req.Write:
		args = append(args, "--mode", "accept-edits") // edits in its folder; commands stay soft-denied
	default:
		args = append(args, "--mode", "plan") // reads only
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if e := agyEffort(req.Effort); e != "" {
		args = append(args, "--effort", e)
	}
	if req.SessionID != "" {
		args = append(args, "--conversation", req.SessionID)
	}
	prompt, imageDirs := agyPrompt(req)
	for _, d := range append(slices.Clone(req.ExtraDirs), imageDirs...) {
		if d = strings.TrimSpace(d); d != "" {
			args = append(args, "--add-dir", d)
		}
	}
	start := time.Now()
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
		return RunResult{}, fmt.Errorf("không chạy được %s: %w", bin, err)
	}
	defer proctrack.Track(ctx, cmd.Process.Pid)()
	res := RunResult{}
	res.Usage.Model = req.Model
	var answer strings.Builder
	var failure, final string
	seenTool := map[int]bool{}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var ev agyEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "init":
			res.Usage.Model = cmp.Or(ev.Init.Model, res.Usage.Model)
		case "step_update":
			st := ev.StepUpdate
			res.SessionID = cmp.Or(st.ConversationID, res.SessionID)
			switch {
			case st.StepType == "agent_response" && st.TextDelta != "":
				answer.WriteString(st.TextDelta)
				emit(Event{Type: "text", Text: st.TextDelta})
			case st.ToolName != "" && !seenTool[st.StepIndex]:
				seenTool[st.StepIndex] = true
				tc := storage.ToolCall{Name: st.ToolName, Summary: toolSummary(st.ToolName, st.ToolInfo)}
				res.Tools = append(res.Tools, tc)
				emit(Event{Type: "tool", Tool: &tc})
			}
		case "result":
			rs := ev.Result
			res.SessionID = cmp.Or(rs.ConversationID, res.SessionID)
			u := rs.Usage
			res.Usage.InputTokens += u.InputTokens + u.CacheReadTokens
			res.Usage.OutputTokens += u.OutputTokens + u.ThinkingTokens
			final = rs.Response
			if rs.Status != "SUCCESS" {
				failure = cmp.Or(rs.Error, rs.Status)
			}
		}
	}
	werr := cmd.Wait()
	res.Usage.DurationMS = time.Since(start).Milliseconds()
	res.Text = strings.TrimSpace(answer.String())
	if res.Text == "" && final != "" { // no deltas streamed: the result has it all
		res.Text = strings.TrimSpace(final)
		emit(Event{Type: "text", Text: res.Text})
	}
	if res.Text == "" && (werr != nil || failure != "") {
		msg := cmp.Or(failure, strings.TrimSpace(stderr.String()))
		if msg == "" {
			msg = werr.Error()
		}
		if strings.Contains(msg, "authentication") {
			msg += " — Antigravity CLI chưa đăng nhập: bấm Đăng nhập ở kết nối AI"
		}
		return res, fmt.Errorf("agy: %s", truncate(msg, 500))
	}
	return res, nil
}

// agyPrompt is the turn's text: instructions and the transcript for a new
// conversation, only the new message for a resumed one. agy reads text only,
// so images are named by path and their folders added to the workspace.
func agyPrompt(req RunRequest) (string, []string) {
	prompt, images := codexPrompt(req.Prompt, req.Attachments)
	var dirs []string
	for _, p := range images {
		prompt += "\n(Ảnh đính kèm: " + p + ")" // i18n-ignore
		if d := filepath.Dir(p); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	if req.SessionID == "" {
		prompt = req.System + "\n\n" + transcript(req.History, prompt)
	}
	return prompt, dirs
}
