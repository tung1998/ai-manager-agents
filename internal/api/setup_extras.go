package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/automation"
	"bitbucket.org/senprints/agent-office/internal/mcpgateway"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/setup"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// setupToolbox is what the setup may pick beyond the packs: the office
// library's skills, and MCP servers from the catalog and the library.
func setupToolbox(a *automation.Service) func() setup.Toolbox {
	return func() setup.Toolbox {
		box := setup.Toolbox{Skills: []setup.Tool{}, MCP: []setup.Tool{}}
		if list, err := a.Library.List("skill"); err == nil {
			for _, it := range list {
				box.Skills = append(box.Skills, setup.Tool{Name: it.Name, Description: it.Description})
			}
		}
		for _, t := range setupTemplates(a) {
			box.MCP = append(box.MCP, setup.Tool{Name: t.Name, Description: t.Description, Needs: templateNeeds(t)})
		}
		return box
	}
}

// setupTemplates: the library's MCP templates, then the catalog's (a
// library one wins on a name clash).
func setupTemplates(a *automation.Service) []automation.MCPTemplate {
	var out []automation.MCPTemplate
	if list, err := a.Library.List("mcp"); err == nil {
		for _, it := range list {
			if it.Template != nil && mcpgateway.ValidName(it.Name) {
				t := *it.Template
				t.Name = it.Name
				out = append(out, t)
			}
		}
	}
	for _, t := range automation.Catalog() {
		if !slices.ContainsFunc(out, func(x automation.MCPTemplate) bool { return x.Name == t.Name }) {
			out = append(out, t)
		}
	}
	return out
}

// templateNeeds: "key" when a required input has no default, "login" for
// OAuth, "" when it works as is.
func templateNeeds(t automation.MCPTemplate) string {
	for _, in := range t.Inputs {
		if in.Required && in.Default == "" {
			return "key"
		}
	}
	if t.Auth == "oauth" {
		return "login"
	}
	return ""
}

// ReadyItem is one line of the checklist shown after the setup is applied.
type ReadyItem struct {
	Kind   string `json:"kind"`   // agents | workflows | skill | mcp | quick_check | command
	Name   string `json:"name"`   // what it is about
	Status string `json:"status"` // ok | todo | error
	Detail string `json:"detail,omitempty"`
}

// setupExtras installs what the person kept beyond agents and workflows
// (skills, MCP servers, quick checks) and reports each one; nothing here
// blocks the setup, a failure is a line on the checklist.
func (s *server) setupExtras(r *http.Request, x storage.Repo, in setupChoice) []ReadyItem {
	ctx := r.Context()
	items := []ReadyItem{}
	for _, name := range cleanList(in.Skills) {
		items = append(items, s.setupSkill(ctx, x, name))
	}
	if len(in.MCP) > 0 {
		var ids []string
		if agents, err := s.cfg.Store.Agents().List(ctx, x.ID); err == nil {
			for _, a := range agents {
				ids = append(ids, a.ID)
			}
		}
		for _, name := range cleanList(in.MCP) {
			items = append(items, s.setupMCP(r, name, ids))
		}
	}
	if in.QuickChecks != nil {
		items = append(items, s.setupQuickChecks(r, x.ID, strings.TrimSpace(*in.QuickChecks)))
	}
	return items
}

func (s *server) setupSkill(ctx context.Context, x storage.Repo, name string) ReadyItem {
	it := ReadyItem{Kind: "skill", Name: name}
	if s.cfg.Automation == nil || x.Path == "" {
		it.Status, it.Detail = "todo", "project không có thư mục: cài skill ở trang Tự động hóa"
		return it
	}
	res, err := s.cfg.Automation.Install(ctx, automation.InstallRequest{Kind: "skill", Library: name,
		Target: automation.Target{Scope: "project", ProjectPath: x.Path}})
	switch {
	case errors.Is(err, automation.ErrExists):
		it.Status, it.Detail = "ok", "đã có trong project"
	case errors.Is(err, automation.ErrNeedsConsent), errors.Is(err, automation.ErrUnsafeSkill):
		it.Status, it.Detail = "todo", "nội dung có cảnh báo an toàn: xem và cài tay ở trang Tự động hóa"
	case err != nil:
		it.Status, it.Detail = "error", err.Error()
	default:
		it.Status, it.Detail = "ok", res.Path
	}
	return it
}

