// Package actions turns an agent's request to operate the project (run,
// restart or stop a process or a compose service) into a proposal that a
// person approves; only then does office carry it out. Agents never operate
// anything directly.
package actions

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/gitops"
	"bitbucket.org/senprints/agent-office/internal/ops"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Kinds agents may propose.
var Kinds = map[string]string{
	"run_process":       "Chạy tiến trình",
	"restart_process":   "Chạy lại tiến trình",
	"stop_process":      "Dừng tiến trình",
	"start_container":   "Bật container",
	"restart_container": "Chạy lại container",
	"stop_container":    "Dừng container",
	"git_commit":        "Commit",
	"git_branch":        "Tạo nhánh",
	"git_push":          "Push lên remote",
	"run_command":       "Chạy lệnh",
	"create_automation": "Tạo tự động hóa",
	"update_automation": "Sửa tự động hóa",
	"config_change":     "Đổi cài đặt",
	"start_task":        "Giao Việc",
	"run_automation":    "Chạy tự động hóa",
}

// Runner starts work the office assistant proposed (ADR-046).
type Runner interface {
	StartTask(ctx context.Context, projectID, agentID, goal string) (string, error)
	RunAutomation(ctx context.Context, automationID string) (string, error)
}

// SetRunner turns on start_task and run_automation proposals.
func (s *Service) SetRunner(r Runner) { s.runner = r }

// ConfigApplier checks and applies settings changes (the config registry of
// the API, ADR-045).
type ConfigApplier interface {
	// CheckChange validates a proposal of projectID's agent, fills c.Before
	// and returns a label for the card.
	CheckChange(ctx context.Context, projectID string, c *storage.ConfigChange) (string, error)
	// ApplyChange carries out an approved change (ctx carries the approver).
	ApplyChange(ctx context.Context, a storage.Action) (string, error)
}

// isAutomation: code that will run unattended, so always a person decides.
func isAutomation(kind string) bool {
	return kind == "create_automation" || kind == "update_automation"
}

// SetConfig turns on config_change proposals.
func (s *Service) SetConfig(c ConfigApplier) { s.config = c }

func isGit(kind string) bool { return strings.HasPrefix(kind, "git_") }

var (
	ErrKind    = errors.New("hành động không được phép")
	ErrTarget  = errors.New("không có mục tiêu này trong project")
	ErrDecided = errors.New("đề xuất này đã được xử lý")
)

// Scope is where a proposal comes from.
type Scope struct {
	ProjectID      string
	ConversationID string
	TaskID         string
	RunRef         string
	JobID          string // the chat answer/task run it comes from (ADR-043)
	Office         bool   // the office assistant: tools across projects (ADR-046)
	Agent          string
	Level          string      // what the agent may do in this run (internal/perm)
	Access         perm.Access // its capabilities and commands in this run
	// Dir is the run's own worktree ("" = the project folder): commands run
	// there, and the project's allowed commands run without asking since
	// nothing there touches the project until a person merges it.
	Dir string
}

// Service proposes and decides actions.
type Service struct {
	store  storage.Store
	ops    *ops.Manager
	config ConfigApplier
	runner Runner
}

// New builds a Service.
func New(store storage.Store, o *ops.Manager) *Service { return &Service{store: store, ops: o} }

func isProcess(kind string) bool { return strings.HasSuffix(kind, "_process") }

