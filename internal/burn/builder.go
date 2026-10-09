package burn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// The builder template (ADR-139) builds a project from an idea, the way
// long-running agent harnesses do: the first scan finds no features.json and
// records one piece, the start (spec, feature list, map, progress log, a
// scaffold whose build/test pass); later scans record the next features not
// passing yet, in order; a worker does one feature, runs its acceptance and
// marks it passing. The repo's files are the memory between turns, and the
// Burn's checks run every passing feature's acceptance again, so a feature
// broken later sends its piece back.

// featuresFile is the feature list at the project's root.
const featuresFile = "features.json"

// maxAcceptance caps the acceptance commands the checks run.
const maxAcceptance = 50

// feature is one entry of features.json.
type feature struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Acceptance string `json:"acceptance"` // a shell command: exit 0 = it works
	Passes     bool   `json:"passes"`
}

// readFeatures reads dir's feature list: a list, or {"features": [...]};
// none or unreadable: none.
func readFeatures(dir string) []feature {
	raw, err := os.ReadFile(filepath.Join(dir, featuresFile))
	if err != nil {
		return nil
	}
	var list []feature
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var wrapped struct {
		Features []feature `json:"features"`
	}
	if json.Unmarshal(raw, &wrapped) == nil {
		return wrapped.Features
	}
	return nil
}

// acceptance are the acceptance commands of the features passing in dir,
// each once, in the list's order.
func acceptance(dir string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range readFeatures(dir) {
		c := strings.TrimSpace(f.Acceptance)
		if !f.Passes || c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
		if len(out) == maxAcceptance {
			break
		}
	}
	return out
}

// builderCheck is how a builder piece is done and checked (burn/work.md).
const builderCheck = "this Burn builds the project feature by feature (features.json, PROGRESS.md at the root). " +
	"Do ONLY the one feature this piece names (or the start, if it says so); read PROGRESS.md and features.json first. " +
	"When it works, run its acceptance command yourself, then set its \"passes\" to true (never for another feature, never without running it), " +
	"add what you did and what is next to PROGRESS.md, and put what you learned (a pitfall, a command) in your burn_done summary. " +
	"Office runs the acceptance of every passing feature again: one you broke sends the piece back"

// parallel is how many pieces b does at a time: a builder one, as each
// feature runs on those before it and all write features.json, PROGRESS.md.
func parallel(b storage.BurnSession) int {
	if templateOf(b) == "builder" {
		return 1
	}
	return max(b.MaxParallel, 1)
}

// builderBusy: a builder Burn with a piece not finished, so no scan: the
// next features are read from features.json once that piece is merged.
func builderBusy(b storage.BurnSession, items []storage.BurnItem) bool {
	if templateOf(b) != "builder" {
		return false
	}
	for _, it := range items {
		if !hunting(it) && openStatus[it.Status] {
			return true
		}
	}
	return false
}
