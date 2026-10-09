package burn

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// maxReported caps the pieces listed in each part of a run's summary.
const maxReported = 15

// report closes a run of the Burn (stopped by hand, its time up, or done
// finishing what it had): what it did, as the last message of its chat, and
// to the bot's chat it names (ADR-120). No AI: it costs nothing.
func (s *Service) report(ctx context.Context, b storage.BurnSession, why string) {
	items, _ := s.store.Burn().Items(ctx, b.ID)
	project := b.ProjectID
	if p, err := s.store.Repos().Get(ctx, b.ProjectID); err == nil {
		project = p.Name
	}
	text := summary(b, items, project, why, time.Now())
	if b.ConversationID != "" {
		_, _ = s.store.Chat().AddMessage(ctx, storage.Message{ConversationID: b.ConversationID, Role: "assistant", Author: "Burn", Content: text})
	}
	if s.notify != nil && b.NotifyChannelID != "" && b.NotifyChatID != "" {
		go func() {
			if err := s.notify(ctx, b.NotifyChannelID, b.NotifyChatID, text); err != nil {
				slog.Warn("burn: summary to the bot", "project", b.ProjectID, "err", err)
			}
		}()
	}
}

// summary is a run's report: since it started, what was done, failed or
// skipped; what is left in progress or waiting.
func summary(b storage.BurnSession, items []storage.BurnItem, project, why string, now time.Time) string {
	var start time.Time
	if b.StartedAt != nil {
		start = *b.StartedAt
	}
	var done, failed, skipped, left []storage.BurnItem
	queued, found := 0, 0
	cost := 0.0
	for _, it := range items {
		thisRun := !it.UpdatedAt.Before(start)
		switch it.Status {
		case "done", "failed", "skipped":
			if !thisRun {
				continue
			}
			switch it.Status {
			case "done":
				done = append(done, it)
			case "failed":
				failed = append(failed, it)
			default:
				skipped = append(skipped, it)
			}
		case "doing", "paused", "review":
			left = append(left, it)
		case "queued":
			queued++
		case "found":
			found++
		}
		if thisRun {
			cost += it.CostUSD
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Burn %s đã dừng** (%s)\n", project, why)
	if !start.IsZero() {
		fmt.Fprintf(&sb, "Chạy %s (%s → %s)", span(now.Sub(start)), start.Local().Format("02/01 15:04"), now.Local().Format("02/01 15:04"))
	}
	if cost > 0 {
		fmt.Fprintf(&sb, " · chi phí các việc $%.2f", cost)
	}
	sb.WriteString("\n")
	switch { // the run's one result (ADR-123)
	case len(done) == 0:
	case b.ResultMode == "worktree":
		sb.WriteString("Kết quả: một diff trong chat Burn, duyệt để đưa vào nhánh hiện tại\n")
	case b.RunBranch != "":
		fmt.Fprintf(&sb, "Kết quả: nhánh `%s`\n", b.RunBranch)
	}
	if len(done)+len(failed)+len(skipped)+len(left) == 0 {
		sb.WriteString("\nLần chạy này chưa xong việc nào.\n")
	}
	part := func(title string, list []storage.BurnItem, line func(storage.BurnItem) string) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(&sb, "\n%s (%d):\n", title, len(list))
		for i, it := range list {
			if i == maxReported {
				fmt.Fprintf(&sb, "- … và %d việc nữa\n", len(list)-maxReported)
				break
			}
			sb.WriteString("- " + line(it) + "\n")
		}
	}
	part("Xong", done, func(it storage.BurnItem) string {
		l := oneLine(it.Title, 120)
		if it.Summary != "" {
			l += ": " + oneLine(it.Summary, 200)
		}
		return l
	})
	reason := func(it storage.BurnItem) string {
		if it.Summary == "" {
			return oneLine(it.Title, 120)
		}
		return oneLine(it.Title, 120) + ": " + oneLine(it.Summary, 160)
	}
	part("Thất bại", failed, reason)
	part("Bỏ qua", skipped, reason)
	part("Còn dở (bật lại thì làm tiếp)", left, func(it storage.BurnItem) string {
		return oneLine(it.Title, 120) + " [" + it.Status + "]"
	})
	if queued+found > 0 {
		fmt.Fprintf(&sb, "\nCòn %d việc đã chọn chờ làm, %d việc tìm thấy chưa chọn.\n", queued, found)
	}
	return strings.TrimSpace(sb.String())
}

// span is a duration as people read it: "2 giờ 15 phút".
func span(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	switch {
	case m < 1:
		return "dưới 1 phút"
	case m < 60:
		return fmt.Sprintf("%d phút", m)
	case m%60 == 0:
		return fmt.Sprintf("%d giờ", m/60)
	}
	return fmt.Sprintf("%d giờ %d phút", m/60, m%60)
}
