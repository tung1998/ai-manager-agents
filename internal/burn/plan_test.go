package burn

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// A plan that cannot run is refused before it is saved.
func TestCheckPlan(t *testing.T) {
	for _, c := range []struct {
		p  storage.BurnPlan
		ok bool
	}{
		{storage.BurnPlan{}, true},
		{storage.BurnPlan{End: EndHours, Hours: 4}, true},
		{storage.BurnPlan{End: EndHours}, false},
		{storage.BurnPlan{End: EndHours, Hours: MaxPlanHours + 1}, false},
		{storage.BurnPlan{End: EndClock, Clock: "07:30"}, true},
		{storage.BurnPlan{End: EndClock, Clock: "7:30"}, false},
		{storage.BurnPlan{End: EndClock, Clock: "24:00"}, false},
		{storage.BurnPlan{End: "later"}, false},
		{storage.BurnPlan{Auto: true, At: "22:00", Days: []int{1, 5}}, true},
		{storage.BurnPlan{Auto: true, At: "22:00"}, false},
		{storage.BurnPlan{Auto: true, Days: []int{1}}, false},
		{storage.BurnPlan{Auto: true, At: "22:00", Days: []int{7}}, false},
		{storage.BurnPlan{TZ: "Mars/Olympus"}, false},
	} {
		if err := CheckPlan(c.p); (err == nil) != c.ok {
			t.Errorf("CheckPlan(%+v) = %v, want ok %v", c.p, err, c.ok)
		}
	}
}

// Turned on now, a run stops after its hours, at the next clock time, or never.
func TestEndFor(t *testing.T) {
	s := &Service{}
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	now := time.Date(2026, 10, 12, 21, 0, 0, 0, loc) // a Monday, 21:00
	if e := s.EndFor(context.Background(), storage.BurnSession{Plan: storage.BurnPlan{End: EndHours, Hours: 3}}, now); e == nil || !e.Equal(now.Add(3*time.Hour)) {
		t.Fatalf("hours = %v", e)
	}
	clock := storage.BurnPlan{End: EndClock, Clock: "07:00", TZ: "Asia/Ho_Chi_Minh"}
	if e := s.EndFor(context.Background(), storage.BurnSession{Plan: clock}, now); e == nil || !e.Equal(time.Date(2026, 10, 13, 7, 0, 0, 0, loc)) {
		t.Fatalf("clock (tomorrow) = %v", e)
	}
	clock.Clock = "23:30"
	if e := s.EndFor(context.Background(), storage.BurnSession{Plan: clock}, now); e == nil || !e.Equal(time.Date(2026, 10, 12, 23, 30, 0, 0, loc)) {
		t.Fatalf("clock (today) = %v", e)
	}
	if e := s.EndFor(context.Background(), storage.BurnSession{Plan: storage.BurnPlan{End: EndNone}}, now); e != nil {
		t.Fatalf("none = %v", e)
	}
}

// A scheduled Burn turns on at its slot (or up to an hour late), once: not
// while on, not again after it was turned off by hand, not on other days.
func TestScheduleDue(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	plan := storage.BurnPlan{Auto: true, At: "22:00", Days: []int{1, 2, 3, 4, 5}, TZ: "Asia/Ho_Chi_Minh"}
	at := time.Date(2026, 10, 12, 22, 0, 0, 0, loc) // Monday
	off := storage.BurnSession{State: "stopped", Plan: plan}
	if due(off, at.Add(-time.Minute)) {
		t.Fatal("due before its time")
	}
	if !due(off, at.Add(10*time.Second)) || !due(off, at.Add(59*time.Minute)) {
		t.Fatal("not due at its time")
	}
	if due(off, at.Add(2*time.Hour)) {
		t.Fatal("due long after its time")
	}
	on := off
	on.State = "running"
	if due(on, at.Add(time.Minute)) {
		t.Fatal("due while on")
	}
	later := at.Add(5 * time.Minute)
	stopped := off
	stopped.StartedAt = &later // it ran since that slot, then was turned off
	if due(stopped, at.Add(10*time.Minute)) {
		t.Fatal("due again after being turned off")
	}
	sat := time.Date(2026, 10, 17, 22, 0, 0, 0, loc)
	if due(off, sat.Add(time.Minute)) {
		t.Fatal("due on a day it is not set for")
	}
	if next, ok := NextStart(plan, sat.Add(time.Minute)); !ok || !next.Equal(time.Date(2026, 10, 19, 22, 0, 0, 0, loc)) { // Monday after
		t.Fatalf("next start = %v %v", next, ok)
	}
	if _, ok := NextStart(storage.BurnPlan{}, at); ok {
		t.Fatal("next start without a schedule")
	}
}

// Only the Burns set to turn on by themselves are looked at.
func TestScheduledSessions(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, plan := range []storage.BurnPlan{{Auto: true, At: "22:00", Days: []int{1}}, {End: EndHours, Hours: 2}} {
		p, err := st.Repos().Create(ctx, storage.Repo{Name: "p" + string(rune('a'+i)), Path: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		b, err := st.Burn().SaveSession(ctx, storage.BurnSession{ProjectID: p.ID, State: "stopped", Plan: plan})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, b.ID)
	}
	list, err := st.Burn().Scheduled(ctx)
	if err != nil || len(list) != 1 || list[0].ID != ids[0] || list[0].Plan.At != "22:00" {
		t.Fatalf("scheduled = %+v, %v", list, err)
	}
	b, _ := st.Burn().SessionByID(ctx, ids[1])
	if b.Plan.End != EndHours || b.Plan.Hours != 2 {
		t.Fatalf("plan read back = %+v", b.Plan)
	}
}
