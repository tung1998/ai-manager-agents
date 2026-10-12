package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Moving MCP servers into office (ADR-093): read a server's config from
// where Claude Code / Codex keep it, take it out of there (a copy of the
// file goes to trash first), and put it back on request.

// MovableTypes are the places office moves MCP servers from.
var MovableTypes = map[string]string{"user": "claude", "local": "claude", "project": "mcp.json", "codex": "codex"}

// MCPConfig resolves an installed MCP server and reads its full config
// (secrets included: they go into office's encrypted store).
func (s *Service) MCPConfig(ctx context.Context, r Ref) (Item, map[string]any, error) {
	if r.Kind != "mcp" {
		return Item{}, nil, ErrNotFound
	}
	it, err := s.resolve(ctx, r)
	if err != nil {
		return it, nil, err
	}
	cfg, err := s.rawMCP(it)
	return it, cfg, err
}

// TakeOutMCP removes a moved server from its source, keeping a copy of the
// file in trash. It returns that copy.
func (s *Service) TakeOutMCP(ctx context.Context, it Item) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if done, err := s.forgetDisabled(ctx, it); done || err != nil {
		return "", err // a user server office kept while off: only office had it
	}
	it.Disabled = false
	loc := it.Location
	switch loc.Type {
	case "user", "local":
		return s.Installer.Remove(ctx, it)
	case "project":
		return s.Installer.Remove(ctx, it)
	case "codex":
		return s.Installer.removeCodexServer(loc.Path, it.Name)
	}
	return "", ErrNotAllowed
}

// Origin is where a moved server came from, to put it back.
type Origin struct {
	Type        string `json:"type"` // user | local | project | codex
	Path        string `json:"path"`
	ProjectPath string `json:"project_path,omitempty"`
	Name        string `json:"name"`
	Label       string `json:"label"`
	Backup      string `json:"backup,omitempty"` // the copy of the file office kept in trash
	MovedAt     string `json:"moved_at"`
	// ConfigEnc is the server's config as the source had it (${VAR}s
	// unexpanded), encrypted: putting it back writes no secret the source
	// did not hold.
	ConfigEnc string `json:"config_enc,omitempty"`
}

// PutBackMCP writes cfg back where o says the server came from. TakeOutMCP
// removed the server from its source, so if a server by that name exists
// there now, someone added it back while it was moved out: refuse (ErrExists)
// instead of silently overwriting it, unless it already matches cfg (a retry
// of a put-back that failed partway through).
func (s *Service) PutBackMCP(ctx context.Context, o Origin, cfg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch o.Type {
	case "user":
		if done, err := checkMCPConflict(s.Installer.existingUserMCP(o.Name)(), cfg); done {
			return err
		}
		return s.Installer.InstallMCP(ctx, Target{Scope: "user"}, o.Name, cfg, true)
	case "local":
		if done, err := checkMCPConflict(s.Installer.existingLocalMCP(o.ProjectPath, o.Name)(), cfg); done {
			return err
		}
		return s.Installer.InstallMCP(ctx, Target{Scope: "local", ProjectPath: o.ProjectPath}, o.Name, cfg, true)
	case "project":
		doc := readJSON(filepath.Join(o.ProjectPath, ".mcp.json"))
		servers, _ := doc["mcpServers"].(map[string]any)
		if done, err := checkMCPConflict(rawMCPEntry(servers, o.Name), cfg); done {
			return err
		}
		return s.Installer.InstallMCP(ctx, Target{Scope: "project", ProjectPath: o.ProjectPath}, o.Name, cfg, true)
	case "codex":
		home, _ := os.UserHomeDir()
		if s.Home != "" {
			home = s.Home
		}
		if filepath.Clean(o.Path) != filepath.Join(home, ".codex", "config.toml") {
			return ErrNotAllowed // only Codex's own config
		}
		if existing, err := codexServer(o.Path, o.Name); err == nil {
			raw, err := json.Marshal(existing)
			if err != nil {
				return err
			}
			if done, err := checkMCPConflict(string(raw), cfg); done {
				return err
			}
		} else if err != ErrNotFound && !os.IsNotExist(err) {
			return err
		}
		return appendCodexServer(o.Path, o.Name, cfg)
	}
	return ErrNotAllowed
}

// checkMCPConflict compares existingRaw (a server's current raw JSON under
// this name, "" if none) against cfg. done is true when PutBackMCP must stop
// here and return err as-is: nothing there yet (done=false) means proceed to
// install; an identical config (done=true, err=nil) means a harmless retry of
// a put-back that failed partway through; anything else (done=true,
// err=ErrExists or a marshal error) means someone put a different server back
// under this name while it was moved out.
func checkMCPConflict(existingRaw string, cfg map[string]any) (done bool, err error) {
	if existingRaw == "" {
		return false, nil
	}
	want, err := json.Marshal(cfg)
	if err != nil {
		return true, err
	}
	var existing, wantNorm any
	if err := json.Unmarshal([]byte(existingRaw), &existing); err != nil {
		return true, ErrExists // unreadable current config: treat as conflicting
	}
	if err := json.Unmarshal(want, &wantNorm); err != nil {
		return true, err
	}
	if reflect.DeepEqual(existing, wantNorm) {
		return true, nil
	}
	return true, ErrExists
}

// backup copies a file into trash.
func (in Installer) backup(p, label string) (string, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(in.Trash, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(in.Trash, time.Now().Format("20060102-150405")+"-"+label)
	return dest, os.WriteFile(dest, raw, 0o600)
}

// appendCodexServer adds a [mcp_servers.<name>] table to Codex's TOML.
func appendCodexServer(p, name string, cfg map[string]any) error {
	if !nameRe.MatchString(name) {
		return ErrBadName
	}
	if _, err := codexServer(p, name); err == nil {
		return ErrExists
	}
	q := strconv.Quote
	var b strings.Builder
	fmt.Fprintf(&b, "\n[mcp_servers.%s]\n", name)
	if u := toString(cfg["url"]); u != "" {
		fmt.Fprintf(&b, "url = %s\n", q(u))
	}
	if c := toString(cfg["command"]); c != "" {
		fmt.Fprintf(&b, "command = %s\n", q(c))
	}
	if args, ok := cfg["args"].([]any); ok && len(args) > 0 {
		parts := make([]string, 0, len(args))
		for _, a := range args {
			parts = append(parts, q(toString(a)))
		}
		fmt.Fprintf(&b, "args = [%s]\n", strings.Join(parts, ", "))
	}
	for _, field := range []string{"env", "headers"} {
		m, ok := cfg[field].(map[string]any)
		if !ok || len(m) == 0 {
			continue
		}
		table := field
		if field == "headers" {
			table = "http_headers"
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "\n[mcp_servers.%s.%s]\n", name, table)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s = %s\n", q(k), q(toString(m[k])))
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(b.String())
	return err
}

// ErrNotMovable: office does not move servers from there.
var ErrNotMovable = errors.New("office chưa chuyển được MCP từ chỗ này")
