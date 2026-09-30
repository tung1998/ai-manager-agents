package chat

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/mcpserver"
)

// GuardCommand runs `office hook guard`: set by the office binary at start.
// Empty (tests, other hosts): a chat that edits runs under dontAsk, which
// Claude Code does not let write in .claude/.
var GuardCommand string

// guardSettings: under bypassPermissions (so an agent may add a skill in
// .claude/skills) every file edit and MCP call goes through `office hook guard`.
func guardSettings(userMCP bool) string {
	cmd := GuardCommand
	if userMCP {
		cmd += " --user-mcp"
	}
	h := []map[string]any{{"type": "command", "command": cmd}}
	b, _ := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []map[string]any{
		{"matcher": "Write|Edit|MultiEdit|NotebookEdit", "hooks": h},
		{"matcher": "mcp__.*", "hooks": h},
	}}})
	return string(b)
}

// Guard is a PreToolUse hook: an edit stays in the working folder (the
// worktree or the project), an MCP tool is office's own unless the agent may
// use the person's servers. Anything else it lets through.
func Guard(in io.Reader, out io.Writer, userMCP bool) {
	var ev struct {
		Cwd       string         `json:"cwd"`
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		deny(out, "agent-office: không đọc được yêu cầu")
		return
	}
	if strings.HasPrefix(ev.ToolName, "mcp__") {
		if !userMCP && !strings.HasPrefix(ev.ToolName, "mcp__"+mcpserver.ServerName+"__") {
			deny(out, "agent-office: agent này không được dùng MCP của người dùng")
		}
		return
	}
	p, _ := ev.ToolInput["file_path"].(string)
	if p == "" {
		p, _ = ev.ToolInput["notebook_path"].(string)
	}
	if p == "" || ev.Cwd == "" {
		return
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(ev.Cwd, p)
	}
	root, target := realPath(ev.Cwd), realPath(p)
	if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		deny(out, "agent-office: chỉ sửa file trong thư mục project")
	}
}

// realPath resolves symlinks of the part that exists (a new file's folder may not).
func realPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

func deny(out io.Writer, reason string) {
	_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
}

