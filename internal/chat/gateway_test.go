package chat

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// A run gets the gateway's servers next to office's own, each at
// /mcp/s/<name> with the run's token (ADR-091); the guard lets their tools through.
func TestMCPConfigGateway(t *testing.T) {
	o := &OfficeAccess{MCPURL: "http://127.0.0.1:1/mcp", Token: "tok", Gateway: []string{"context7"}}
	p, err := writeMCPConfig(o)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	raw, _ := os.ReadFile(p)
	var cfg struct {
		MCPServers map[string]struct {
			Type, URL string
			Headers   map[string]string
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	c7 := cfg.MCPServers["context7"]
	if len(cfg.MCPServers) != 2 || c7.Type != "http" || c7.URL != "http://127.0.0.1:1/mcp/s/context7" || c7.Headers["Authorization"] != "Bearer tok" {
		t.Fatalf("config = %s", raw)
	}

	old := GuardCommand
	defer func() { GuardCommand = old }()
	GuardCommand = "'/x/office' hook guard"
	a := claudeRunner{}.args(RunRequest{Write: true, Office: o}, false)
	if s := a[slices.Index(a, "--settings")+1]; !strings.Contains(s, "--mcp context7") {
		t.Fatalf("guard without the gateway's server: %s", s)
	}
}
