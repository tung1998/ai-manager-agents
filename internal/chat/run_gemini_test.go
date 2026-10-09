package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeminiRunnerStreams(t *testing.T) {
	tmp := t.TempDir()
	// the person's ~/.gemini: their settings and a sign-in to link through
	real := filepath.Join(tmp, "home")
	os.MkdirAll(filepath.Join(real, ".gemini"), 0o700)
	os.WriteFile(filepath.Join(real, ".gemini", "settings.json"), []byte(`{"security":{"auth":{"selectedType":"oauth-personal"}},"mcpServers":{"mine":{"command":"x"}}}`), 0o600)
	os.WriteFile(filepath.Join(real, ".gemini", "oauth_creds.json"), []byte(`{}`), 0o600)
	t.Setenv("GEMINI_CLI_HOME", real)

	bin, seen := filepath.Join(tmp, "gemini"), filepath.Join(tmp, "seen")
	os.WriteFile(bin, []byte(`#!/bin/sh
{ echo "$GEMINI_CLI_HOME"; echo "$*"; echo "token=$OFFICE_MCP_TOKEN"; cat "$GEMINI_CLI_HOME/.gemini/settings.json"; echo;
  test -e "$GEMINI_CLI_HOME/.gemini/oauth_creds.json" && echo creds-linked; } > `+seen+`
cat >/dev/null
echo 'Loaded cached credentials.'
echo '{"type":"init","session_id":"s","model":"gemini-3.5-flash"}'
echo '{"type":"message","role":"user","content":"chào"}'
echo '{"type":"message","role":"assistant","content":"Xin ","delta":true}'
echo '{"type":"tool_use","tool_name":"read_file","tool_id":"1","parameters":{"file_path":"a.txt"}}'
echo '{"type":"tool_use","tool_name":"mcp_office_ops_overview","tool_id":"2","parameters":{}}'
echo '{"type":"tool_use","tool_name":"mcp_my_srv_list","tool_id":"3","parameters":{}}'
echo '{"type":"message","role":"assistant","content":"chào","delta":true}'
echo '{"type":"result","status":"success","stats":{"input_tokens":12,"output_tokens":3}}'
`), 0o755)

	req := RunRequest{Bin: bin, Prompt: "chào", WorkDir: tmp, Write: true,
		Office: &OfficeAccess{MCPURL: "http://127.0.0.1:1/mcp", Token: "tok", Gateway: []string{"my srv"}}}
	var text strings.Builder
	res, err := geminiRunner{}.Run(context.Background(), req, func(e Event) {
		if e.Type == "text" {
			text.WriteString(e.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "Xin chào" || res.Text != "Xin chào" || res.Usage.Model != "gemini-3.5-flash" || res.Usage.InputTokens != 12 || res.Usage.OutputTokens != 3 {
		t.Fatalf("text=%q res=%+v", text.String(), res)
	}
	if len(res.Tools) != 3 || res.Tools[0].Summary != "Đọc a.txt" || res.Tools[1].Name != "mcp__office__ops_overview" || res.Tools[2].Name != "mcp__my srv__list" {
		t.Fatalf("tools = %+v", res.Tools)
	}

	raw, _ := os.ReadFile(seen)
	got := string(raw)
	for _, want := range []string{"--approval-mode auto_edit", "--allowed-mcp-server-names", "token=tok", "creds-linked"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	lines := strings.Split(got, "\n")
	var settings struct {
		Security   map[string]any `json:"security"`
		MCPServers map[string]struct {
			HTTPURL string            `json:"httpUrl"`
			Headers map[string]string `json:"headers"`
			Trust   bool              `json:"trust"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &settings); err != nil {
		t.Fatalf("settings %q: %v", lines[3], err)
	}
	off := settings.MCPServers["office"]
	if settings.Security == nil || off.HTTPURL != "http://127.0.0.1:1/mcp" || !off.Trust || off.Headers["Authorization"] != "Bearer $OFFICE_MCP_TOKEN" {
		t.Fatalf("settings = %+v", settings)
	}
	if _, ok := settings.MCPServers["mine"]; ok {
		t.Fatal("the person's MCP servers leak in without UserMCP")
	}
	if gw := settings.MCPServers["my srv"]; gw.HTTPURL != "http://127.0.0.1:1/mcp/s/my srv" || gw.Headers[ClientHeader] != "gemini" {
		t.Fatalf("gateway = %+v", gw)
	}
	// the throwaway home is gone, the real sign-in is not
	if _, err := os.Stat(lines[0]); lines[0] == real || !os.IsNotExist(err) {
		t.Fatalf("throwaway home %q left behind", lines[0])
	}
	if _, err := os.Stat(filepath.Join(real, ".gemini", "oauth_creds.json")); err != nil {
		t.Fatal("real credentials removed")
	}
}

// A JSON line over the scanner's 16MB cap must fail the run instead of
// silently returning an empty "success" (the "result" event never gets read).
func TestGeminiFailsOnOversizedLine(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gemini")
	script := `#!/bin/sh
cat >/dev/null
echo '{"type":"init","session_id":"s1"}'
python3 -c 'print("{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"" + "x"*(17*1024*1024) + "\"}")'
`
	os.WriteFile(bin, []byte(script), 0o755)
	res, err := geminiRunner{}.Run(context.Background(), RunRequest{Bin: bin, WorkDir: dir, Prompt: "hi"}, func(Event) {})
	if err == nil {
		t.Fatalf("expected an error for an oversized line, got res = %+v", res)
	}
	if res.Text != "" {
		t.Fatalf("result must not be read past the oversized line: res = %+v", res)
	}
}

func TestGeminiRunnerError(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "gemini")
	os.WriteFile(bin, []byte(`#!/bin/sh
cat >/dev/null
echo 'Please set an Auth method in your settings.json' >&2
exit 41
`), 0o755)
	_, err := geminiRunner{}.Run(context.Background(), RunRequest{Bin: bin, Prompt: "x", WorkDir: tmp}, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "Auth method") {
		t.Fatalf("err = %v", err)
	}
}

// resumeCLI is a fake CLI that logs argv and stdin per call and fails a
// resume of any session but "live".
func resumeCLI(t *testing.T, name, started string) (bin, log string) {
	tmp := t.TempDir()
	bin, log = filepath.Join(tmp, name), filepath.Join(tmp, "log")
	os.WriteFile(bin, []byte(`#!/bin/sh
in=$(cat)
echo "ARGS $*" >> `+log+`
echo "STDIN $in" >> `+log+`
case "$*" in *"resume gone"*|*"--resume gone"*) echo 'Error resuming session: Invalid session identifier "gone"' >&2; exit 42;; esac
echo '`+started+`'
echo '{"type":"message","role":"assistant","content":"ok"}'
echo '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}'
`), 0o755)
	return bin, log
}

func TestCLIRunnersResume(t *testing.T) {
	for _, c := range []struct {
		name    string
		runner  Runner
		started string
		resume  string
	}{
		{"gemini", geminiRunner{}, `{"type":"init","session_id":"live","model":"m"}`, "--resume live"},
		{"codex", codexRunner{}, `{"type":"thread.started","thread_id":"live"}`, "resume live -"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GEMINI_CLI_HOME", t.TempDir())
			bin, log := resumeCLI(t, c.name, c.started)
			hist := []HistoryItem{{Role: "user", Content: "câu cũ"}, {Role: "assistant", Content: "trả lời cũ"}}
			req := RunRequest{Bin: bin, System: "HƯỚNG DẪN", History: hist, Prompt: "mới", WorkDir: t.TempDir()}

			// a fresh session: instructions and transcript, and its id comes back
			res, err := c.runner.Run(context.Background(), req, func(Event) {})
			if err != nil || res.SessionID != "live" || res.Text != "ok" {
				t.Fatalf("fresh: %+v %v", res, err)
			}
			// going on: only the new message, in the same session
			os.Remove(log)
			req.SessionID = "live"
			if res, err = c.runner.Run(context.Background(), req, func(Event) {}); err != nil || res.SessionID != "live" {
				t.Fatalf("resume: %+v %v", res, err)
			}
			raw, _ := os.ReadFile(log)
			got := string(raw)
			if !strings.Contains(got, c.resume) || strings.Contains(got, "HƯỚNG DẪN") || strings.Contains(got, "câu cũ") {
				t.Fatalf("resume call:\n%s", got)
			}
			// a session the CLI lost: start over with the transcript
			os.Remove(log)
			req.SessionID = "gone"
			if res, err = c.runner.Run(context.Background(), req, func(Event) {}); err != nil || res.SessionID != "live" {
				t.Fatalf("lost: %+v %v", res, err)
			}
			raw, _ = os.ReadFile(log)
			if got = string(raw); strings.Count(got, "ARGS") != 2 || !strings.Contains(got, "HƯỚNG DẪN") || !strings.Contains(got, "câu cũ") {
				t.Fatalf("fallback calls:\n%s", got)
			}
		})
	}
}
