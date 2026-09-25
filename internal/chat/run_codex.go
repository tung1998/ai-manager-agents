package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// codexRunner drives `codex exec` in a read-only sandbox. Codex keeps no
// session here, so earlier turns are passed as a transcript.
type codexRunner struct{}

func (codexRunner) Run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error) {
	bin := firstNonEmpty(req.Bin, "codex")
	args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only", "--cd", req.WorkDir}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	prompt, images := codexPrompt(req.Prompt, req.Attachments)
	args = append(args, "-")
	if len(images) > 0 {
		args = append(args, "--image", strings.Join(images, ","))
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = strings.NewReader(req.System + "\n\n" + transcript(req.History, prompt))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return RunResult{}, fmt.Errorf("không chạy được %s: %w", bin, err)
	}
	res := RunResult{}
	res.Usage.Model = req.Model
	var answer []string
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var ev struct {
			Type string `json:"type"`
			Item struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Command string `json:"command"`
			} `json:"item"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "item.completed" && ev.Item.Type == "agent_message" && ev.Item.Text != "":
			if len(answer) > 0 {
				emit(Event{Type: "text", Text: "\n\n"})
			}
			answer = append(answer, ev.Item.Text)
			emit(Event{Type: "text", Text: ev.Item.Text})
		case ev.Type == "item.started" && ev.Item.Type == "command_execution":
			in, _ := json.Marshal(map[string]string{"command": ev.Item.Command})
			tc := storage.ToolCall{Name: "command_execution", Summary: toolSummary("command_execution", in)}
			res.Tools = append(res.Tools, tc)
			emit(Event{Type: "tool", Tool: &tc})
		}
		if ev.Usage.InputTokens+ev.Usage.OutputTokens > 0 {
			res.Usage.InputTokens, res.Usage.OutputTokens = ev.Usage.InputTokens, ev.Usage.OutputTokens
		}
	}
	werr := cmd.Wait()
	res.Usage.DurationMS = time.Since(start).Milliseconds()
	res.Text = strings.TrimSpace(strings.Join(answer, "\n\n"))
	if werr != nil && res.Text == "" {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = werr.Error()
		}
		return res, fmt.Errorf("codex: %s", truncate(msg, 500))
	}
	return res, nil
}
