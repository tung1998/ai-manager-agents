// Package officetools gives agents read-only access to what office knows
// about a project at run time: processes (build/dev/test), docker compose
// services, health checks and their incidents. The same tools are served to
// Claude Code over MCP (internal/mcpserver) and to API agents directly.
package officetools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ops"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Tool is one callable tool.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}

// Toolbox runs the tools for one project at a time.
type Toolbox struct {
	store storage.Store
	ops   *ops.Manager
}

// New builds a Toolbox; ops may be nil (processes/containers unavailable).
func New(store storage.Store, o *ops.Manager) *Toolbox { return &Toolbox{store: store, ops: o} }

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var linesProp = map[string]any{"type": "integer", "description": "Số dòng log cuối (mặc định 200, tối đa 1000)"}

// Tools lists the tools (names are stable: agents and the UI refer to them).
func (t *Toolbox) Tools() []Tool {
	return []Tool{
		{Name: "ops_overview", Description: "Tổng quan vận hành của project: các tiến trình (dev, build, test…) và trạng thái/mã thoát/cổng, các service docker compose, các giám sát (Up/Down) và sự cố gần đây. Gọi đầu tiên khi được hỏi về lỗi build, lỗi chạy, deploy hay giám sát.",
			Schema: obj(map[string]any{})},
		{Name: "process_logs", Description: "Đọc log gần nhất của một tiến trình office chạy cho project (ví dụ dev, build, test), kèm lệnh, trạng thái và mã thoát.",
			Schema: obj(map[string]any{"name": map[string]any{"type": "string", "description": "Tên tiến trình, xem ops_overview"}, "lines": linesProp}, "name")},
		{Name: "container_logs", Description: "Đọc log gần nhất của một service docker compose của project, kèm trạng thái container.",
			Schema: obj(map[string]any{"service": map[string]any{"type": "string"}, "lines": linesProp}, "service")},
		{Name: "monitor_detail", Description: "Chi tiết một giám sát: cấu hình, các lần kiểm tra gần đây, sự kiện Up/Down và phân tích AI trước đó.",
			Schema: obj(map[string]any{"name": map[string]any{"type": "string", "description": "Tên giám sát, xem ops_overview"}}, "name")},
	}
}

// Has reports whether name is one of the tools.
func (t *Toolbox) Has(name string) bool {
	for _, x := range t.Tools() {
		if x.Name == name {
			return true
		}
	}
	return false
}

