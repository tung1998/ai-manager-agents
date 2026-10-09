package perm

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func TestLevels(t *testing.T) {
	a := storage.Agent{Permissions: storage.Permissions{Level: Edit}}
	if got := Effective(a, Operate, Policy{MaxLevel: Check}); got != Check {
		t.Fatalf("project cap: %s", got)
	}
	if got := Effective(a, Propose, Policy{MaxLevel: Operate}); got != Propose {
		t.Fatalf("mode cap: %s", got)
	}
	ro := storage.Agent{Permissions: storage.Permissions{ReadOnly: true}}
	if got := Effective(ro, Operate, Policy{MaxLevel: Operate}); got != Read {
		t.Fatalf("read-only agent: %s", got)
	}
	if !AtLeast(Edit, Check) || AtLeast(Propose, Check) {
		t.Fatal("AtLeast")
	}
	legacy := storage.Agent{}
	if Agent(legacy) != Propose {
		t.Fatal("legacy writable agent should be propose")
	}
}

func TestDenied(t *testing.T) {
	p := Policy{DenyPaths: []string{".env", "**/*.pem", "migrations/", "dashboard/nuxt.config.ts"}}
	got := p.Denied([]string{".env", "a/b/cert.pem", "migrations/0001.sql", "dashboard/nuxt.config.ts", "app/x.vue", "cert.pem"})
	if len(got) != 5 {
		t.Fatalf("denied: %v", got)
	}
}

func TestDefaultPolicyDeniesNestedEnv(t *testing.T) {
	p := DefaultPolicy()
	for _, f := range []string{"backend/.env.local", "api/.env.production", "a/b/.env.test", ".env.local", ".env"} {
		if len(p.Denied([]string{f})) != 1 {
			t.Errorf("%q should be denied", f)
		}
	}
	if len(p.Denied([]string{"src/env.go"})) != 0 {
		t.Error("src/env.go should not be denied")
	}
}

