package perm_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// TestCoversRisky: a wrapper by path or case, or "git *", is not allowed for good.
func TestCoversRisky(t *testing.T) {
	for p, want := range map[string]bool{
		"/bin/bash *":      true,
		"/usr/bin/env *":   true,
		"BASH *":           true,
		"SUDO *":           true,
		"git *":            true,
		"git -C x *":       true,
		"git -c *":         true,
		"git --no-pager *": true,
		"rm *":             true,
		"git switch *":     false,
		"git log *":        false,
		"go test *":        false,
		"bashful *":        false,
	} {
		if got := perm.CoversRisky(p); got != want {
			t.Errorf("CoversRisky(%q) = %v, want %v", p, got, want)
		}
	}
}

// TestRiskyGitGlobals: git's global options don't hide the subcommand.
func TestRiskyGitGlobals(t *testing.T) {
	for line, want := range map[string]bool{
		"git -C . push origin main":                  true,
		"git --no-pager push":                        true,
		"git -c x=y reset --hard":                    true,
		"git --git-dir=.git --work-tree=. clean -fd": true,
		"git --git-dir .git branch -D x":             true,
		"/usr/bin/git -C . push":                     true,
		"cd x && git -C . push":                      true,
		"git -C . status":                            false,
		"git -c x=y log":                             false,
		"git log -C push":                            false, // -C of log, not git's
	} {
		if got := perm.Risky(line); got != want {
			t.Errorf("Risky(%q) = %v, want %v", line, got, want)
		}
	}
}

func alwaysStore(t *testing.T) (storage.Store, storage.Repo, storage.Agent) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	org := team.NewService(st, nil)
	proj, _ := st.Repos().Create(ctx, storage.Repo{Name: "p"}) // no folder: every built-in pack
	if err := org.AddFromLibrary(ctx, proj.ID, team.StarterAgent); err != nil {
		t.Fatal(err)
	}
	agents, _ := st.Agents().List(ctx, proj.ID)
	return st, proj, agents[0]
}

func TestAllowAlways(t *testing.T) {
	ctx := context.Background()
	st, proj, ag := alwaysStore(t)
	pol := perm.DefaultPolicy()
	pol.Packs = []perm.Pack{{ID: "tools", Label: "Tools", Commands: []string{"make build"}}}
	perm.SavePolicy(ctx, st, proj.ID, pol)
	safe := perm.LoadPolicy(ctx, st, proj.ID).Safe

	// in a built-in pack already: no pack changes; the default picks are kept
	x, err := perm.AllowAlways(ctx, st, proj.ID, ag.ID, "go mod tidy")
	if err != nil || x.Added || x.NewPack || x.Pack != "Go" {
		t.Fatalf("in catalog = %+v %v", x, err)
	}
	got, _ := st.Agents().Get(ctx, ag.ID)
	if got.Permissions.Commands == nil || !slices.Contains(*got.Permissions.Commands, "go mod tidy") || len(*got.Permissions.Commands) != len(safe)+1 {
		t.Fatalf("picks = %v (safe %d)", got.Permissions.Commands, len(safe))
	}
	for _, c := range safe {
		if !slices.Contains(*got.Permissions.Commands, c) {
			t.Fatalf("lost safe command %q", c)
		}
	}

	// a project pack of the same program takes it
	if x, err = perm.AllowAlways(ctx, st, proj.ID, ag.ID, "make test"); err != nil || !x.Added || x.NewPack || x.Pack != "Tools" {
		t.Fatalf("same program = %+v %v", x, err)
	}
	// none: a pack is made
	if x, err = perm.AllowAlways(ctx, st, proj.ID, ag.ID, "git switch *"); err != nil || !x.Added || !x.NewPack || x.Pack != "git (đã duyệt)" {
		t.Fatalf("new pack = %+v %v", x, err)
	}
	// once made, the next of that program goes there
	if x, err = perm.AllowAlways(ctx, st, proj.ID, ag.ID, "git fetch *"); err != nil || x.NewPack || x.Pack != "git (đã duyệt)" {
		t.Fatalf("auto pack again = %+v %v", x, err)
	}
	p := perm.LoadPolicy(ctx, st, proj.ID)
	i := slices.IndexFunc(p.Packs, func(x perm.Pack) bool { return x.ID == "auto-git" })
	if i < 0 || !slices.Equal(p.Packs[i].Commands, []string{"git switch *", "git fetch *"}) || !slices.Contains(p.Packs[0].Commands, "make test") {
		t.Fatalf("packs = %+v", p.Packs)
	}
	got, _ = st.Agents().Get(ctx, ag.ID)
	acc := perm.Resolve(got, perm.Check, p)
	for _, c := range []string{"go mod tidy", "make test", "git switch *", "git fetch *", "git status"} {
		if !slices.Contains(acc.Commands, c) {
			t.Fatalf("access lacks %q: %v", c, acc.Commands)
		}
	}
	if _, err := perm.AllowAlways(ctx, st, proj.ID, ag.ID, "git reset *"); err == nil {
		t.Fatal("a pattern covering git reset --hard must be refused")
	}
	if _, err := perm.AllowAlways(ctx, st, proj.ID, ag.ID, "rm *"); err == nil {
		t.Fatal("rm must be refused")
	}
}

