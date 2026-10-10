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
	go s.retro(context.WithoutCancel(ctx), b) // what went wrong, what to change (ADR-143)
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
	if len(done) > 0 && b.RunBranch != "" { // the run's one result (ADR-123)
		fmt.Fprintf(&sb, "Kết quả: nhánh `%s` (worktree của lần chạy, review ở đó rồi merge)\n", b.RunBranch)
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
	// to read with care (ADR-131): what it did where a mistake costs most
	var careful []storage.BurnItem
	for _, it := range done {
		if len(risks(it.Files)) > 0 {
			careful = append(careful, it)
		}
	}
	part("Cần đọc kỹ", careful, func(it storage.BurnItem) string {
		return oneLine(it.Title, 120) + " — " + strings.Join(risks(it.Files), ", ")
	})
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

// riskAreas: where in a project a change wants a careful read, by what its
// path holds (lower case), first match wins.
var riskAreas = []struct{ label, words string }{
	{"migration/dữ liệu", "migration,migrate,.sql,schema"},
	{"bảo mật/quyền", "auth,perm,secret,token,password,crypto,oauth,session,guard,security,acl"},
	{"thanh toán", "payment,billing,checkout,stripe,paypal,invoice"},
	{"phụ thuộc", "go.mod,go.sum,package.json,pnpm-lock,package-lock,yarn.lock,cargo.toml,requirements.txt"},
	{"triển khai/CI", "dockerfile,docker-compose,compose.y,.github/,.gitlab-ci,deploy,makefile"},
	{"cấu hình", ".env,config.y,settings.json"},
}

// risks are the risk areas files touch, each once.
func risks(files []string) []string {
	var out []string
	for _, a := range riskAreas {
		hit := false
		for _, f := range files {
			f = strings.ToLower(f)
			for _, w := range strings.Split(a.words, ",") {
				if strings.Contains(f, w) {
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
		if hit {
			out = append(out, a.label)
		}
	}
	return out
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

// say writes a line of the Burn's log in its chat: what it starts, finishes,
// waits for (no AI, it costs nothing), so the chat shows what goes on.
func (s *Service) say(ctx context.Context, b storage.BurnSession, text string) {
	if b.ConversationID == "" {
		return
	}
	_, _ = s.store.Chat().AddMessage(context.WithoutCancel(ctx), storage.Message{ConversationID: b.ConversationID, Role: "assistant", Author: "Burn", Content: text})
}

// sayItem logs where a piece got to; nothing for a piece still in progress.
func (s *Service) sayItem(ctx context.Context, b storage.BurnSession, it storage.BurnItem) {
	title := oneLine(it.Title, 120)
	why := ""
	if it.Summary != "" {
		why = ": " + oneLine(it.Summary, 300)
	}
	switch it.Status {
	case "done":
		s.say(ctx, b, "**Xong** "+title+why+s.progress(ctx, b))
	case "failed":
		s.say(ctx, b, "**Thất bại** "+title+why)
	case "skipped":
		s.say(ctx, b, "**Bỏ qua** "+title+why)
	case "review":
		s.say(ctx, b, "**Chờ review kết quả** "+title)
	case "queued":
		if it.Attempts > 0 {
			s.say(ctx, b, "**Làm lại sau** "+title+why)
		}
	}
}

// progress is where the run is, after a piece done: " · Tiến độ: xong N, còn M"
// (M the pieces open: waiting, being done or reviewed); "" when unknown.
func (s *Service) progress(ctx context.Context, b storage.BurnSession) string {
	items, err := s.store.Burn().Items(context.WithoutCancel(ctx), b.ID)
	if err != nil {
		return ""
	}
	done, open := 0, 0
	for _, it := range items {
		switch {
		case hunting(it):
		case it.Status == "done" && it.RunBranch == b.RunBranch:
			done++
		case openStatus[it.Status]:
			open++
		}
	}
	return fmt.Sprintf("\n_Tiến độ: xong %d, còn %d_", done, open)
}

// sayStart is the first line of a run: how it runs, until when.
func (s *Service) sayStart(ctx context.Context, b storage.BurnSession) {
	agent := "agent mặc định"
	if a, err := s.store.Agents().Get(ctx, b.AgentID); err == nil {
		agent = a.Name
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Burn bắt đầu** · mẫu %s · %s · tối đa %d việc song song", templateLabel[templateOf(b)], agent, parallel(b))
	if b.EndsAt != nil {
		fmt.Fprintf(&sb, " · tắt lúc %s", b.EndsAt.Local().Format("02/01 15:04"))
	}
	if b.RunBranch != "" {
		fmt.Fprintf(&sb, " · nhánh `%s`", b.RunBranch)
	}
	if b.Focus != "" {
		sb.WriteString("\nTrọng tâm: " + oneLine(b.Focus, 300))
	}
	s.say(ctx, b, sb.String())
}
