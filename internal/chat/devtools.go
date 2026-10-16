package chat

import (
	"strings"

	"bitbucket.org/senprints/agent-office/internal/features"
)

// devTools tells the agent the office tools of the project's features on
// (git, Vận hành); "" when none is.
func devTools(on []string) string {
	has := func(f string) bool { return features.Has(on, f) }
	var read []string
	if has(features.Ops) {
		read = append(read, "ops_overview (build/dev/test processes, docker compose, monitors, incidents)", "process_logs", "container_logs", "monitor_detail")
	}
	var parts []string
	if len(read) > 0 {
		parts = append(parts, andList(read)+" to read")
	}
	if has(features.Git) {
		parts = append(parts, "git_status/git_diff/git_log for git")
	}
	var acts []string
	if has(features.Ops) {
		acts = append(acts, "running/restarting/stopping a process or container")
	}
	if has(features.Git) {
		acts = append(acts, "a commit, a new branch or a push")
	}
	if len(acts) > 0 {
		note := "the person approves, then office does it, unless the permission pack lets it run on its own"
		if has(features.Git) {
			note += "; a push always needs approval"
		}
		parts = append(parts, "propose_action to PROPOSE "+strings.Join(acts, ", ")+" ("+note+")")
	}
	if len(parts) > 1 {
		parts[len(parts)-1] = "and " + parts[len(parts)-1]
	}
	return strings.Join(parts, "; ")
}

// andList: "a, b and c".
func andList(xs []string) string {
	if len(xs) == 1 {
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}
