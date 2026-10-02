package actions

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

type fakeMCP struct{ got string }

func (f *fakeMCP) CallTool(_ context.Context, server, tool string, args json.RawMessage) (string, bool, error) {
	f.got = server + "/" + tool + " " + string(args)
	return "đã ghi", false, nil
}

// A call of an MCP tool that writes waits for a person even at the top
// level (the gateway already let through what the agent may call), each
// call is its own proposal, and approving it runs the call (ADR-093).
func TestMCPCall(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	svc := New(st, nil)
	sc := Scope{ProjectID: proj.ID, RunRef: "r1", Agent: "Dev", Level: perm.Operate, Access: perm.Access{Level: perm.Operate, Caps: perm.Preset(perm.Operate)}}
	call := func(x string) storage.ActionArgs {
		return storage.ActionArgs{MCP: &storage.MCPCallArgs{Server: "jira", Tool: "create_issue", Arguments: json.RawMessage(`{"title":"` + x + `"}`)}}
	}
	if _, err := svc.Propose(ctx, sc, "mcp_call", "", "", call("a")); err == nil {
		t.Fatal("mcp_call without the gateway")
	}
	f := &fakeMCP{}
	svc.SetMCP(f)
	a, err := svc.Propose(ctx, sc, "mcp_call", "", "Tham số: …", call("a"))
	if err != nil || a.Status != "pending" || a.Target != "jira/create_issue" {
		t.Fatalf("propose = %+v %v", a, err)
	}
	b, _ := svc.Propose(ctx, sc, "mcp_call", "", "", call("b"))
	if b.ID == a.ID {
		t.Fatal("two calls with other arguments became one proposal")
	}
	done, err := svc.Decide(ctx, a.ID, true, "human:a")
	if err != nil || done.Status != "done" || done.Detail != "đã ghi" || f.got != `jira/create_issue {"title":"a"}` {
		t.Fatalf("decide = %+v %v (%s)", done, err, f.got)
	}
}
