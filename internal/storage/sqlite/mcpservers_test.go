package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Two Get-mutate-Update calls starting from the same row must not let the
// second silently clobber the first's field (lost update): Update is CAS'd
// on updated_at, so the second call gets ErrConflict instead.
func TestMCPServerUpdateConflict(t *testing.T) {
	ctx := context.Background()
	st, _ := openStore(t)
	m, err := st.MCPServers().Create(ctx, storage.MCPServer{Name: "demo", Kind: "http", URL: "https://example.com", Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	a, _ := st.MCPServers().Get(ctx, m.ID)
	b, _ := st.MCPServers().Get(ctx, m.ID)

	a.Enabled = false
	if err := st.MCPServers().Update(ctx, a); err != nil {
		t.Fatalf("first update: %v", err)
	}

	b.Agents = []string{"agt_1"}
	if err := st.MCPServers().Update(ctx, b); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("second update from the same stale row: got %v, want ErrConflict", err)
	}

	got, _ := st.MCPServers().Get(ctx, m.ID)
	if got.Enabled {
		t.Fatal("the first update's field was lost")
	}
	if len(got.Agents) != 0 {
		t.Fatal("the conflicting update's field was applied despite ErrConflict")
	}
}