// Propose validates and records a pending action (an identical pending one
// from the same conversation/task is returned instead of a duplicate).
func (s *Service) Propose(ctx context.Context, sc Scope, kind, target, reason string, args ...storage.ActionArgs) (storage.Action, error) {
	if _, ok := Kinds[kind]; !ok {
		return storage.Action{}, fmt.Errorf("%w: %s (cho phép: %s)", ErrKind, kind, strings.Join(slices.Sorted(maps.Keys(Kinds)), ", "))
	}
	target = strings.TrimSpace(target)
	a := storage.Action{ProjectID: sc.ProjectID, ConversationID: sc.ConversationID, TaskID: sc.TaskID, RunRef: sc.RunRef, JobID: sc.JobID,
		Kind: kind, Target: target, Reason: strings.TrimSpace(reason), ProposedBy: sc.Agent}
	if len(args) > 0 {
		a.Args = args[0]
	}
	if kind == "start_task" || kind == "run_automation" { // costs tokens: a person always decides
		if s.runner == nil {
			return a, errors.New("không giao việc được ở đây")
		}
		if err := s.checkRun(ctx, &a); err != nil {
			return a, err
		}
	} else if kind == "config_change" { // a person always decides
		if s.config == nil || a.Args.Change == nil {
			return a, errors.New("không đổi được cài đặt ở đây")
		}
		label, err := s.config.CheckChange(ctx, sc.ProjectID, a.Args.Change)
		if err != nil {
			return a, err
		}
		a.Target, a.TargetID = label, a.Args.Change.ID
	} else if isAutomation(kind) {
		spec, err := s.automationSpec(ctx, sc.ProjectID, kind, a.Args.Automation)
		if err != nil {
			return a, err
		}
		a.Target = spec.Name
		if kind == "update_automation" {
			a.TargetID = spec.AutomationID
		}
	} else if isGit(kind) {
		if sc.Dir != "" {
			return a, errors.New("bạn đang làm trong worktree riêng: commit, tạo nhánh và push làm sau khi người dùng gộp thay đổi vào project")
		}
		if err := s.checkGit(ctx, &a); err != nil {
			return a, err
		}
	} else if kind == "run_command" {
		args, err := perm.SplitCommand(target)
		if err != nil {
			return a, fmt.Errorf("%w: chạy không qua shell nên không dùng được | ; & > $ (gọi từng lệnh riêng)", err)
		}
		if _, err := s.projectPath(ctx, sc.ProjectID); err != nil {
			return a, err
		}
		a.Target, a.Args.Dir = strings.Join(args, " "), sc.Dir
	} else if isProcess(kind) {
		procs, err := s.store.Processes().List(ctx, sc.ProjectID)
		if err != nil {
			return a, err
		}
		for _, p := range procs {
			if strings.EqualFold(p.Name, target) || p.ID == target {
				a.Target, a.TargetID = p.Name, p.ID
			}
		}
		if a.TargetID == "" {
			return a, fmt.Errorf("%w: tiến trình %q", ErrTarget, target)
		}
	} else {
		if s.ops == nil {
			return a, ErrTarget
		}
		v, err := s.ops.Compose(ctx, sc.ProjectID, "", false)
		if err != nil {
			return a, err
		}
		if !slices.ContainsFunc(v.Services, func(x ops.Service) bool { return x.Name == target }) {
			return a, fmt.Errorf("%w: service %q", ErrTarget, target)
		}
	}
	if sc.ConversationID != "" || sc.TaskID != "" {
		if list, err := s.store.Actions().List(ctx, sc.ConversationID, sc.TaskID, ""); err == nil {
			for _, x := range list {
				if x.Status == "pending" && x.Kind == a.Kind && x.Target == a.Target {
					return x, nil
				}
			}
		}
	}
	a, err := s.store.Actions().Create(ctx, a)
	if err != nil {
		return a, err
	}
	// within the agent's permission package and the project's allow lists,
	// office carries it out right away
	if s.autoAllowed(ctx, a, sc.Access) {
		done, err := s.Decide(ctx, a.ID, true, "auto:"+sc.Agent+" ("+perm.Label(sc.Access.Level)+")")
		s.auditAuto(ctx, sc, done, err)
		return done, err
	}
	return a, nil
}

// auditAuto logs an action office carried out on the agent's own permission
// (no person approved it) as the agent's change (ADR-043).
func (s *Service) auditAuto(ctx context.Context, sc Scope, a storage.Action, err error) {
	via := "chat"
	if sc.ConversationID == "" && sc.TaskID != "" {
		via = "task"
	}
	if err == nil && a.Status == "failed" {
		err = errors.New(a.Detail)
	}
	actx := audit.With(ctx, audit.Who{Kind: "agent", Name: sc.Agent, Via: via, ConversationID: sc.ConversationID, JobID: sc.JobID, TaskID: sc.TaskID, ActionID: a.ID})
	_ = audit.Record(actx, s.store.Audit(), audit.Change{Action: "action.approve", Resource: "action", ResourceID: a.ID, ProjectID: sc.ProjectID,
		Detail: map[string]any{"kind": a.Kind, "target": a.Target, "status": a.Status, "auto": true}, Err: err})
}

