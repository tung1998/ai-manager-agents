package automation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// While a server is moved out, someone can add a different server back under
// the same name (another CLI, another catalog install). PutBackMCP must
// refuse instead of silently overwriting it.
func TestPutBackMCPUserConflict(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := map[string]any{"mcpServers": map[string]any{"x": map[string]any{"command": "added-back"}}}
	raw, _ := json.Marshal(cfg)
	write(t, filepath.Join(home, ".claude.json"), string(raw))

	log := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeClaude(t, log, false)
	s := &Service{Home: home, Installer: Installer{Home: home, Trash: t.TempDir(), Claude: func() string { return bin }}}

	err := s.PutBackMCP(ctx, Origin{Type: "user", Name: "x"}, map[string]any{"command": "original"})
	if err != ErrExists {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if calls, _ := os.ReadFile(log); len(strings.TrimSpace(string(calls))) != 0 {
		t.Fatalf("InstallMCP must not run on conflict, calls = %q", calls)
	}
	// the server added back while moved out must survive untouched.
	after, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if string(after) != string(raw) {
		t.Fatalf("config changed: %s", after)
	}
}

// No server under that name: TakeOutMCP's removal held, so put it back.
func TestPutBackMCPUserNoConflict(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{}}`)

	log := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeClaude(t, log, false)
	s := &Service{Home: home, Installer: Installer{Home: home, Trash: t.TempDir(), Claude: func() string { return bin }}}

	if err := s.PutBackMCP(ctx, Origin{Type: "user", Name: "x"}, map[string]any{"command": "original"}); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "add-json") {
		t.Fatalf("calls = %q, want add-json", calls)
	}
}

// A retry of a put-back that failed partway through (e.g. the store write
// after it): the same config is already there, so this is success, not a
// conflict, and must not re-run the CLI.
func TestPutBackMCPUserAlreadyDone(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	cfg := map[string]any{"mcpServers": map[string]any{"x": map[string]any{"command": "original"}}}
	raw, _ := json.Marshal(cfg)
	write(t, filepath.Join(home, ".claude.json"), string(raw))

	log := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeClaude(t, log, false)
	s := &Service{Home: home, Installer: Installer{Home: home, Trash: t.TempDir(), Claude: func() string { return bin }}}

	if err := s.PutBackMCP(ctx, Origin{Type: "user", Name: "x"}, map[string]any{"command": "original"}); err != nil {
		t.Fatal(err)
	}
	if calls, _ := os.ReadFile(log); len(strings.TrimSpace(string(calls))) != 0 {
		t.Fatalf("already-matching put-back must not call the CLI again, calls = %q", calls)
	}
}

func TestPutBackMCPLocalConflict(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	proj := t.TempDir()
	cfg := map[string]any{"projects": map[string]any{proj: map[string]any{
		"mcpServers": map[string]any{"x": map[string]any{"command": "added-back"}},
	}}}
	raw, _ := json.Marshal(cfg)
	write(t, filepath.Join(home, ".claude.json"), string(raw))

	log := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeClaude(t, log, false)
	s := &Service{Home: home, Installer: Installer{Home: home, Trash: t.TempDir(), Claude: func() string { return bin }}}

	err := s.PutBackMCP(ctx, Origin{Type: "local", ProjectPath: proj, Name: "x"}, map[string]any{"command": "original"})
	if err != ErrExists {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if calls, _ := os.ReadFile(log); len(strings.TrimSpace(string(calls))) != 0 {
		t.Fatalf("InstallMCP must not run on conflict, calls = %q", calls)
	}
}

func TestPutBackMCPLocalNoConflict(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	proj := t.TempDir()
	write(t, filepath.Join(home, ".claude.json"), `{"projects":{}}`)

	log := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeClaude(t, log, false)
	s := &Service{Home: home, Installer: Installer{Home: home, Trash: t.TempDir(), Claude: func() string { return bin }}}

	if err := s.PutBackMCP(ctx, Origin{Type: "local", ProjectPath: proj, Name: "x"}, map[string]any{"command": "original"}); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "add-json") {
		t.Fatalf("calls = %q, want add-json", calls)
	}
}

func TestPutBackMCPProjectConflict(t *testing.T) {
	ctx := context.Background()
	proj := t.TempDir()
	write(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"x":{"command":"added-back"}}}`)

	s := &Service{Installer: Installer{Trash: t.TempDir()}}
	err := s.PutBackMCP(ctx, Origin{Type: "project", ProjectPath: proj, Name: "x"}, map[string]any{"command": "original"})
	if err != ErrExists {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	after, _ := os.ReadFile(filepath.Join(proj, ".mcp.json"))
	if !strings.Contains(string(after), "added-back") {
		t.Fatalf(".mcp.json overwritten: %s", after)
	}
}

