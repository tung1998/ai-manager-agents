package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/mcpserver"
	"bitbucket.org/senprints/agent-office/internal/proctrack"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// codexRunner drives `codex exec` in a read-only sandbox and goes on in the
// chat's session with `codex exec resume`, like the CLI itself.
type codexRunner struct{}

// codexTokenEnv carries the run's token to Codex's MCP client.
const codexTokenEnv = "OFFICE_MCP_TOKEN"

// codexMCPArgs gives Codex the office MCP server and the gateway's servers
// (streamable HTTP, the run's token from codexTokenEnv; ADR-093).
func codexMCPArgs(req RunRequest) []string {
	if req.Office == nil || req.NoTools || req.Office.MCPURL == "" {
		return nil
	}
	q := strconv.Quote
	out := []string{"-c", "experimental_use_rmcp_client=true"}
	add := func(name, url string, client bool) {
		key := "mcp_servers." + name
		out = append(out, "-c", key+".url="+q(url), "-c", key+".bearer_token_env_var="+q(codexTokenEnv))
		if client {
			out = append(out, "-c", key+".http_headers={"+q(ClientHeader)+"="+q("codex")+"}")
		}
	}
	add(mcpserver.ServerName, req.Office.MCPURL, false)
	for _, n := range req.Office.Gateway {
		add(n, req.Office.MCPURL+"/s/"+n, true)
	}
	return out
}

// codexEffortArgs asks Codex to think this hard; its highest is xhigh.
func codexEffortArgs(e string) []string {
	if e == "" {
		return nil
	}
	if e == "max" {
		e = "xhigh"
	}
	return []string{"-c", "model_reasoning_effort=" + strconv.Quote(e)}
}

func (r codexRunner) Run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error) {
	res, err := r.run(ctx, req, emit)
	// a session Codex no longer has: start over with the transcript
	if err != nil && req.SessionID != "" && res.Text == "" {
		req.SessionID = ""
		return r.run(ctx, req, emit)
	}
	return res, err
}

func (codexRunner) run(ctx context.Context, req RunRequest, emit func(Event)) (RunResult, error) {
	bin := firstNonEmpty(req.Bin, "codex")
	sandbox := "read-only"
	if req.Write {
		sandbox = "workspace-write" // its worktree, or the project in direct mode
	}
	args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", sandbox, "--cd", req.WorkDir}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	args = append(args, codexEffortArgs(req.Effort)...)
	args = append(args, codexMCPArgs(req)...)
	prompt, images := codexPrompt(req.Prompt, req.Attachments)
	if req.SessionID != "" {
		// exec's options stay before the subcommand
		args = append(args, "resume", req.SessionID)
	} else {
		// a resumed session already has the instructions and earlier turns
		prompt = req.System + "\n\n" + transcript(req.History, prompt)
	}
	args = append(args, "-") // the prompt from stdin
	if len(images) > 0 {
		// last: --image takes several values and would swallow what follows
		args = append(args, "--image", strings.Join(images, ","))
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = req.WorkDir
	if req.Office != nil && !req.NoTools {
		// the run's token reaches Codex through its environment, not argv
		cmd.Env = append(os.Environ(), codexTokenEnv+"="+req.Office.Token)
	}
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
	var answer []string
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Command string `json:"command"`
				Server  string `json:"server"`
				Tool    string `json:"tool"`
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
		case ev.Type == "thread.started" && ev.ThreadID != "":
			res.SessionID = ev.ThreadID
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
		case ev.Type == "item.started" && ev.Item.Type == "mcp_tool_call":
			tc := storage.ToolCall{Name: "mcp__" + ev.Item.Server + "__" + ev.Item.Tool}
			res.Tools = append(res.Tools, tc)
			emit(Event{Type: "tool", Tool: &tc})
		}
		if ev.Usage.InputTokens+ev.Usage.OutputTokens > 0 {
			res.Usage.InputTokens, res.Usage.OutputTokens = ev.Usage.InputTokens, ev.Usage.OutputTokens
		}
	}
	var scanErr error
	if serr := sc.Err(); serr != nil {
		scanErr = fmt.Errorf("codex: đọc output lỗi: %w", serr)
	}
	io.Copy(io.Discard, out)
	werr := cmd.Wait()
	res.Usage.DurationMS = time.Since(start).Milliseconds()
	res.Text = strings.TrimSpace(strings.Join(answer, "\n\n"))
	if scanErr != nil {
		return res, scanErr
	}
	if werr != nil && res.Text == "" {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = werr.Error()
		}
		return res, fmt.Errorf("codex: %s", truncate(msg, 500))
	}
	return res, nil
}
