package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// run executes bin with args, feeding stdin; stderr is kept for error messages.
func run(ctx context.Context, bin string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("không tìm thấy lệnh %q trong PATH", bin)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		return "", fmt.Errorf("%s: %v: %s", bin, err, msg)
	}
	return stdout.String(), nil
}

// ---- Claude Code ----

type claudeCLI struct{ bin string }

func (c *claudeCLI) Check(ctx context.Context) (CheckResult, error) {
	out, err := run(ctx, c.bin, "", "--version")
	if err != nil {
		return CheckResult{}, err
	}
	v := strings.TrimSpace(out)
	return CheckResult{Version: v, Models: DefaultModelsClaude, Detail: v}, nil
}

// DefaultModelsClaude are offered for CLI providers, which cannot list models.
var DefaultModelsClaude = []string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5", "claude-haiku-4-5"}

func (c *claudeCLI) Complete(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	args := []string{"-p", "--output-format", "json", "--max-turns", "1"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.System != "" {
		args = append(args, "--append-system-prompt", req.System)
	}
	// The prompt goes on stdin: argv is visible in ps and has a size limit.
	out, err := run(ctx, c.bin, req.Prompt, args...)
	if err != nil {
		return Result{}, err
	}
	var res struct {
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
	if err := json.Unmarshal([]byte(lastJSONObject(out)), &res); err != nil {
		return Result{}, fmt.Errorf("claude: không đọc được output JSON: %w", err)
	}
	if res.IsError {
		return Result{}, fmt.Errorf("claude: %s", res.Result)
	}
	// Claude Code reports cached prompt tokens separately; count the whole prompt.
	in := res.Usage.InputTokens + res.Usage.CacheReadTokens + res.Usage.CacheCreationTokens
	return Result{Text: res.Result, Model: req.Model, InputTokens: in, OutputTokens: res.Usage.OutputTokens,
		CostUSD: res.TotalCostUSD, DurationMS: time.Since(start).Milliseconds()}, nil
}

// ---- Codex ----

type codexCLI struct{ bin string }

func (c *codexCLI) Check(ctx context.Context) (CheckResult, error) {
	out, err := run(ctx, c.bin, "", "--version")
	if err != nil {
		return CheckResult{}, err
	}
	v := strings.TrimSpace(out)
	return CheckResult{Version: v, Detail: v}, nil
}

func (c *codexCLI) Complete(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only"}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	prompt := req.Prompt
	if req.System != "" {
		prompt = req.System + "\n\n" + req.Prompt
	}
	args = append(args, "-") // read the prompt from stdin
	out, err := run(ctx, c.bin, prompt, args...)
	if err != nil {
		return Result{}, err
	}
	res := Result{Model: req.Model, DurationMS: time.Since(start).Milliseconds()}
	// JSONL events; the event shape has changed across codex versions, so read
	// both the item.* and msg.* styles and keep the last agent message.
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var ev struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Msg struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"msg"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch {
		case ev.Item.Type == "agent_message" && ev.Item.Text != "":
			res.Text = ev.Item.Text
		case ev.Msg.Type == "agent_message" && ev.Msg.Message != "":
			res.Text = ev.Msg.Message
		}
		if ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0 {
			res.InputTokens, res.OutputTokens = ev.Usage.InputTokens, ev.Usage.OutputTokens
		}
	}
	if res.Text == "" {
		return Result{}, errors.New("codex: không có câu trả lời trong output")
	}
	return res, nil
}

// lastJSONObject returns the last line that looks like a JSON object, since
// CLIs may print warnings before the result.
func lastJSONObject(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "{") {
			return l
		}
	}
	return strings.TrimSpace(out)
}
