package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// maybeAnalyse asks the project's lead agent why a monitor went down, if AI
// is enabled for it, within its daily cap and a cooldown. It runs in the
// background and fills the event's analysis.
func (s *Service) maybeAnalyse(m storage.Monitor, ev storage.MonitorEvent) {
	if !m.AIEnabled || s.engine == nil {
		return
	}
	ctx := actor.With(context.Background(), "monitor:"+m.Name)
	skip := func(reason string) {
		ev.AnalysisStatus, ev.Analysis = "skipped", reason
		_ = s.store.Monitors().UpdateEvent(ctx, ev)
	}
	// someone stopped it on purpose: nothing to analyse
	if m.Type == "process" && s.ops != nil {
		if st := s.ops.State(m.Target).Status; st == "stopped" || st == "stopping" {
			skip("Không phân tích: tiến trình được dừng chủ động.")
			return
		}
	}
	spent, _ := s.store.Monitors().AICostSince(ctx, m.ID, s.now().Add(-24*time.Hour))
	if m.AIBudgetUSD > 0 && spent >= m.AIBudgetUSD {
		skip(fmt.Sprintf("Không phân tích: đã dùng $%.3f/%.2f trần AI 24 giờ của giám sát này.", spent, m.AIBudgetUSD))
		return
	}
	if recent, err := s.store.Monitors().Events(ctx, m.ProjectID, 50); err == nil {
		for _, e := range recent {
			if e.ID != ev.ID && e.MonitorID == m.ID && e.AnalysisStatus == "done" && s.now().Sub(e.At) < aiCooldown {
				skip("Không phân tích lại: đã phân tích trong 30 phút gần đây.")
				return
			}
		}
	}
	project, err := s.store.Repos().Get(ctx, m.ProjectID)
	if err != nil {
		return
	}
	d, err := s.engine.DefaultAgent(ctx, m.ProjectID)
	var lead *storage.Agent
	if err == nil && d.Disabled { // a paused agent does no work
		skip("Không phân tích: " + storage.OffNotice(d.Name))
		return
	}
	if err == nil {
		lead = &d
	}
	if err != nil || lead == nil {
		skip("Không phân tích: project chưa có mô hình tổ chức (cần agent lead).")
		return
	}
	ev.AnalysisStatus = "running"
	_ = s.store.Monitors().UpdateEvent(ctx, ev)

	go func() {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		res, err := s.engine.Invoke(ctx, project, *lead, s.prompt(ctx, m), nil, "monitor", nil)
		if err != nil && strings.TrimSpace(res.Text) == "" {
			ev.AnalysisStatus, ev.Analysis = "failed", "Phân tích lỗi: "+err.Error()
		} else {
			ev.AnalysisStatus, ev.Analysis = "done", res.Text
		}
		if res.CostUSD != nil {
			ev.CostUSD = *res.CostUSD
		}
		_ = s.store.Monitors().UpdateEvent(context.Background(), ev)
	}()
}

func (s *Service) prompt(ctx context.Context, m storage.Monitor) string {
	var checks []string
	if list, err := s.store.Monitors().Checks(ctx, m.ID, s.now().Add(-2*time.Hour)); err == nil && len(list) > 0 {
		for _, c := range list[max(0, len(list)-15):] {
			state := "OK"
			if !c.OK {
				state = "FAIL"
			}
			checks = append(checks, fmt.Sprintf("%s %s %dms %s", c.At.Local().Format("15:04:05"), state, c.LatencyMS, c.Message))
		}
	}
	logs := ""
	switch m.Type {
	case "process":
		if s.ops != nil {
			logs = s.ops.Tail(m.Target, 150)
		}
	case "container":
		if s.ops != nil {
			logs, _ = s.ops.ComposeTail(ctx, m.ProjectID, m.Config.File, m.Target, 150)
		}
	}
	if strings.TrimSpace(logs) == "" {
		logs = ""
	}
	return prompts.Render("monitor/analyse", map[string]any{"Name": m.Name, "Type": m.Type, "Target": m.Target, "Interval": m.IntervalS,
		"Last": m.LastMessage, "Checks": checks, "Logs": logs})
}