// autoAllowed: the run's capabilities decide, within the project's allow
// lists (processes, containers, commands). Push always needs a person.
func (s *Service) autoAllowed(ctx context.Context, a storage.Action, acc perm.Access) bool {
	switch a.Kind {
	case "create_automation", "update_automation", "config_change", "start_task", "run_automation":
		return false // code that runs unattended, or settings: a person always decides
	case "git_commit":
		return acc.Can(perm.CapCommit)
	case "git_branch":
		return acc.Can(perm.CapBranch)
	case "git_push":
		return false // publishing code always needs a person
	case "run_command":
		args, _ := perm.SplitCommand(a.Target)
		if _, safe := perm.MatchCommand(acc.Safe, args); safe {
			return true // checks and reads, from read only
		}
		_, ok := perm.MatchCommand(acc.Commands, args)
		return ok && (acc.Can(perm.CapCommands) || a.Args.Dir != "" && perm.AtLeast(acc.Level, perm.Propose))
	}
	if isProcess(a.Kind) {
		if !slices.Contains(acc.Processes, a.TargetID) {
			return false
		}
		if a.Kind == "run_process" && acc.Can(perm.CapCommands) {
			if p, err := s.store.Processes().Get(ctx, a.TargetID); err == nil && p.Kind == "job" {
				return true // a check command
			}
		}
		return acc.Can(perm.CapProcess)
	}
	return slices.Contains(acc.Containers, a.Target) && acc.Can(perm.CapContainer)
}

// Decide runs (approve) or rejects a pending action.
func (s *Service) Decide(ctx context.Context, id string, approve bool, by string) (storage.Action, error) {
	a, err := s.store.Actions().Get(ctx, id)
	if err != nil {
		return a, err
	}
	if a.Status != "pending" {
		return a, ErrDecided
	}
	now := time.Now().UTC()
	a.DecidedBy, a.DecidedAt = by, &now
	if !approve {
		a.Status = "rejected"
		return a, s.store.Actions().Update(ctx, a)
	}
	if a.Kind == "run_command" {
		root, err := s.projectPath(ctx, a.ProjectID)
		if a.Args.Dir != "" {
			root = a.Args.Dir
			if _, serr := os.Stat(root); serr != nil {
				err = errors.New("worktree của lượt này đã được dọn nên không chạy được nữa")
			}
		}
		out := ""
		if err == nil {
			out, err = runCommand(ctx, root, a.Target)
		}
		a.Status, a.Detail = "done", "Thành công"
		if err != nil {
			a.Status, a.Detail = "failed", err.Error()
		}
		if out != "" {
			a.Detail += "\n" + out
		}
		return a, s.store.Actions().Update(ctx, a)
	}
	if err := s.run(ctx, a); err != nil {
		a.Status, a.Detail = "failed", err.Error()
	} else {
		a.Status, a.Detail = "done", Kinds[a.Kind]+" "+a.Target+": đã thực hiện"
		if a.Kind == "git_commit" {
			a.Detail = fmt.Sprintf("Đã commit %d file: %s", len(a.Args.Files), a.Target)
		}
		if strings.HasSuffix(a.Kind, "_container") {
			a.Detail += " (docker compose chạy nền, xem mục Container)"
		}
	}
	return a, s.store.Actions().Update(ctx, a)
}

// checkGit validates a git action and fills its target: commit files must be
// changed and allowed by the policy (none given = every allowed change).
func (s *Service) checkGit(ctx context.Context, a *storage.Action) error {
	root, err := s.projectPath(ctx, a.ProjectID)
	if err != nil {
		return err
	}
	switch a.Kind {
	case "git_commit":
		a.Args.Message = strings.TrimSpace(firstNonEmpty(a.Args.Message, a.Target))
		if a.Args.Message == "" {
			return errors.New("commit cần message")
		}
		st, err := gitops.ReadStatus(ctx, root)
		if err != nil {
			return err
		}
		changed := map[string]bool{}
		for _, c := range st.Changes {
			changed[c.Path] = true
		}
		files := a.Args.Files
		if len(files) == 0 {
			for _, c := range st.Changes {
				files = append(files, c.Path)
			}
		}
		pol := perm.LoadPolicy(ctx, s.store, a.ProjectID)
		var keep []string
		for _, f := range files {
			if changed[strings.TrimPrefix(f, "./")] {
				keep = append(keep, strings.TrimPrefix(f, "./"))
			}
		}
		if denied := pol.Denied(keep); len(denied) > 0 {
			return fmt.Errorf("không được commit file cấm: %s", strings.Join(denied, ", "))
		}
		if len(keep) == 0 {
			return errors.New("không có thay đổi nào để commit")
		}
		a.Args.Files = keep
		a.Target = strings.SplitN(a.Args.Message, "\n", 2)[0]
	case "git_branch":
		a.Args.Branch = strings.TrimSpace(firstNonEmpty(a.Args.Branch, a.Target))
		if a.Args.Branch == "" {
			return errors.New("cần tên nhánh")
		}
		a.Target = a.Args.Branch
	case "git_push":
		st, err := gitops.ReadStatus(ctx, root)
		if err != nil {
			return err
		}
		a.Target = st.Branch
	}
	return nil
}

