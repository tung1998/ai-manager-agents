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
	"slices"
	"strings"
	"time"

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
}

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
	Agent          string
	Level          string      // what the agent may do in this run (internal/perm)
	Access         perm.Access // its capabilities and commands in this run
}

// Service proposes and decides actions.
type Service struct {
	store storage.Store
	ops   *ops.Manager
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
	a := storage.Action{ProjectID: sc.ProjectID, ConversationID: sc.ConversationID, TaskID: sc.TaskID, RunRef: sc.RunRef,
		Kind: kind, Target: target, Reason: strings.TrimSpace(reason), ProposedBy: sc.Agent}
	if len(args) > 0 {
		a.Args = args[0]
	}
	if isGit(kind) {
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
		a.Target = strings.Join(args, " ")
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
		return s.Decide(ctx, a.ID, true, "auto:"+sc.Agent+" ("+perm.Label(sc.Access.Level)+")")
	}
	return a, nil
}

// autoAllowed: the run's capabilities decide, within the project's allow
// lists (processes, containers, commands). Push always needs a person.
func (s *Service) autoAllowed(ctx context.Context, a storage.Action, acc perm.Access) bool {
	switch a.Kind {
	case "git_commit":
		return acc.Can(perm.CapCommit)
	case "git_branch":
		return acc.Can(perm.CapBranch)
	case "git_push":
		return false // publishing code always needs a person
	case "run_command":
		args, _ := perm.SplitCommand(a.Target)
		_, ok := perm.MatchCommand(acc.Commands, args)
		return ok && acc.Can(perm.CapCommands)
	}
	pol := perm.LoadPolicy(ctx, s.store, a.ProjectID)
	if isProcess(a.Kind) {
		if !slices.Contains(pol.AllowedCommands, a.TargetID) {
			return false
		}
		if a.Kind == "run_process" && acc.Can(perm.CapCommands) {
			if p, err := s.store.Processes().Get(ctx, a.TargetID); err == nil && p.Kind == "job" {
				return true // a check command
			}
		}
		return acc.Can(perm.CapProcess)
	}
	return slices.Contains(pol.AllowedContainers, a.Target) && acc.Can(perm.CapContainer)
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
		if !isProcess(a.Kind) && !isGit(a.Kind) {
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