func TestPutBackMCPProjectNoConflict(t *testing.T) {
	ctx := context.Background()
	proj := t.TempDir()

	s := &Service{Installer: Installer{Trash: t.TempDir()}}
	if err := s.PutBackMCP(ctx, Origin{Type: "project", ProjectPath: proj, Name: "x"}, map[string]any{"command": "original"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(proj, ".mcp.json"))
	if !strings.Contains(string(after), "original") {
		t.Fatalf(".mcp.json = %s, want the put-back config", after)
	}
}

func TestPutBackMCPProjectAlreadyDone(t *testing.T) {
	ctx := context.Background()
	proj := t.TempDir()
	write(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"x":{"command":"original"}}}`)

	s := &Service{Installer: Installer{Trash: t.TempDir()}}
	if err := s.PutBackMCP(ctx, Origin{Type: "project", ProjectPath: proj, Name: "x"}, map[string]any{"command": "original"}); err != nil {
		t.Fatal(err)
	}
}

// A codex put-back retried with the identical config (e.g. the caller crashed
// before persisting the "moved" flag elsewhere) must succeed as a no-op,
// like the other three scopes, instead of always returning ErrExists.
func TestPutBackMCPCodexRetrySameConfig(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	p := filepath.Join(home, ".codex", "config.toml")

	s := &Service{Home: home, Installer: Installer{Trash: t.TempDir()}}
	cfg := map[string]any{"command": "npx", "args": []any{"-y", "server"}}

	if err := s.PutBackMCP(ctx, Origin{Type: "codex", Path: p, Name: "x"}, cfg); err != nil {
		t.Fatalf("first put-back: %v", err)
	}
	raw, _ := os.ReadFile(p)
	if n := strings.Count(string(raw), "[mcp_servers.x]"); n != 1 {
		t.Fatalf("after first put-back, [mcp_servers.x] count = %d, want 1:\n%s", n, raw)
	}

	if err := s.PutBackMCP(ctx, Origin{Type: "codex", Path: p, Name: "x"}, cfg); err != nil {
		t.Fatalf("retry with identical cfg: err = %v, want nil", err)
	}
	raw, _ = os.ReadFile(p)
	if n := strings.Count(string(raw), "[mcp_servers.x]"); n != 1 {
		t.Fatalf("after retry, [mcp_servers.x] count = %d, want 1 (no duplicate write):\n%s", n, raw)
	}
}

// A different server added back under the same name while it was moved out
// must still be refused.
func TestPutBackMCPCodexConflict(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	p := filepath.Join(home, ".codex", "config.toml")

	s := &Service{Home: home, Installer: Installer{Trash: t.TempDir()}}
	if err := s.PutBackMCP(ctx, Origin{Type: "codex", Path: p, Name: "x"}, map[string]any{"command": "added-back"}); err != nil {
		t.Fatalf("seed put-back: %v", err)
	}

	err := s.PutBackMCP(ctx, Origin{Type: "codex", Path: p, Name: "x"}, map[string]any{"command": "original"})
	if err != ErrExists {
		t.Fatalf("err = %v, want ErrExists", err)
	}
}
