package llm

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// run executes bin with args, feeding stdin; stderr is kept for error messages.
func run(ctx context.Context, bin string, stdin string, args ...string) (string, error) {
	return runEnv(ctx, bin, stdin, nil, args...)
}

// runEnv is run with extra environment variables.
func runEnv(ctx context.Context, bin string, stdin string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
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
		return stdout.String(), &cliError{msg: fmt.Sprintf("%s: %v: %s", bin, err, cliFailure(stdout.String(), stderr.String())), stderr: stderr.String()}
	}
	return stdout.String(), nil
}

// cliError keeps a failed run's whole stderr for CLIs whose reason is not
// at the end (Gemini prints it first, then a stack trace).
type cliError struct{ msg, stderr string }

func (e *cliError) Error() string { return e.msg }

// cliFailure turns a failed run's output into one readable line. CLIs print
// their result as JSON whose reason (result/subtype/errors) sits at the END of
// a long object, so a plain prefix cut would throw away the only useful part.
func cliFailure(stdout, stderr string) string {
	var res struct {
		Result  string   `json:"result"`
		Subtype string   `json:"subtype"`
		Errors  []string `json:"errors"`
		Error   string   `json:"error"`
	}
	if j := lastJSONObject(stdout); j != "" && json.Unmarshal([]byte(j), &res) == nil {
		parts := []string{}
		for _, p := range append([]string{res.Result, res.Error}, res.Errors...) {
			if p = strings.TrimSpace(p); p != "" && !slices.Contains(parts, p) {
				parts = append(parts, p)
			}
		}
		if len(parts) == 0 && res.Subtype != "" {
			parts = append(parts, res.Subtype)
		}
		if len(parts) > 0 {
			return clip(strings.Join(parts, " · "), 400)
		}
	}
	if msg := strings.TrimSpace(stderr); msg != "" {
		return clip(msg, 400)
	}
	return clip(strings.TrimSpace(stdout), 400)
}

// clip keeps the tail of a long line: the reason a CLI failed is usually there.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// claudeLoggedIn reads `claude auth status --json`. ok is false only when the
// CLI answers and says it is signed out, so a version too old for the
// subcommand never turns a working setup into a sign-in prompt.
func claudeLoggedIn(ctx context.Context, bin string) (ok bool, account string) {
	out, err := run(ctx, bin, "", "auth", "status", "--json")
	if err != nil && strings.TrimSpace(out) == "" {
		return true, ""
	}
	var st struct {
		LoggedIn bool   `json:"loggedIn"`
		Email    string `json:"email"`
		OrgName  string `json:"orgName"`
	}
	if json.Unmarshal([]byte(lastJSONObject(out)), &st) != nil {
		return true, ""
	}
	account = st.Email
	if st.OrgName != "" && account != "" {
		account += " · " + st.OrgName
	}
	return st.LoggedIn, account
}

// ---- Claude Code ----

type claudeCLI struct{ bin string }

func (c *claudeCLI) Check(ctx context.Context) (CheckResult, error) {
	out, err := run(ctx, c.bin, "", "--version")
	if err != nil {
		return CheckResult{}, err
	}
	v := strings.TrimSpace(out)
	// The binary answering says nothing about the account: an expired session
	// only shows up when a prompt is sent, which used to read as "connected".
	if ok, account := claudeLoggedIn(ctx, c.bin); !ok {
		return CheckResult{}, fmt.Errorf("%s · %w: hãy bấm Đăng nhập để ký lại phiên Claude Code%s", v, ErrNeedsLogin, orPrefix(" (tài khoản cũ: ", account, ")"))
	}
	return CheckResult{Version: v, Models: DefaultModelsClaude, Detail: v}, nil
}

func orPrefix(prefix, s, suffix string) string {
	if s == "" {
		return ""
	}
	return prefix + s + suffix
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
		// A signed-out CLI fails before any API call (zero tokens, no cost);
		// say so, so the dashboard can offer to sign in.
		if ok, _ := claudeLoggedIn(ctx, c.bin); !ok {
			return Result{}, fmt.Errorf("%w: phiên Claude Code đã hết hạn, hãy đăng nhập lại", ErrNeedsLogin)
		}
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
	if st, _ := run(ctx, c.bin, "", "login", "status"); strings.Contains(strings.ToLower(st), "not logged in") {
		return CheckResult{}, fmt.Errorf("%s · %w: hãy bấm Đăng nhập để ký lại phiên Codex", v, ErrNeedsLogin)
	}
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
	if err := sc.Err(); err != nil {
		return Result{}, fmt.Errorf("codex: đọc output lỗi: %w", err)
	}
	if res.Text == "" {
		return Result{}, errors.New("codex: không có câu trả lời trong output")
	}
	return res, nil
}

