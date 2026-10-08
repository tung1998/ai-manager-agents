package chat

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
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
		Error          string          `json:"error"`
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

func (r antigravityRunner) Run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error) {
	res, stepErr, err := r.run(ctx, req, emit)
	// a conversation agy no longer has: start over with the transcript
	if err != nil && req.SessionID != "" && res.Text == "" && stepErr == "" {
		req.SessionID = ""
		res, stepErr, err = r.run(ctx, req, emit)
	}
	// agy ends a turn without an answer when a step fails (a command the
	// mode denies): asked once more in its conversation to answer with what
	// it has, else the turn fails (not an empty answer taken as done)
	if res.Text == "" && res.SessionID != "" && ctx.Err() == nil && (err == nil || stepErr != "") {
		more := req
		more.SessionID, more.Attachments = res.SessionID, nil
		more.Prompt = "You have not answered yet. Write your final answer to the request above with what you have; do not retry the step that just failed."
		if stepErr != "" {
			more.Prompt = "The last step failed: " + truncate(stepErr, 300) + "\n" + more.Prompt
		}
		again, againErr, err2 := r.run(ctx, more, emit)
		again.Tools = append(res.Tools, again.Tools...)
		again.Usage.InputTokens += res.Usage.InputTokens
		again.Usage.OutputTokens += res.Usage.OutputTokens
		again.Usage.DurationMS += res.Usage.DurationMS
		res, err = again, err2
		stepErr = cmp.Or(againErr, stepErr)
	}
	if err == nil && res.Text == "" {
		msg := "agy kết thúc lượt mà không trả lời"
		if stepErr != "" {
			msg += ": " + truncate(stepErr, 300)
		}
		err = errors.New(msg)
	}
	return res, err
}

// run is one agy call; stepErr is the last step that failed (it may end the turn).
func (antigravityRunner) run(ctx context.Context, req RunRequest, emit func(Event)) (res RunResult, stepErr string, _ error) {
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
	// the model names its level, or the default model takes up to high (ADR-106)
	if e := storage.FitEffort(storage.ProviderAntigravityCLI, req.Model, req.Effort); e != "" {
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
		return RunResult{}, "", err
	}
	if err := cmd.Start(); err != nil {
		return RunResult{}, "", fmt.Errorf("không chạy được %s: %w", bin, err)
	}
	defer proctrack.Track(ctx, cmd.Process.Pid)()
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
			if st.State == "ERROR" {
				stepErr = cmp.Or(st.Error, st.StepType+" lỗi")
			}
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
		return res, stepErr, fmt.Errorf("agy: %s", truncate(msg, 500))
	}
	return res, stepErr, nil
}

// agyPrompt is the turn's text: instructions and the transcript for a new
// conversation, only the new message for a resumed one. agy reads text only,
// so images are named by path and their folders added to the workspace.
func agyPrompt(req RunRequest) (string, []string) {
	prompt, images := codexPrompt(req.Prompt, req.Attachments)
	var dirs []string
	for _, p := range images {
		prompt += "\n(Attached image: " + p + ")"
		if d := filepath.Dir(p); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	if req.SessionID == "" {
		prompt = req.System + "\n\n" + transcript(req.History, prompt)
	}
	return prompt, dirs
}
