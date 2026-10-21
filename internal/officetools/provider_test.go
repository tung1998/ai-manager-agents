package officetools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// onlyFor lists its tool for one project, as a pack on there only.
type onlyFor struct{ project string }

func (p onlyFor) Tools(sc Scope) []Tool {
	if sc.ProjectID != p.project {
		return nil
	}
	return []Tool{{Name: "pack_hello", Description: "hello", Schema: obj(map[string]any{}), ReadOnly: true}}
}
func (onlyFor) Call(_ context.Context, sc Scope, name string, _ json.RawMessage) (string, bool) {
	return name + " " + sc.ProjectID, false
}

// ADR-171: a pack's tools are there only where the pack lists them.
func TestProviderTools(t *testing.T) {
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	box := New(st, nil, nil)
	box.AddProvider(onlyFor{project: "p_on"})
	on, off := Scope{ProjectID: "p_on"}, Scope{ProjectID: "p_off"}
	if !box.Has(on, "pack_hello") || box.Has(off, "pack_hello") {
		t.Fatal("the pack's tool only where it is on")
	}
	if box.Has(Scope{ProjectID: "p_on", Office: true}, "pack_hello") {
		t.Fatal("not for the office assistant")
	}
	if out, isErr := box.Call(context.Background(), on, "pack_hello", nil); isErr || out != "pack_hello p_on" {
		t.Fatalf("call = %q %v", out, isErr)
	}
	if _, isErr := box.Call(context.Background(), off, "pack_hello", nil); !isErr {
		t.Fatal("off: an unknown tool")
	}
}