func TestLoadPolicyUpgradesOldEnvPattern(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "office.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := Policy{DenyPaths: []string{".env", ".env.*", "**/*.pem"}}
	if err := SavePolicy(context.Background(), st, "proj", old); err != nil {
		t.Fatal(err)
	}
	p := LoadPolicy(context.Background(), st, "proj")
	if len(p.Denied([]string{"backend/.env.local"})) != 1 {
		t.Fatalf("stale saved policy should still block nested .env files: %v", p.DenyPaths)
	}
}

// The quick check (ADR-133) is on by default, also for a policy saved before
// it existed; turned off, it stays off.
func TestQuickCheckDefault(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "office.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if !LoadPolicy(ctx, st, "none").QuickCheck {
		t.Fatal("default off")
	}
	if err := st.Settings().Set(ctx, policyKey("old"), map[string]any{"deny_paths": []string{".env"}}); err != nil {
		t.Fatal(err)
	}
	if !LoadPolicy(ctx, st, "old").QuickCheck {
		t.Fatal("a policy saved before the setting lost it")
	}
	p := DefaultPolicy()
	p.QuickCheck = false
	if err := SavePolicy(ctx, st, "off", p); err != nil {
		t.Fatal(err)
	}
	if LoadPolicy(ctx, st, "off").QuickCheck {
		t.Fatal("turned off, still on")
	}
}

func TestResolveCustomCaps(t *testing.T) {
	caps := []string{CapPropose, CapCommands, CapCommit}
	cmds := []string{"go test ./..."}
	a := storage.Agent{Permissions: storage.Permissions{Level: Propose, Caps: &caps, Commands: &cmds}}
	if Agent(a) != Edit {
		t.Fatalf("own level follows the highest pick: %s", Agent(a))
	}
	p := DefaultPolicy()
	p.Catalog = []string{"go test ./...", "go vet ./...", "pnpm run deploy"}
	p.Safe = []string{"go test ./...", "go vet ./..."}
	acc := Resolve(a, Operate, p)
	if !acc.Can(CapCommit) || acc.Can(CapApply) || len(acc.Commands) != 1 {
		t.Fatalf("custom picks: %+v", acc)
	}
	if acc := Resolve(a, Check, p); acc.Can(CapCommit) || !acc.Can(CapCommands) {
		t.Fatalf("mode lowers: %+v", acc)
	}
	// no picks: the safe commands, from read on
	if acc := Resolve(storage.Agent{Permissions: storage.Permissions{Level: Read}}, Operate, p); len(acc.Commands) != 2 || len(acc.Safe) != 2 {
		t.Fatalf("default is the safe commands: %+v", acc)
	}
	// picking an unsafe command: listed, but not run on its own at any level
	deploy := []string{"pnpm run deploy", "gone"}
	acc = Resolve(storage.Agent{Permissions: storage.Permissions{Level: Check, Commands: &deploy}}, Operate, p)
	if len(acc.Commands) != 1 || len(acc.Safe) != 0 {
		t.Fatalf("picks within the catalog: %+v", acc)
	}
}

func TestCatalogSafe(t *testing.T) {
	packs := []Pack{
		{ID: "scripts", Commands: []string{"pnpm run test", "pnpm run test:unit", "pnpm run dev", "pnpm run deploy", "pnpm run typecheck", "pnpm run build"}},
		{ID: "go", Commands: []string{"go test ./...", "go mod tidy"}},
		{ID: "custom-1", Custom: true, Commands: []string{"make test"}},
	}
	all, safe := Catalog(packs)
	if len(all) != 9 || strings.Join(safe, ",") != "pnpm run test,pnpm run test:unit,pnpm run typecheck,pnpm run build,go test ./..." {
		t.Fatalf("all=%v safe=%v", all, safe)
	}
}

func TestCommands(t *testing.T) {
	if _, err := SplitCommand("go test ./... && rm -rf /"); err == nil {
		t.Fatal("shell syntax")
	}
	args, err := SplitCommand(`git log --format="%h %s" -5`)
	if err != nil || len(args) != 4 || args[2] != "--format=%h %s" {
		t.Fatalf("split: %q %v", args, err)
	}
	// a proposed command is stored joined, then split again when it runs
	for _, want := range [][]string{
		args,
		{"sqlite3", "-readonly", "x.db", `SELECT a FROM m WHERE id='c 1' AND b="q"`},
		{"echo", "", "it's", `say "hi"`},
	} {
		got, err := SplitCommand(JoinCommand(want))
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("round trip %q: %q %v", want, got, err)
		}
	}
	pats := []string{"go test *", "git status"}
	if _, ok := MatchCommand(pats, []string{"go", "test", "./x"}); !ok {
		t.Fatal("prefix")
	}
	if _, ok := MatchCommand(pats, []string{"go", "testx"}); ok {
		t.Fatal("word boundary")
	}
	if _, ok := MatchCommand(pats, []string{"git", "status", "-s"}); ok {
		t.Fatal("exact")
	}
	if _, err := CleanPattern("go * test"); err == nil {
		t.Fatal("star only at the end")
	}
}

// A " *" pattern of a command that reads or checks never takes a flag that
// writes files or runs a program of the caller's choice.
func TestCommandsRiskyFlags(t *testing.T) {
	pats := []string{"go test *", "git diff *", "git log *", "git show *", "pytest *", "docker compose logs *"}
	for _, line := range []string{
		"go test -exec /tmp/x ./...", "go test -exec=/tmp/x ./...", "go test --toolexec=x ./...",
		"go test -o /tmp/bin ./x", "go test -c ./x", "go test -coverprofile=/etc/x ./...", "go test -overlay o.json ./...",
		"git diff --output=/etc/passwd", "git log --output /tmp/x", "git show --output=x HEAD",
		"pytest -p evil", "pytest -pevil", "pytest -vp evil", "pytest --basetemp=/home", "pytest -c other.ini",
		"pytest --rootdir=/", "pytest -o addopts=-x", "pytest --override-ini=x", "pytest --junitxml=/etc/x",
		"pytest -p=evil", "pytest -xc x.ini", "pytest -vo x=y", "pytest -p", "pytest -p evil -p no:cacheprovider",
		"pytest -p no:x -p evil", "pytest -vpevil",
		// files out of the project (ADR-094)
		"git diff --no-index /x ~/.ssh/id_rsa", "git diff --no-index a b", "git diff --no-ind a b",
		"git diff /etc/passwd /etc/hosts", "git diff HEAD -- ../other/secret", "git show HEAD -- /etc/passwd",
		"git log -- internal/../../x", "git diff --outp=x", "git log ~/.ssh",
		"go test ../other/...", "go test /abs/pkg", "go test -coverpkg=../x/... ./...",
		"pytest /etc/x.py", "pytest ../tests", "pytest --deselect=/abs/x.py",
	} {
		args, err := SplitCommand(line)
		if err != nil {
			t.Fatal(line, err)
		}
		if p, ok := MatchCommand(pats, args); ok {
			t.Errorf("%q allowed by %q", line, p)
		}
	}
	for _, line := range []string{
		"go test ./...", "go test -run TestX -v -count=1 ./internal/...", "go test -race -cover ./x",
		"git diff --stat HEAD~1", "git log --oneline -5", "git show HEAD",
		"pytest -x -q tests", "pytest -k slow", "pytest --co", "docker compose logs -f web",
		"pytest -p no:cacheprovider", "pytest -pno:cacheprovider", "pytest -vp no:randomly", "pytest -p=no:x",
		"pytest -ktest_cache", "pytest -mslow_proc", "pytest -rp", "pytest -rfEp", "pytest -vrp tests",
		"pytest -W error::DeprecationWarning", "pytest -n 4", "pytest -k 'not proc'",
		// paths in the project
		"git diff HEAD~1 -- internal/", "git log HEAD~3..HEAD", "git diff main...HEAD -- docs/a.md",
		"git show HEAD~1:internal/x.go", "git diff -- internal/../go.mod", "git log --no-merges -3",
		"go test ./internal/...", "go test -run TestX ./internal/perm/", "pytest tests/x.py", "pytest tests/x.py::test_a",
		"docker compose logs --tail=50 web",
	} {
		args, _ := SplitCommand(line)
		if _, ok := MatchCommand(pats, args); !ok {
			t.Errorf("%q refused", line)
		}
	}
	// an exact pattern still allows what it names
	args, _ := SplitCommand("go test -c ./x")
	if _, ok := MatchCommand([]string{"go test -c ./x"}, args); !ok {
		t.Error("exact pattern")
	}
}

func TestUserMCPAtEveryLevel(t *testing.T) { // MCP servers are tools, like Read
	for _, l := range []string{Read, Propose, Check, Edit, Operate} {
		if !slices.Contains(Preset(l), CapUserMCP) {
			t.Fatalf("%s preset lacks user MCP: %v", l, Preset(l))
		}
	}
	a := storage.Agent{Permissions: storage.Permissions{ReadOnly: true}}
	if acc := Resolve(a, Operate, Policy{MaxLevel: Operate}); !acc.Can(CapUserMCP) || acc.Level != Read {
		t.Fatalf("a read-only lead = %+v", acc)
	}
}
