package features

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func TestEffective(t *testing.T) {
	on, off := Effective(Config{}, true)
	if !slices.Equal(on, All()) || len(off) != 0 {
		t.Fatalf("no setting: everything on, got %v %v", on, off)
	}
	on, off = Effective(Config{Packs: map[string]bool{"develop": false}}, true)
	if len(on) != 0 || off[Ops] != ReasonProject {
		t.Fatalf("pack off: %v %v", on, off)
	}
	on, _ = Effective(Config{Packs: map[string]bool{"develop": true}, Off: []string{Ops}}, true)
	if Has(on, Ops) || !Has(on, Git) {
		t.Fatalf("one feature off: %v", on)
	}
	on, off = Effective(Config{Packs: map[string]bool{"develop": false}}, false)
	if len(on) != 0 || off[Git] != ReasonNoFolder || off[Ops] != ReasonProject {
		t.Fatalf("no folder wins for git: %v", off)
	}
	on, _ = Effective(Config{}, false)
	if Has(on, Git) || !Has(on, Ops) {
		t.Fatalf("no folder, everything else on: %v", on)
	}
}

func TestLoadSaveFor(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: t.TempDir()})
	if got := For(ctx, st, p.ID); !slices.Equal(got, All()) {
		t.Fatalf("unset: everything on, got %v", got)
	}
	if err := Save(ctx, st, p.ID, Config{Packs: map[string]bool{"develop": true, "nope": false}, Off: []string{Git, "nope"}}); err != nil {
		t.Fatal(err)
	}
	c := LoadConfig(ctx, st, p.ID)
	if _, ok := c.Packs["nope"]; ok || !slices.Equal(c.Off, []string{Git}) {
		t.Fatalf("unknown packs/features dropped: %+v", c)
	}
	if got := For(ctx, st, p.ID); Has(got, Git) || !Has(got, Ops) {
		t.Fatalf("git off: %v", got)
	}
	// a setting that does not read: everything on
	_ = st.Settings().Set(ctx, Key(p.ID), "broken")
	if got := For(ctx, st, p.ID); !slices.Equal(got, All()) {
		t.Fatalf("broken setting: everything on, got %v", got)
	}
}
