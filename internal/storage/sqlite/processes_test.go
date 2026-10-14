package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Update is CAS'd on updated_at, so two concurrent PATCHes that both read the
// same row (one toggling autostart, one editing the command) can't have the
// second silently overwrite the first's write: the loser gets ErrStale
// instead of dropping a change.
func TestProcessUpdateConflictsWithConcurrentWrite(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	proc, err := st.Processes().Create(ctx, storage.Process{
		ProjectID: p.ID, Name: "web", Command: "npm start", Autostart: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	// two independent reads of the same row, like OpsPanel's toggleAutostart
	// and saveForm racing each other.
	fromToggle := proc
	fromForm := proc

	fromToggle.Autostart = true
	if err := st.Processes().Update(ctx, fromToggle); err != nil {
		t.Fatalf("first update: %v", err)
	}

	fromForm.Command = "npm run dev"
	if err := st.Processes().Update(ctx, fromForm); !errors.Is(err, storage.ErrStale) {
		t.Fatalf("stale second update = %v, want ErrStale", err)
	}

	got, err := st.Processes().Get(ctx, proc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Autostart {
		t.Fatalf("autostart toggle was lost: %+v", got)
	}
	if got.Command != "npm start" {
		t.Fatalf("stale write landed despite ErrStale: %+v", got)
	}
}

// Update still answers storage.ErrConflict (not ErrStale) for the actual
// unique-name violation, so the handler can tell a race from a plain
// duplicate name and keep the right message for each.
func TestProcessUpdateNameConflictIsNotStale(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	if _, err := st.Processes().Create(ctx, storage.Process{ProjectID: p.ID, Name: "web"}); err != nil {
		t.Fatal(err)
	}
	other, err := st.Processes().Create(ctx, storage.Process{ProjectID: p.ID, Name: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	other.Name = "web"
	if err := st.Processes().Update(ctx, other); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("rename to a taken name = %v, want ErrConflict", err)
	}
}
