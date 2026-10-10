package burn

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Pieces on the same files (ADR-144): two at once clash when merged into the
// run, and a piece reviewed without what was agreed on its file before may
// undo it. A piece names its files in its brief ("Where: file:line"); once
// merged, it has the files it touched.

// pathRe is a file path as a piece's title or brief writes it.
var pathRe = regexp.MustCompile(`(?:[\w@.-]+/)*[\w@-][\w@.-]*\.(?:go|vue|ts|tsx|js|jsx|mjs|cjs|md|json|ya?ml|css|scss|sql|sh|py|toml|html)\b`)

// paths are the files a piece touched or names.
func paths(it storage.BurnItem) []string {
	out := slices.Clone(it.Files)
	for _, m := range pathRe.FindAllString(it.Title+"\n"+it.Detail, -1) {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

// samePath: a and b are one file, one of them maybe written short (its name,
// or the end of its path).
func samePath(a, b string) bool {
	if len(a) < len(b) {
		a, b = b, a
	}
	return a == b || strings.HasSuffix(a, "/"+b)
}

// overlap: a and b share a file.
func overlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if samePath(x, y) {
				return true
			}
		}
	}
	return false
}

// clashes: a piece being worked on now is on one of its files; it waits
// for that one, not to clash with it when merged.
func clashes(it storage.BurnItem, items []storage.BurnItem, busy map[string]bool) bool {
	mine := paths(it)
	if len(mine) == 0 {
		return false
	}
	for _, x := range items {
		if x.ID != it.ID && busy[x.ID] && !hunting(x) && overlap(mine, paths(x)) {
			return true
		}
	}
	return false
}

// maxRelated: how many earlier pieces on the same files a review is shown.
const maxRelated = 5

// related are the other pieces of this run on its files, latest first,
// with what came of them: a review keeps to what was agreed there.
func related(b storage.BurnSession, it storage.BurnItem, items []storage.BurnItem) []string {
	mine := paths(it)
	if len(mine) == 0 {
		return nil
	}
	var out []string
	for _, x := range slices.Backward(items) {
		if x.ID == it.ID || hunting(x) || x.RunBranch != b.RunBranch || x.Status == "found" || !overlap(mine, paths(x)) {
			continue
		}
		line := fmt.Sprintf("[%s] %s", x.Status, oneLine(x.Title, 120))
		if note := firstNonEmpty(x.ReviewNote, x.Summary); note != "" && !openStatus[x.Status] {
			line += " — " + oneLine(note, 300)
		}
		if out = append(out, line); len(out) == maxRelated {
			break
		}
	}
	return out
}

// maxChanged: how many files a scan is told the run changed.
const maxChanged = 30

// changed are the files this run's finished pieces changed, each with the
// pieces that did: a scan reads them as they are now before finding fault.
func changed(b storage.BurnSession, items []storage.BurnItem) []string {
	by := map[string][]string{}
	var order []string
	for _, x := range items {
		if x.Status != "done" || x.RunBranch != b.RunBranch {
			continue
		}
		for _, f := range x.Files {
			if _, ok := by[f]; !ok {
				order = append(order, f)
			}
			by[f] = append(by[f], oneLine(x.Title, 80))
		}
	}
	if len(order) > maxChanged {
		order = order[len(order)-maxChanged:]
	}
	out := make([]string, 0, len(order))
	for _, f := range order {
		out = append(out, f+": "+strings.Join(by[f], " | "))
	}
	return out
}
