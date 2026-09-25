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

	"bitbucket.org/senprints/agent-office/internal/ops"
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
}

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
func (s *Service) Propose(ctx context.Context, sc Scope, kind, target, reason string) (storage.Action, error) {
	if _, ok := Kinds[kind]; !ok {
		return storage.Action{}, fmt.Errorf("%w: %s (cho phép: %s)", ErrKind, kind, strings.Join(slices.Sorted(maps.Keys(Kinds)), ", "))
	}
	target = strings.TrimSpace(target)
	a := storage.Action{ProjectID: sc.ProjectID, ConversationID: sc.ConversationID, TaskID: sc.TaskID, RunRef: sc.RunRef,
		Kind: kind, Target: target, Reason: strings.TrimSpace(reason), ProposedBy: sc.Agent}
	if isProcess(kind) {
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
	return s.store.Actions().Create(ctx, a)
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
	if err := s.run(ctx, a); err != nil {
		a.Status, a.Detail = "failed", err.Error()
	} else {
		a.Status, a.Detail = "done", Kinds[a.Kind]+" "+a.Target+": đã thực hiện"
		if !isProcess(a.Kind) {
			a.Detail += " (docker compose chạy nền, xem mục Container)"
		}
	}
	return a, s.store.Actions().Update(ctx, a)
}

func (s *Service) run(ctx context.Context, a storage.Action) error {
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