// setupMCP adds a server to office's MCP gateway for the project's agents.
// A server already there is left as it is (its agents get the project's
// added when it is limited to some); one that needs a key is not added.
func (s *server) setupMCP(r *http.Request, name string, agentIDs []string) ReadyItem {
	ctx := r.Context()
	it := ReadyItem{Kind: "mcp", Name: name}
	if s.cfg.Automation == nil || s.cfg.Store == nil {
		it.Status, it.Detail = "error", "quản lý MCP đang tắt"
		return it
	}
	servers, err := s.cfg.Store.MCPServers().List(ctx)
	if err != nil {
		it.Status, it.Detail = "error", err.Error()
		return it
	}
	for _, m := range servers {
		if m.Name != name {
			continue
		}
		if len(m.Agents) > 0 {
			before := m
			for _, id := range agentIDs {
				if !slices.Contains(m.Agents, id) {
					m.Agents = append(m.Agents, id)
				}
			}
			if len(m.Agents) != len(before.Agents) {
				if err := s.cfg.Store.MCPServers().Update(ctx, m); err != nil {
					it.Status, it.Detail = "error", err.Error()
					return it
				}
				s.audit(r, audit.Change{Action: "mcp_server.update", ResourceID: m.ID, Before: s.auditMCP(before), After: s.auditMCP(m)})
			}
		}
		return mcpReady(it, m)
	}
	var tpl *automation.MCPTemplate
	for _, t := range setupTemplates(s.cfg.Automation) {
		if t.Name == name {
			tpl = &t
			break
		}
	}
	if tpl == nil {
		it.Status, it.Detail = "error", "không có trong kho MCP"
		return it
	}
	if templateNeeds(*tpl) == "key" {
		var keys []string
		for _, in := range tpl.Inputs {
			if in.Required && in.Default == "" {
				keys = append(keys, in.Label)
			}
		}
		it.Status, it.Detail = "todo", "cần "+strings.Join(keys, ", ")+": thêm ở trang MCP"
		return it
	}
	cfg, err := automation.Render(*tpl, nil)
	if err != nil {
		it.Status, it.Detail = "error", err.Error()
		return it
	}
	m, err := mcpFromConfig(name, cfg)
	if err != nil {
		it.Status, it.Detail = "error", err.Error()
		return it
	}
	m.Agents = agentIDs
	if err := s.applyMCPServer(m.input, &m.MCPServer); err != nil {
		it.Status, it.Detail = "error", err.Error()
		return it
	}
	created, err := s.cfg.Store.MCPServers().Create(ctx, m.MCPServer)
	if err != nil {
		it.Status, it.Detail = "error", err.Error()
		return it
	}
	s.audit(r, audit.Change{Action: "mcp_server.create", ResourceID: created.ID, After: s.auditMCP(created)})
	if s.cfg.Gateway != nil {
		s.checkMCPLater(created)
	}
	it.Status, it.Detail = "ok", "đã thêm vào MCP của office, đang kiểm tra"
	if tpl.Auth == "oauth" {
		it.Status, it.Detail = "todo", "đã thêm: bấm Kết nối ở trang MCP để đăng nhập"
	}
	return it
}

func mcpReady(it ReadyItem, m storage.MCPServer) ReadyItem {
	switch {
	case !m.Enabled:
		it.Status, it.Detail = "todo", "đã có nhưng đang tắt: bật ở trang MCP"
	case m.LastCheckStatus == "needs_login":
		it.Status, it.Detail = "todo", "cần đăng nhập: bấm Kết nối ở trang MCP"
	case m.LastCheckStatus == "error":
		it.Status, it.Detail = "error", "kiểm tra lỗi: "+m.LastCheckError
	default:
		it.Status, it.Detail = "ok", "đã có trong MCP của office"
	}
	return it
}

type mcpNew struct {
	storage.MCPServer
	input mcpServerInput
}