// ---- Gemini ----

type geminiCLI struct{ bin string }

// DefaultModelsGemini are Gemini CLI's model aliases (they follow its newest
// models) and a few pinned names; the CLI cannot list models.
var DefaultModelsGemini = []string{"auto", "pro", "flash", "flash-lite", "gemini-3.1-pro-preview", "gemini-3.5-flash", "gemini-2.5-pro", "gemini-2.5-flash"}

// GeminiSignedIn reports whether Gemini CLI has credentials: a Google sign-in
// cached in ~/.gemini, or an API key / Vertex / Code Assist setup in the
// environment or ~/.gemini/.env.
func GeminiSignedIn() bool {
	ok, _ := GeminiAccount()
	return ok
}

// GeminiAccount is GeminiSignedIn plus the Google account Gemini CLI signed
// in with ("" for a key from the environment or an unknown account).
func GeminiAccount() (bool, string) {
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_GCA", "GOOGLE_CLOUD_ACCESS_TOKEN"} {
		if os.Getenv(k) != "" {
			return true, ""
		}
	}
	home := os.Getenv("GEMINI_CLI_HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	dir := filepath.Join(home, ".gemini")
	if _, err := os.Stat(filepath.Join(dir, "oauth_creds.json")); err == nil {
		var acc struct {
			Active string `json:"active"`
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "google_accounts.json"))
		_ = json.Unmarshal(raw, &acc)
		return true, acc.Active
	}
	env, _ := os.ReadFile(filepath.Join(dir, ".env"))
	return geminiEnvKey.Match(env), ""
}

var geminiEnvKey = regexp.MustCompile(`(?m)^\s*(export\s+)?(GEMINI_API_KEY|GOOGLE_API_KEY)\s*=\s*\S`)

func (c *geminiCLI) Check(ctx context.Context) (CheckResult, error) {
	out, err := run(ctx, c.bin, "", "--version")
	if err != nil {
		return CheckResult{}, err
	}
	v := strings.TrimSpace(out)
	if !GeminiSignedIn() {
		return CheckResult{}, fmt.Errorf("%s · %w: hãy bấm Đăng nhập (tài khoản Google) hoặc đặt GEMINI_API_KEY", v, ErrNeedsLogin)
	}
	return CheckResult{Version: v, Models: DefaultModelsGemini, Detail: v}, nil
}

func (c *geminiCLI) Complete(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	// headless because stdin is not a terminal; default approval keeps it to reading
	args := []string{"--output-format", "json", "--approval-mode", "default"}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	prompt := req.Prompt
	if req.System != "" {
		prompt = req.System + "\n\n" + req.Prompt
	}
	out, err := runEnv(ctx, c.bin, prompt, []string{"GEMINI_CLI_TRUST_WORKSPACE=true"}, args...)
	var res struct {
		Response string `json:"response"`
		Error    *struct {
			Message string `json:"message"`
		} `json:"error"`
		Stats struct {
			Models map[string]struct {
				Tokens struct {
					Prompt     int `json:"prompt"`
					Candidates int `json:"candidates"`
				} `json:"tokens"`
			} `json:"models"`
		} `json:"stats"`
	}
	jerr := json.Unmarshal([]byte(lastJSONObject(out)), &res)
	if err != nil || jerr != nil || res.Error != nil {
		var ce *cliError
		if errors.As(err, &ce) && res.Error == nil {
			if msg := GeminiError(ce.stderr); msg != "" {
				return Result{}, fmt.Errorf("gemini: %s", msg)
			}
		}
		if !GeminiSignedIn() {
			return Result{}, fmt.Errorf("%w: Gemini CLI chưa đăng nhập", ErrNeedsLogin)
		}
		if res.Error != nil && res.Error.Message != "" {
			return Result{}, fmt.Errorf("gemini: %s", clip(res.Error.Message, 400))
		}
		if err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("gemini: không đọc được output JSON: %w", jerr)
	}
	r := Result{Text: strings.TrimSpace(res.Response), Model: req.Model, DurationMS: time.Since(start).Milliseconds()}
	for m, s := range res.Stats.Models {
		r.InputTokens += s.Tokens.Prompt
		r.OutputTokens += s.Tokens.Candidates
		if r.Model == "" {
			r.Model = m
		}
	}
	if r.Text == "" {
		return Result{}, errors.New("gemini: không có câu trả lời trong output")
	}
	return r, nil
}

var (
	ansiColor  = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	geminiFail = regexp.MustCompile(`(?:Error[^:]*|[A-Za-z]+Error): (.+)$`)
)

