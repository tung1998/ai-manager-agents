package perm

import (
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
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
