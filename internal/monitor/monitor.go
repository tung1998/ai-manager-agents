// Package monitor watches a project's office processes and compose
// containers on a schedule, one monitor per process or service (turned on
// from its row in Vận hành). Checks are rule-based and free; a monitor with
// AI enabled asks the project's lead agent to analyse when it goes down,
// within a daily cap.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/ops"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Types: what a monitor watches.
var Types = []string{"process", "container"}

const (
	downAfter   = 2 // consecutive failures before a container is down (flap guard)
	keepChecks  = 7 * 24 * time.Hour
	aiCooldown  = 30 * time.Minute
	maxParallel = 8
)

// Result is one check outcome.
type Result struct {
	OK        bool
	LatencyMS int
	Message   string
}

// Service schedules and runs monitors.
type Service struct {
	store  storage.Store
	ops    *ops.Manager
	engine *chat.Engine
	now    func() time.Time

	mu       sync.Mutex
	inFlight map[string]bool
}

// New builds a Service. engine may be nil (no AI analysis).
func New(store storage.Store, o *ops.Manager, engine *chat.Engine) *Service {
	return &Service{store: store, ops: o, engine: engine, now: time.Now, inFlight: map[string]bool{}}
}

// Run checks due monitors until ctx ends.
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	sem := make(chan struct{}, maxParallel)
	for {
		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			_ = s.store.Monitors().PruneChecks(ctx, s.now().Add(-keepChecks))
		case <-tick.C:
			list, err := s.store.Monitors().List(ctx, "")
			if err != nil {
				continue
			}
			for _, m := range list {
				if !m.Enabled || !s.due(m) || !s.claim(m.ID) {
					continue
				}
				sem <- struct{}{}
				m := m
				safeGo("CheckNow", func() {
					defer func() { <-sem; s.release(m.ID) }()
					s.CheckNow(ctx, m.ID)
				})
			}
		}
	}
}

func (s *Service) due(m storage.Monitor) bool {
	iv := time.Duration(max(m.IntervalS, 10)) * time.Second
	return m.LastCheckedAt == nil || s.now().Sub(*m.LastCheckedAt) >= iv
}

func (s *Service) claim(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight[id] {
		return false
	}
	s.inFlight[id] = true
	return true
}

func (s *Service) release(id string) {
	s.mu.Lock()
	delete(s.inFlight, id)
	s.mu.Unlock()
}

// CheckNow runs one check, records it and handles status changes.
func (s *Service) CheckNow(ctx context.Context, id string) (storage.Monitor, error) {
	m, err := s.store.Monitors().Get(ctx, id)
	if err != nil {
		return m, err
	}
	res := s.check(ctx, m)
	now := s.now().UTC()
	m.LastCheckedAt = &now
	if err := s.store.Monitors().AddCheck(ctx, storage.MonitorCheck{MonitorID: m.ID, At: now, OK: res.OK, LatencyMS: res.LatencyMS, Message: res.Message}); err != nil {
		slog.Error("monitor: save check failed", "monitor_id", m.ID, "error", err)
	}
	m.LastLatencyMS, m.LastMessage = res.LatencyMS, res.Message
	prev := m.Status
	threshold := downAfter
	if m.Type == "process" {
		threshold = 1 // already a sustained signal
	}
	if res.OK {
		m.Fails = 0
		m.Status = "up"
	} else {
		m.Fails++
		if m.Fails >= threshold {
			m.Status = "down"
		}
	}
	if m.Status != prev && m.Status != "pending" {
		m.LastChangeAt = &now
		// the first result of a new monitor is not an incident, unless it is down
		if prev != "pending" || m.Status == "down" {
			ev, err := s.store.Monitors().AddEvent(ctx, storage.MonitorEvent{MonitorID: m.ID, ProjectID: m.ProjectID, Kind: m.Status, Message: res.Message, At: now})
			if err == nil && m.Status == "down" {
				s.maybeAnalyse(m, ev)
			}
		}
	}
	return m, s.store.Monitors().SaveStatus(ctx, m)
}

func (s *Service) check(ctx context.Context, m storage.Monitor) Result {
	switch m.Type {
	case "process":
		if s.ops == nil {
			return Result{Message: "không quản lý tiến trình"}
		}
		st := s.ops.State(m.Target)
		if st.Status == "running" {
			return Result{OK: true, Message: fmt.Sprintf("đang chạy (PID %d)", st.PID)}
		}
		switch st.Status {
		case "stopped", "stopping":
			return Result{Message: "tiến trình đã bị dừng"}
		case "restarting":
			return Result{Message: fmt.Sprintf("tiến trình lỗi, đang chạy lại (lần %d)", st.Restarts)}
		}
		msg := "tiến trình " + st.Status
		if st.ExitCode != nil {
			msg += fmt.Sprintf(", mã thoát %d", *st.ExitCode)
		}
		return Result{Message: msg}
	case "container":
		if s.ops == nil {
			return Result{Message: "không quản lý container"}
		}
		start := s.now()
		c, err := s.ops.ServiceContainer(ctx, m.ProjectID, m.Config.File, m.Target)
		ms := int(s.now().Sub(start).Milliseconds())
		switch {
		case err != nil:
			return Result{LatencyMS: ms, Message: err.Error()}
		case c == nil:
			return Result{LatencyMS: ms, Message: "container chưa được tạo"}
		case c.State != "running":
			return Result{LatencyMS: ms, Message: c.Status}
		case c.Health == "unhealthy":
			return Result{LatencyMS: ms, Message: "unhealthy: " + c.Status}
		}
		return Result{OK: true, LatencyMS: ms, Message: c.Status}
	}
	return Result{Message: "loại giám sát không hỗ trợ"}
}
