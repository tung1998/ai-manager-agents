package chat

import (
	"errors"
	"testing"
	"time"
)

// An out-of-quota error from a connection that reports no usage itself is
// kept as a "rejected" report, until the reset it names.
func TestLimitsFromError(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	l := limitsFromError(nil, errors.New("agy: Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 9m51s."), now)
	if l == nil || l.Status != "rejected" || !l.Windows["quota"].ResetsAt.Equal(now.Add(9*time.Minute+51*time.Second)) {
		t.Fatalf("limits = %+v", l)
	}
	if l := limitsFromError(nil, errors.New("429 Too Many Requests"), now); l == nil || len(l.Windows) != 0 {
		t.Fatalf("no reset named: a cooldown, got %+v", l)
	}
	if l := limitsFromError(nil, errors.New("build failed"), now); l != nil {
		t.Fatalf("not a quota error: %+v", l)
	}
	if l := limitsFromError(&Limits{Status: "rejected"}, errors.New("usage limit"), now); l != nil {
		t.Fatalf("already reported: %+v", l)
	}
}

func TestRejectedFiveHourResetDoesNotWaitForWeeklyReset(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	l := Limits{Status: "rejected", UpdatedAt: now.Add(-time.Hour), Windows: map[string]LimitWindow{
		"five_hour": {Utilization: 1, ResetsAt: now.Add(time.Minute)},
		"seven_day": {Utilization: 0.4, ResetsAt: now.Add(3 * 24 * time.Hour)},
	}}
	if !overNow(l, now) {
		t.Fatal("five-hour limit has not reset yet")
	}
	if overNow(l, now.Add(2*time.Minute)) {
		t.Fatal("weekly window with spare capacity kept the primary connection deprioritized")
	}
	l.Windows["seven_day"] = LimitWindow{Utilization: 1, ResetsAt: now.Add(3 * 24 * time.Hour)}
	if !overNow(l, now.Add(2*time.Minute)) {
		t.Fatal("full weekly window must still hold the connection")
	}
}
