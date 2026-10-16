package chat

import (
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/prompts"
)

// The lines of a prompt follow the project's keywords: a docs project (no
// git UI, no operations, no build) is not told about commits, logs or tests.
func TestOfficeToolsPromptFollowsKeywords(t *testing.T) {
	all := prompts.Render("chat/office-tools", map[string]any{"On": keywords("git", "ops", KeyBuild)})
	for _, s := range []string{"- Operations: ops_overview", "\n- Git: git_status", "\n- After proposing a code change", "\n- When the person pastes"} {
		if !strings.Contains(all, s) {
			t.Fatalf("everything on lacks %q:\n%s", s, all)
		}
	}
	none := prompts.Render("chat/office-tools", map[string]any{"On": keywords()})
	if !strings.HasPrefix(none, "- When the person pastes") {
		t.Fatalf("nothing on:\n%s", none)
	}
	for _, s := range []string{"ops_overview", "git_status", "build/test"} {
		if strings.Contains(none, s) {
			t.Fatalf("nothing on still mentions %q", s)
		}
	}
	ed := func(on ...string) string {
		return prompts.Render("chat/edit-worktree", map[string]any{"AutoApply": false, "OfficeTools": true, "On": keywords(on...), "Commands": []string{"git status"}})
	}
	if !strings.Contains(ed(KeyBuild), "run the related checks") || strings.Contains(ed(), "run the related checks") {
		t.Fatal("edit-worktree asks for checks only with build commands")
	}
}