// GeminiError reads why Gemini CLI failed from its stderr: the first error
// line without its "Error authenticating: XError:" prefixes and the stack
// trace after it, with a hint for the refusals people can act on.
func GeminiError(stderr string) string {
	var lines []string
	for _, l := range strings.Split(stderr, "\n") {
		l = strings.TrimSpace(ansiColor.ReplaceAllString(l, ""))
		if l != "" && !strings.HasPrefix(l, "at ") && !slices.Contains(lines, l) {
			lines = append(lines, l)
		}
	}
	msg := ""
	for _, l := range lines {
		if m := geminiFail.FindStringSubmatch(l); m != nil {
			msg = m[1]
			for { // "Error authenticating: IneligibleTierError: reason"
				n := geminiFail.FindStringSubmatch(msg)
				if n == nil {
					break
				}
				msg = n[1]
			}
			break
		}
	}
	if msg == "" {
		msg = strings.Join(lines, "\n")
	}
	switch {
	case strings.Contains(stderr, "UNSUPPORTED_CLIENT") || strings.Contains(stderr, "IneligibleTierError"):
		msg += " — Google không còn cho Gemini CLI dùng tài khoản Google cá nhân (gói miễn phí): dùng API key (GEMINI_API_KEY từ aistudio.google.com/apikey) hoặc gói Code Assist có dự án Google Cloud (GOOGLE_CLOUD_PROJECT)."
	case strings.Contains(stderr, "ProjectIdRequiredError"):
		msg += " — tài khoản này cần dự án Google Cloud: đặt GOOGLE_CLOUD_PROJECT cho office."
	}
	return clip(msg, 600)
}

// ---- Antigravity ----

type antigravityCLI struct{ bin string }

// agyModel is a model slug in `agy models` output.
var agyModel = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)

// AntigravityModels lists the models of the signed-in account (`agy models`);
// a signed-out CLI gives ErrNeedsLogin.
func AntigravityModels(ctx context.Context, bin string, env []string) ([]string, error) {
	cmd := exec.CommandContext(ctx, bin, "models")
	if len(env) > 0 {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if strings.Contains(strings.ToLower(text), "sign in") || strings.Contains(text, "authentication") {
		return nil, fmt.Errorf("%w: hãy bấm Đăng nhập (tài khoản Google) cho Antigravity CLI", ErrNeedsLogin)
	}
	if err != nil {
		return nil, fmt.Errorf("agy models: %v: %s", err, clip(strings.TrimSpace(text), 300))
	}
	var models []string
	for _, l := range strings.Split(text, "\n") {
		f := strings.Fields(strings.Trim(l, " \t-*•>"))
		if len(f) > 0 && agyModel.MatchString(f[0]) && !slices.Contains(models, f[0]) {
			models = append(models, f[0])
		}
	}
	return models, nil
}

func (c *antigravityCLI) Check(ctx context.Context) (CheckResult, error) {
	out, err := run(ctx, c.bin, "", "--version")
	if err != nil {
		return CheckResult{}, err
	}
	v := strings.TrimSpace(out)
	models, err := AntigravityModels(ctx, c.bin, nil)
	if err != nil {
		return CheckResult{}, fmt.Errorf("%s · %w", v, err)
	}
	return CheckResult{Version: v, Models: models, Detail: v}, nil
}

// agyResult is Antigravity's json output (and the payload of its
// stream-json "result" event).
type agyResult struct {
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
}

func (c *antigravityCLI) Complete(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	// the prompt on stdin runs one turn non-interactively; plan mode only reads
	args := []string{"--output-format", "json", "--mode", "plan", "--disable-slash-commands"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	prompt := req.Prompt
	if req.System != "" {
		prompt = req.System + "\n\n" + req.Prompt
	}
	out, err := run(ctx, c.bin, prompt, args...)
	var res agyResult
	if jerr := json.Unmarshal([]byte(lastJSONObject(out)), &res); jerr != nil {
		if err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("agy: không đọc được output JSON: %w", jerr)
	}
	if res.Status != "SUCCESS" {
		if strings.Contains(res.Error, "authentication") {
			return Result{}, fmt.Errorf("%w: Antigravity CLI chưa đăng nhập", ErrNeedsLogin)
		}
		return Result{}, fmt.Errorf("agy: %s", clip(cmp.Or(res.Error, res.Status), 400))
	}
	return Result{Text: strings.TrimSpace(res.Response), Model: req.Model, DurationMS: time.Since(start).Milliseconds(),
		InputTokens: res.Usage.InputTokens + res.Usage.CacheReadTokens, OutputTokens: res.Usage.OutputTokens + res.Usage.ThinkingTokens}, nil
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