func (s *Service) projectPath(ctx context.Context, projectID string) (string, error) {
	p, err := s.store.Repos().Get(ctx, projectID)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", errors.New("project không gắn thư mục")
	}
	return p.Path, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (s *Service) run(ctx context.Context, a storage.Action) error {
	if a.Kind == "start_task" || a.Kind == "run_automation" {
		if s.runner == nil {
			return errors.New("không giao việc được ở đây")
		}
		var err error
		if a.Kind == "start_task" {
			_, err = s.runner.StartTask(ctx, a.ProjectID, a.TargetID, a.Args.Message)
		} else {
			_, err = s.runner.RunAutomation(ctx, a.TargetID)
		}
		return err
	}
	if a.Kind == "config_change" {
		if s.config == nil {
			return errors.New("không đổi được cài đặt ở đây")
		}
		_, err := s.config.ApplyChange(ctx, a)
		return err
	}
	if isAutomation(a.Kind) {
		return s.saveAutomation(ctx, a)
	}
	if isGit(a.Kind) {
		root, err := s.projectPath(ctx, a.ProjectID)
		if err != nil {
			return err
		}
		switch a.Kind {
		case "git_commit":
			_, err = gitops.Commit(ctx, root, a.Args.Message, a.Args.Files)
		case "git_branch":
			err = gitops.CreateBranch(ctx, root, a.Args.Branch)
		case "git_push":
			_, err = gitops.Push(ctx, root)
		}
		return err
	}
	if s.ops == nil {
		return errors.New("office không quản lý tiến trình/container")
	}
	switch a.Kind {
	case "run_process":
		return s.ops.Start(ctx, a.TargetID)
	case "restart_process":
		return s.ops.Restart(ctx, a.TargetID)
	case "stop_process":
		return s.ops.Stop(a.TargetID)
	case "start_container":
		return s.ops.ComposeAction(ctx, a.ProjectID, "", "up", a.Target)
	case "restart_container":
		return s.ops.ComposeAction(ctx, a.ProjectID, "", "restart", a.Target)
	case "stop_container":
		return s.ops.ComposeAction(ctx, a.ProjectID, "", "stop", a.Target)
	}
	return ErrKind
}

// checkRun checks a start_task (Target = the agent's name, "" = the team;
// Args.Message = the goal) or run_automation (Target = its id) proposal.
func (s *Service) checkRun(ctx context.Context, a *storage.Action) error {
	if a.Kind == "run_automation" {
		au, err := s.store.Automations().Get(ctx, a.Target)
		if err != nil || au.ProjectID != a.ProjectID {
			return errors.New("không có tự động hóa này trong project")
		}
		a.Target, a.TargetID = au.Name, au.ID
		return nil
	}
	goal := strings.TrimSpace(a.Args.Message)
	if goal == "" {
		return errors.New("hãy ghi rõ việc cần làm")
	}
	agent := strings.TrimSpace(a.Target)
	a.TargetID = ""
	if agent != "" {
		m, err := s.store.OrgModels().GetForRepo(ctx, a.ProjectID)
		if err != nil {
			return errors.New("project chưa có agent")
		}
		list, _ := s.store.Agents().List(ctx, m.ID)
		for _, x := range list {
			if strings.EqualFold(x.Name, agent) || strings.EqualFold(x.Key, agent) {
				a.TargetID = x.ID
			}
		}
		if a.TargetID == "" {
			return fmt.Errorf("không có agent %q trong project", agent)
		}
	}
	a.Target = goal
	if r := []rune(goal); len(r) > 80 {
		a.Target = string(r[:80]) + "…"
	}
	return nil
}
