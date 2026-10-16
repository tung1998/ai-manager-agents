package chat

import (
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/features"
	"bitbucket.org/senprints/agent-office/internal/prompts"
)

func officeToolsText(on []string) string {
	return prompts.Render("chat/office-tools", map[string]any{"DevTools": devTools(on),
		"Logs": features.OpsTab(on), "Checks": features.Has(on, features.Checks)})
}

func TestOfficeToolsPromptFollowsFeatures(t *testing.T) {
	// everything on: the wording agents have always had
	want := "- You have office tools: ops_overview (build/dev/test processes, docker compose, monitors, incidents), process_logs, container_logs and monitor_detail to read; git_status/git_diff/git_log for git; and propose_action to PROPOSE running/restarting/stopping a process or container, a commit, a new branch or a push (the person approves, then office does it, unless the permission pack lets it run on its own; a push always needs approval). Asked about a failing build or run, a deploy or a monitor, get the real logs and state before concluding, then check them against the code. After proposing a code change, propose re-running the related build/test to verify it. When the person pastes"
	if got := officeToolsText(nil); !strings.HasPrefix(got, want) {
		t.Fatalf("everything on changed the prompt:\n%s", got)
	}
	got := officeToolsText([]string{})
	for _, s := range []string{"ops_overview", "git_status", "propose_action to PROPOSE", "build/test"} {
		if strings.Contains(got, s) {
			t.Fatalf("develop off still mentions %q:\n%s", s, got)
		}
	}
	if !strings.HasPrefix(got, "- When the person pastes") {
		t.Fatalf("develop off: %s", got)
	}
	got = officeToolsText([]string{features.Monitor})
	if !strings.Contains(got, "ops_overview (") || !strings.Contains(got, "monitor_detail to read") || strings.Contains(got, "process_logs") || strings.Contains(got, "propose_action to PROPOSE") {
		t.Fatalf("monitor only: %s", got)
	}
}
