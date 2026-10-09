package trigger

import (
	"context"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Quest is a piece of work a script gives the project's Burn (ADR-132): a line
// "@@quest <title>", or "@@quest <title> :: <detail>".
type Quest struct{ Title, Detail string }

// What a quest may carry (the Burn's own caps): longer is cut.
const maxQuestTitle, maxQuestDetail = 160, 4000

// GiveQuests hands quests to a project's Burn, leaving out the ones it has
// open already; with start, a stopped Burn starts once any was added. who
// names the starter. The office wires it to the Burn service (nil = no Burn).
type GiveQuests func(ctx context.Context, projectID string, qs []Quest, start bool, who string) (added int, started bool, err error)

// SetQuests sets where a script's @@quest lines go.
func (r *Runner) SetQuests(fn GiveQuests) { r.quests = fn }

// Quests are the @@quest lines of a script's output, each title once (case
// and spaces aside).
func Quests(output string) []Quest {
	var out []Quest
	seen := map[string]bool{}
	for _, l := range strings.Split(output, "\n") {
		rest, ok := questLine(l)
		if !ok {
			continue
		}
		title, detail, _ := strings.Cut(rest, " :: ")
		title = cutRunes(strings.Join(strings.Fields(title), " "), maxQuestTitle)
		k := strings.ToLower(title)
		if title == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, Quest{Title: title, Detail: cutRunes(strings.TrimSpace(detail), maxQuestDetail)})
	}
	return out
}

// questLine is what follows "@@quest" on a line that is one ("@@quest:" too).
func questLine(l string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(l), "@@quest")
	if !ok || rest != "" && rest[0] != ' ' && rest[0] != '\t' && rest[0] != ':' {
		return "", false // @@questions is not one
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, ":")), true
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n]))
	}
	return s
}

// giveQuests hands the run's @@quest lines to the Burn and says how it went
// (a line for the job's output; "" = no quest). Only a schedule or a run by
// hand starts the Burn: what a webhook or a bot's message drives never does
// (it works with full access, ADR-074).
func (r *Runner) giveQuests(ctx context.Context, a storage.Automation, j storage.Job, out string) string {
	qs := Quests(out)
	if len(qs) == 0 {
		return ""
	}
	if r.quests == nil {
		return fmt.Sprintf("Office chưa bật Burn: bỏ qua %d quest", len(qs))
	}
	trusted := j.Trigger == "schedule" || j.Trigger == "manual" && j.Payload == ""
	added, started, err := r.quests(ctx, a.ProjectID, qs, a.Script.BurnStart && trusted, "auto:"+a.Name)
	msg := fmt.Sprintf("Đã thêm %d quest cho Burn", added)
	if n := len(qs) - added; n > 0 {
		msg += fmt.Sprintf(" (bỏ qua %d đã có)", n)
	}
	switch {
	case started:
		msg += "; Burn đã bật"
	case a.Script.BurnStart && added > 0 && !trusted:
		msg += "; Burn không tự bật từ webhook hay tin nhắn kênh"
	}
	if err != nil {
		msg += "; lỗi: " + err.Error()
	}
	return msg
}
