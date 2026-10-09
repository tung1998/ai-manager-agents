package setup

import (
	"strings"
	"testing"
)

func TestUserPromptIncludesCatalog(t *testing.T) {
	out := userPrompt("shop", "Project: shop", "", nil, []string{"go test ./..."}, Toolbox{})
	if !strings.Contains(out, "go test ./...") {
		t.Fatalf("prompt must include the project's command catalog: %s", out)
	}
}

func TestUserPromptSkipsCatalogSectionWhenEmpty(t *testing.T) {
	out := userPrompt("shop", "Project: shop", "", nil, nil, Toolbox{})
	if strings.Contains(out, "command catalog") || strings.Contains(out, "office library") || strings.Contains(out, "MCP servers office") {
		t.Fatalf("prompt must not mention empty sections: %s", out)
	}
}

func TestUserPromptIncludesToolbox(t *testing.T) {
	out := userPrompt("shop", "Project: shop", "", nil, nil, Toolbox{
		Skills: []Tool{{Name: "deploy-check", Description: "checks a deploy"}},
		MCP:    []Tool{{Name: "github", Needs: "key"}},
	})
	for _, want := range []string{"deploy-check", `"github"`, `"needs": "key"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt must include %s: %s", want, out)
		}
	}
}

func TestCleanPicks(t *testing.T) {
	tools := []Tool{{Name: "playwright"}, {Name: "github", Needs: "key"}}
	got, warnings := cleanPicks("MCP", []Pick{{Name: "github", Reason: "PRs"}, {Name: "made-up"}, {Name: "github"}, {Name: " playwright "}}, tools)
	if len(got) != 2 || got[0].Name != "github" || got[0].Needs != "key" || got[1].Name != "playwright" {
		t.Fatalf("picks = %+v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "made-up") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestCleanQuickChecks(t *testing.T) {
	if s, w := cleanQuickChecks(".ts .vue: npx eslint {file}"); s == "" || len(w) != 0 {
		t.Fatalf("valid checks dropped: %q %v", s, w)
	}
	if s, w := cleanQuickChecks("not a rule"); s != "" || len(w) != 1 {
		t.Fatalf("invalid checks kept: %q %v", s, w)
	}
}
