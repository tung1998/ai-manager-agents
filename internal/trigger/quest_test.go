package trigger_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// @@quest lines (ADR-132): a title, a detail after " :: ", each title once
// (case and spaces aside); other lines and look-alikes are not quests.
func TestQuestLines(t *testing.T) {
	out := strings.Join([]string{
		"checked 12 issues",
		"@@quest Sửa lỗi đăng nhập :: trang /login trả 500",
		"  @@quest   Thêm   test cho API  ",
		"@@quest: Dọn log :: a :: b",
		"@@quest sửa LỖI đăng   nhập :: lặp lại",
		"@@questions are not quests",
		"@@quest ",
		"@@agent: nhìn giúp",
	}, "\n")
	want := []trigger.Quest{
		{Title: "Sửa lỗi đăng nhập", Detail: "trang /login trả 500"},
		{Title: "Thêm test cho API"},
		{Title: "Dọn log", Detail: "a :: b"},
	}
	if got := trigger.Quests(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("quests = %#v", got)
	}
	if long := trigger.Quests("@@quest " + strings.Repeat("x", 300)); len(long) != 1 || len([]rune(long[0].Title)) != 160 {
		t.Fatalf("a long title: %v", long)
	}
}

type questSink struct {
	mu     sync.Mutex
	got    []trigger.Quest
	starts []bool
}

func (s *questSink) give(_ context.Context, _ string, qs []trigger.Quest, start bool, _ string) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got, s.starts = append(s.got, qs...), append(s.starts, start)
	return len(qs) - 1, start, nil // the first one was open already
}

// A script's quests go to the Burn; the job's output says how many, and the
// answer leaves the @@quest lines out. burn_start starts it on a run by hand
// or a schedule, never on a webhook's.
func TestScriptGivesQuests(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	sink := &questSink{}
	r := trigger.New(st, &recExec{})
	r.SetQuests(sink.give)
	a, _ := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "triage", Source: "schedule", Action: "script", Enabled: true,
		Config: storage.AutomationConfig{EveryMinutes: 5},
		Script: storage.AutomationScript{Lang: "bash", Body: "echo xong\necho '@@quest Một'\necho '@@quest Hai :: chi tiết'", TimeoutS: 30, BurnStart: true}})
	runOnce(t, r, st, a, "")
	if len(sink.got) != 2 || sink.got[1] != (trigger.Quest{Title: "Hai", Detail: "chi tiết"}) || !sink.starts[0] {
		t.Fatalf("quests = %+v starts %v", sink.got, sink.starts)
	}
	s, _ := jobsOf(t, st, a)
	if s.Status != "done" || !strings.Contains(s.Output, "Đã thêm 1 quest cho Burn (bỏ qua 1 đã có); Burn đã bật") || !strings.Contains(s.Output, "@@quest Một") {
		t.Fatalf("output = %q", s.Output)
	}
	// a webhook's run: quests, but no start
	if _, _, err := r.Enqueue(ctx, a, "webhook", `{"x":1}`, "", ""); err != nil {
		t.Fatal(err)
	}
	r.StartReady(ctx, time.Now().UTC())
	r.Wait()
	if len(sink.starts) != 2 || sink.starts[1] {
		t.Fatalf("starts = %v", sink.starts)
	}
}