// Call runs a tool for a project and returns text (and whether it failed).
func (t *Toolbox) Call(ctx context.Context, projectID, name string, raw json.RawMessage) (string, bool) {
	var in struct {
		Name    string `json:"name"`
		Service string `json:"service"`
		Lines   int    `json:"lines"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "Tham số không hợp lệ: " + err.Error(), true
		}
	}
	if in.Lines <= 0 {
		in.Lines = 200
	}
	in.Lines = min(in.Lines, 1000)
	var (
		out string
		err error
	)
	switch name {
	case "ops_overview":
		out, err = t.overview(ctx, projectID)
	case "process_logs":
		out, err = t.processLogs(ctx, projectID, in.Name, in.Lines)
	case "container_logs":
		out, err = t.containerLogs(ctx, projectID, in.Service, in.Lines)
	case "monitor_detail":
		out, err = t.monitorDetail(ctx, projectID, in.Name)
	default:
		return "Công cụ không tồn tại: " + name, true
	}
	if err != nil {
		return "Lỗi: " + err.Error(), true
	}
	return out, false
}

func when(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Local().Format("02/01 15:04:05")
}

func (t *Toolbox) overview(ctx context.Context, projectID string) (string, error) {
	var b strings.Builder
	procs, err := t.store.Processes().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	b.WriteString("## Tiến trình\n")
	if len(procs) == 0 {
		b.WriteString("(chưa có)\n")
	}
	for _, p := range procs {
		line := fmt.Sprintf("- %s [%s] `%s`", p.Name, p.Kind, p.Command)
		if t.ops != nil {
			st := t.ops.State(p.ID)
			line += " — " + st.Status
			if st.ExitCode != nil {
				line += fmt.Sprintf(", mã thoát %d", *st.ExitCode)
			}
			if st.Port > 0 {
				line += fmt.Sprintf(", cổng %d", st.Port)
			}
			if st.FinishedAt != nil && st.Status != "running" {
				line += ", kết thúc " + when(st.FinishedAt)
			}
		}
		b.WriteString(line + "\n")
	}
	if t.ops != nil {
		if v, err := t.ops.Compose(ctx, projectID, "", false); err == nil && len(v.Files) > 0 {
			fmt.Fprintf(&b, "\n## Docker compose (%s)\n", v.File)
			if v.Docker.Error != "" {
				b.WriteString("Docker: " + v.Docker.Error + "\n")
			}
			for _, s := range v.Services {
				if s.Container == nil {
					fmt.Fprintf(&b, "- %s — chưa tạo container\n", s.Name)
					continue
				}
				fmt.Fprintf(&b, "- %s — %s (%s)\n", s.Name, s.Container.State, s.Container.Status)
			}
		}
	}
	mons, err := t.store.Monitors().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	b.WriteString("\n## Giám sát\n")
	if len(mons) == 0 {
		b.WriteString("(chưa có)\n")
	}
	for _, m := range mons {
		status := m.Status
		if !m.Enabled {
			status = "tạm dừng"
		}
		fmt.Fprintf(&b, "- %s [%s %s] — %s: %s (kiểm tra %s)\n", m.Name, m.Type, m.Target, status, m.LastMessage, when(m.LastCheckedAt))
	}
	evs, err := t.store.Monitors().Events(ctx, projectID, 10)
	if err == nil && len(evs) > 0 {
		names := map[string]string{}
		for _, m := range mons {
			names[m.ID] = m.Name
		}
		b.WriteString("\n## Sự kiện gần đây\n")
		for _, e := range evs {
			fmt.Fprintf(&b, "- %s %s %s: %s\n", e.At.Local().Format("02/01 15:04"), strings.ToUpper(e.Kind), names[e.MonitorID], e.Message)
		}
	}
	return b.String(), nil
}

func (t *Toolbox) processLogs(ctx context.Context, projectID, name string, lines int) (string, error) {
	if t.ops == nil {
		return "", errors.New("office không quản lý tiến trình")
	}
	procs, err := t.store.Processes().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, p := range procs {
		if strings.EqualFold(p.Name, name) || p.ID == name {
			st := t.ops.State(p.ID)
			head := fmt.Sprintf("Tiến trình %s: `%s` (thư mục %s) — %s", p.Name, p.Command, p.Cwd, st.Status)
			if st.ExitCode != nil {
				head += fmt.Sprintf(", mã thoát %d", *st.ExitCode)
			}
			tail := t.ops.Tail(p.ID, lines)
			if strings.TrimSpace(tail) == "" {
				tail = "(chưa có log trong phiên này của office)"
			}
			return head + "\n\n```\n" + tail + "```", nil
		}
	}
	return "", fmt.Errorf("không có tiến trình %q; xem ops_overview", name)
}

func (t *Toolbox) containerLogs(ctx context.Context, projectID, service string, lines int) (string, error) {
	if t.ops == nil {
		return "", errors.New("office không quản lý container")
	}
	c, err := t.ops.ServiceContainer(ctx, projectID, "", service)
	if err != nil {
		return "", err
	}
	head := "Service " + service + ": chưa tạo container"
	if c != nil {
		head = fmt.Sprintf("Service %s: %s (%s), image %s", service, c.State, c.Status, c.Image)
	}
	tail, err := t.ops.ComposeTail(ctx, projectID, "", service, lines)
	if err != nil {
		return head + "\n(không đọc được log: " + err.Error() + ")", nil
	}
	return head + "\n\n```\n" + tail + "```", nil
}

func (t *Toolbox) monitorDetail(ctx context.Context, projectID, name string) (string, error) {
	mons, err := t.store.Monitors().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, m := range mons {
		if !strings.EqualFold(m.Name, name) && m.ID != name {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Giám sát %s [%s] đích %s, mỗi %ds — %s: %s\n", m.Name, m.Type, m.Target, m.IntervalS, m.Status, m.LastMessage)
		if checks, err := t.store.Monitors().Checks(ctx, m.ID, time.Now().Add(-6*time.Hour)); err == nil && len(checks) > 0 {
			b.WriteString("\nKiểm tra gần đây:\n")
			for _, c := range checks[max(0, len(checks)-20):] {
				ok := "OK"
				if !c.OK {
					ok = "LỖI"
				}
				fmt.Fprintf(&b, "- %s %s %dms %s\n", c.At.Local().Format("15:04:05"), ok, c.LatencyMS, c.Message)
			}
		}
		if evs, err := t.store.Monitors().Events(ctx, projectID, 50); err == nil {
			n := 0
			for _, e := range evs {
				if e.MonitorID != m.ID || n >= 5 {
					continue
				}
				n++
				fmt.Fprintf(&b, "\nSự kiện %s %s: %s\n", e.At.Local().Format("02/01 15:04"), strings.ToUpper(e.Kind), e.Message)
				if e.AnalysisStatus == "done" && e.Analysis != "" {
					fmt.Fprintf(&b, "Phân tích trước đó:\n%s\n", e.Analysis)
				}
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("không có giám sát %q; xem ops_overview", name)
}