// mcpFromConfig turns a rendered template config into a gateway server
// (machine scope, as the gateway has only that).
func mcpFromConfig(name string, cfg map[string]any) (mcpNew, error) {
	n := mcpNew{MCPServer: storage.MCPServer{Name: name, Scope: "machine", Origin: "manual", Enabled: true}}
	str := func(v any) string { s, _ := v.(string); return s }
	strMap := func(v any) *map[string]string {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		out := map[string]string{}
		for k, x := range m {
			out[k] = str(x)
		}
		return &out
	}
	switch str(cfg["type"]) {
	case "http":
		n.Kind = "http"
		u := str(cfg["url"])
		n.input.URL, n.input.Headers = &u, strMap(cfg["headers"])
	case "stdio", "":
		n.Kind = "stdio"
		c := str(cfg["command"])
		var args []string
		if list, ok := cfg["args"].([]any); ok {
			for _, a := range list {
				args = append(args, str(a))
			}
		}
		n.input.Command, n.input.Args, n.input.Env = &c, &args, strMap(cfg["env"])
	default:
		return n, fmt.Errorf("loại MCP %q chưa hỗ trợ", str(cfg["type"]))
	}
	return n, nil
}

// setupQuickChecks turns quick checks on, and sets the project's own ones
// when it has none yet (never overwriting what a person wrote).
func (s *server) setupQuickChecks(r *http.Request, projectID, checks string) ReadyItem {
	ctx := r.Context()
	it := ReadyItem{Kind: "quick_check", Name: "kiểm tra nhanh"}
	if _, err := perm.ParseQuickChecks(checks); err != nil {
		it.Status, it.Detail = "error", err.Error()
		return it
	}
	pol := perm.LoadPolicy(ctx, s.cfg.Store, projectID)
	before := pol
	pol.QuickCheck = true
	switch {
	case checks == "":
		it.Detail = "bật kiểm tra có sẵn (gofmt/vet, JSON, YAML)"
	case strings.TrimSpace(pol.QuickChecks) == "":
		pol.QuickChecks = checks
		it.Detail = strings.ReplaceAll(checks, "\n", "; ")
	default:
		it.Detail = "giữ lệnh project đã có"
	}
	if pol.QuickCheck != before.QuickCheck || pol.QuickChecks != before.QuickChecks {
		if err := perm.SavePolicy(ctx, s.cfg.Store, projectID, pol); err != nil {
			it.Status, it.Detail = "error", err.Error()
			return it
		}
		s.audit(r, audit.Change{Action: "policy.update", Resource: "policy", ProjectID: projectID,
			After: map[string]any{"quick_check": pol.QuickCheck, "quick_checks": pol.QuickChecks}})
	}
	it.Status = "ok"
	return it
}

// readiness reports what the project has after the setup: agents,
// workflows, and whether the commands of its catalog can run here.
func (s *server) readiness(ctx context.Context, projectID string) []ReadyItem {
	var items []ReadyItem
	if agents, err := s.cfg.Store.Agents().List(ctx, projectID); err == nil {
		it := ReadyItem{Kind: "agents", Name: "agent", Status: "ok", Detail: fmt.Sprint(len(agents))}
		if len(agents) == 0 {
			it.Status = "error"
		}
		items = append(items, it)
	}
	if wfs, err := s.cfg.Store.Workflows().List(ctx, projectID); err == nil {
		var keys []string
		for _, w := range wfs {
			keys = append(keys, w.Key)
		}
		items = append(items, ReadyItem{Kind: "workflows", Name: "quy trình", Status: "ok", Detail: strings.Join(keys, ", ")})
	}
	pol := perm.LoadPolicy(ctx, s.cfg.Store, projectID)
	missing := map[string]bool{}
	var tools []string
	for _, c := range pol.Catalog {
		f := strings.Fields(c)
		if len(f) == 0 || slices.Contains(tools, f[0]) {
			continue
		}
		tools = append(tools, f[0])
		if _, err := exec.LookPath(f[0]); err != nil {
			missing[f[0]] = true
		}
	}
	switch {
	case len(tools) == 0:
		items = append(items, ReadyItem{Kind: "command", Name: "lệnh build/test", Status: "todo", Detail: "project chưa có lệnh nào: thêm ở Quyền của project"})
	case len(missing) > 0:
		var names []string
		for _, t := range tools {
			if missing[t] {
				names = append(names, t)
			}
		}
		items = append(items, ReadyItem{Kind: "command", Name: "lệnh build/test", Status: "error", Detail: "máy chưa có: " + strings.Join(names, ", ")})
	default:
		items = append(items, ReadyItem{Kind: "command", Name: "lệnh build/test", Status: "ok", Detail: strings.Join(tools, ", ")})
	}
	return items
}
