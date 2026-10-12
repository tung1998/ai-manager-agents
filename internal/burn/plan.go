package burn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A Burn's run plan: how long a run lasts once turned on, and when it turns
// on by itself (storage.BurnPlan). Turning it on asks nothing more.

// Ends of a run (BurnPlan.End).
const (
	EndReset = "reset" // the agent's weekly limit reset, else 8 hours ("" too)
	EndHours = "hours"
	EndClock = "clock"
	EndNone  = "none"
)

// MaxPlanHours caps a run's length in hours (a week).
const MaxPlanHours = 168

// SchedSource names who turned a Burn on by its schedule.
const SchedSource = "auto:lịch Burn" // i18n-ignore

// schedLate: a schedule missed by up to this long (office was off) still turns it on.
const schedLate = time.Hour

// parseClock reads HH:MM.
func parseClock(v string) (h, m int, ok bool) {
	if _, err := fmt.Sscanf(v, "%d:%d", &h, &m); err != nil || len(v) != 5 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// planLoc is the plan's time zone (the machine's when unset or unknown).
func planLoc(p storage.BurnPlan) *time.Location {
	if p.TZ != "" {
		if l, err := time.LoadLocation(p.TZ); err == nil {
			return l
		}
	}
	return time.Local
}

// CheckPlan refuses a plan that cannot run: a missing time, no day.
func CheckPlan(p storage.BurnPlan) error {
	switch p.End {
	case "", EndReset, EndNone:
	case EndHours:
		if p.Hours < 1 || p.Hours > MaxPlanHours {
			return fmt.Errorf("số giờ chạy phải từ 1 đến %d", MaxPlanHours)
		}
	case EndClock:
		if _, _, ok := parseClock(p.Clock); !ok {
			return errors.New("giờ tắt phải dạng HH:MM")
		}
	default:
		return errors.New("không có kiểu dừng này")
	}
	if p.TZ != "" {
		if _, err := time.LoadLocation(p.TZ); err != nil {
			return errors.New("múi giờ không hợp lệ")
		}
	}
	if !p.Auto {
		return nil
	}
	if _, _, ok := parseClock(p.At); !ok {
		return errors.New("giờ tự bật phải dạng HH:MM")
	}
	if len(p.Days) == 0 {
		return errors.New("chọn ít nhất một ngày tự bật")
	}
	for _, d := range p.Days {
		if d < 0 || d > 6 {
			return errors.New("ngày tự bật không hợp lệ")
		}
	}
	return nil
}

// nextClock is the first HH:MM after now in loc.
func nextClock(now time.Time, clock string, loc *time.Location) time.Time {
	h, m, _ := parseClock(clock)
	n := now.In(loc)
	t := time.Date(n.Year(), n.Month(), n.Day(), h, m, 0, 0, loc)
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// EndFor is when a run started now stops by the Burn's plan (nil: never).
func (s *Service) EndFor(ctx context.Context, b storage.BurnSession, now time.Time) *time.Time {
	var t time.Time
	switch b.Plan.End {
	case EndNone:
		return nil
	case EndHours:
		t = now.Add(time.Duration(max(b.Plan.Hours, 1)) * time.Hour)
	case EndClock:
		t = nextClock(now, b.Plan.Clock, planLoc(b.Plan))
	default:
		t = s.DefaultEnd(ctx, b.AgentID)
	}
	t = t.UTC()
	return &t
}

// slot is the schedule's latest start at or before now (zero: none within a week).
func slot(p storage.BurnPlan, now time.Time) time.Time {
	h, m, ok := parseClock(p.At)
	if !p.Auto || !ok {
		return time.Time{}
	}
	loc := planLoc(p)
	n := now.In(loc)
	for back := 0; back < 7; back++ {
		d := n.AddDate(0, 0, -back)
		t := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
		if !t.After(now) && slices.Contains(p.Days, int(t.Weekday())) {
			return t
		}
	}
	return time.Time{}
}

// due says whether a scheduled Burn turns on now: its slot came (missed by
// no more than schedLate), it is off, and it has not run since that slot (a
// run turned off by hand is not turned on again until the next slot).
func due(b storage.BurnSession, now time.Time) bool {
	at := slot(b.Plan, now)
	if at.IsZero() || now.Sub(at) > schedLate || b.Active() {
		return false
	}
	return b.StartedAt == nil || b.StartedAt.Before(at)
}

// NextStart is when a scheduled Burn turns on next (false: not scheduled).
func NextStart(p storage.BurnPlan, now time.Time) (time.Time, bool) {
	h, m, ok := parseClock(p.At)
	if !p.Auto || !ok || len(p.Days) == 0 {
		return time.Time{}, false
	}
	loc := planLoc(p)
	n := now.In(loc)
	for ahead := 0; ahead <= 7; ahead++ {
		d := n.AddDate(0, 0, ahead)
		t := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
		if t.After(now) && slices.Contains(p.Days, int(t.Weekday())) {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// schedTick: how often the schedules are looked at.
const schedTick = 30 * time.Second

// schedule turns on the Burns whose time came, until ctx ends.
func (s *Service) schedule(ctx context.Context) {
	t := time.NewTicker(schedTick)
	defer t.Stop()
	for {
		s.StartDue(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// StartDue turns on each scheduled Burn whose time came, with its plan's end.
func (s *Service) StartDue(ctx context.Context, now time.Time) {
	list, err := s.store.Burn().Scheduled(ctx)
	if err != nil {
		return
	}
	for _, b := range list {
		at := slot(b.Plan, now)
		s.mu.Lock()
		tried := s.tried[b.ProjectID].Equal(at)
		s.tried[b.ProjectID] = at // once a slot: a start that failed waits for the next
		s.mu.Unlock()
		if tried || !due(b, now) {
			continue
		}
		if _, err := s.TurnOn(ctx, b.ProjectID, SchedSource, now); err != nil {
			slog.Warn("burn: scheduled start", "project", b.ProjectID, "err", err)
			s.say(ctx, b, "Không tự bật Burn theo lịch được: "+err.Error()) // i18n-ignore
		}
	}
}

// TurnOn starts a project's Burn on its saved settings, stopping by its plan.
func (s *Service) TurnOn(ctx context.Context, projectID, who string, now time.Time) (storage.BurnSession, error) {
	b, err := s.Session(ctx, projectID)
	if err != nil {
		return b, err
	}
	b.EndsAt = s.EndFor(ctx, b, now)
	if b, err = s.store.Burn().SaveSession(ctx, b); err != nil {
		return b, err
	}
	return s.Begin(ctx, projectID, who)
}