// TestSavePolicyIfRejectsConcurrentWrite: two admins both load the same
// policy, both edit from that shared starting point and save — the second
// save must be rejected (a real conflict), not silently clobber the first
// admin's change.
func TestSavePolicyIfRejectsConcurrentWrite(t *testing.T) {
	ctx := context.Background()
	st, proj, _ := alwaysStore(t)

	base := perm.DefaultPolicy()
	base.QuickChecks = "seed"
	if err := perm.SavePolicy(ctx, st, proj.ID, base); err != nil {
		t.Fatal(err)
	}

	// both admins open the editor at the same time: same read, same raw.
	shared, raw := perm.LoadPolicyRaw(ctx, st, proj.ID)

	first := shared
	first.QuickChecks = "from admin A"
	if err := perm.SavePolicyIf(ctx, st, proj.ID, raw, first); err != nil {
		t.Fatalf("first save should succeed: %v", err)
	}

	second := shared
	second.QuickChecks = "from admin B"
	if err := perm.SavePolicyIf(ctx, st, proj.ID, raw, second); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("second save with stale raw = %v, want storage.ErrConflict", err)
	}

	got := perm.LoadPolicy(ctx, st, proj.ID)
	if got.QuickChecks != "from admin A" {
		t.Fatalf("QuickChecks = %q, want admin A's change kept (not silently clobbered)", got.QuickChecks)
	}
}

// Two saves race from one shared read: exactly one lands, the other gets
// ErrConflict, and the stored policy is the winner's.
func TestSavePolicyIfConcurrent(t *testing.T) {
	ctx := context.Background()
	st, proj, _ := alwaysStore(t)
	if err := perm.SavePolicy(ctx, st, proj.ID, perm.DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
	shared, raw := perm.LoadPolicyRaw(ctx, st, proj.ID)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i, who := range []string{"A", "B"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := shared
			p.QuickChecks = who
			<-start
			errs[i] = perm.SavePolicyIf(ctx, st, proj.ID, raw, p)
		}()
	}
	close(start)
	wg.Wait()
	won := -1
	for i, err := range errs {
		switch {
		case err == nil:
			if won >= 0 {
				t.Fatal("both saves landed")
			}
			won = i
		case !errors.Is(err, storage.ErrConflict):
			t.Fatalf("save %d: %v", i, err)
		}
	}
	if won < 0 {
		t.Fatal("no save landed")
	}
	if got := perm.LoadPolicy(ctx, st, proj.ID).QuickChecks; got != []string{"A", "B"}[won] {
		t.Fatalf("stored %q, want the winner's", got)
	}
}

// A stored policy that is not valid JSON must still be overwritable.
func TestSavePolicyIfOverwritesCorrupt(t *testing.T) {
	ctx := context.Background()
	st, proj, _ := alwaysStore(t)
	if err := st.Settings().Set(ctx, "policy:"+proj.ID, "not a policy"); err != nil {
		t.Fatal(err)
	}
	p, raw := perm.LoadPolicyRaw(ctx, st, proj.ID)
	p.QuickChecks = "fixed"
	if err := perm.SavePolicyIf(ctx, st, proj.ID, raw, p); err != nil {
		t.Fatalf("save over a corrupt policy: %v", err)
	}
	if got := perm.LoadPolicy(ctx, st, proj.ID).QuickChecks; got != "fixed" {
		t.Fatalf("QuickChecks = %q", got)
	}
}

// UpdatePolicy re-reads and retries when a save lands in between, so both
// changes survive.
func TestUpdatePolicyRetriesOnConflict(t *testing.T) {
	ctx := context.Background()
	st, proj, _ := alwaysStore(t)
	calls := 0
	_, err := perm.UpdatePolicy(ctx, st, proj.ID, func(p *perm.Policy) (bool, error) {
		calls++
		if calls == 1 { // an admin saves while this change is being made
			other := perm.LoadPolicy(ctx, st, proj.ID)
			other.QuickChecks = "admin"
			if err := perm.SavePolicy(ctx, st, proj.ID, other); err != nil {
				t.Fatal(err)
			}
		}
		p.DenyPaths = append(p.DenyPaths, "secret/**")
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := perm.LoadPolicy(ctx, st, proj.ID)
	if calls != 2 || got.QuickChecks != "admin" || !slices.Contains(got.DenyPaths, "secret/**") {
		t.Fatalf("calls=%d policy=%+v: the admin's edit or the change was lost", calls, got)
	}
}
