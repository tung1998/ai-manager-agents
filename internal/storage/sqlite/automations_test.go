package sqlite_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func TestRecordRunConcurrentFailuresCount(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	a, err := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "a", Source: "schedule", Action: "chat", AgentID: "ag"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.Automations().RecordRun(ctx, a.ID, now, "fail", "boom", 5, ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := st.Automations().Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failures != 2 {
		t.Fatalf("failures = %d, want 2 (both concurrent increments should count)", got.Failures)
	}
}

func TestRecordRunResetAndDisable(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	a, err := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "a", Source: "schedule", Action: "chat", AgentID: "ag", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if err := st.Automations().RecordRun(ctx, a.ID, now, "fail", "boom", 3, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.Automations().Get(ctx, a.ID)
	if got.Failures != 2 || !got.Enabled {
		t.Fatalf("after 2 failures = %+v", got)
	}
	// a success in between resets the streak
	if err := st.Automations().RecordRun(ctx, a.ID, now, "ok", "", 3, "conv1"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Automations().Get(ctx, a.ID)
	if got.Failures != 0 || got.Config.ConversationID != "conv1" {
		t.Fatalf("after ok = %+v", got)
	}
	// reaching the limit disables it
	for i := 0; i < 3; i++ {
		if err := st.Automations().RecordRun(ctx, a.ID, now, "fail", "boom", 3, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = st.Automations().Get(ctx, a.ID)
	if got.Enabled || got.DisabledCode != "failures" || got.Failures != 3 {
		t.Fatalf("after 3 failures = %+v", got)
	}
	// a budget stop ("skip") leaves the streak untouched
	if err := st.Automations().RecordRun(ctx, a.ID, now, "skip", "", 3, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Automations().Get(ctx, a.ID)
	if got.Failures != 3 {
		t.Fatalf("after skip, failures changed = %+v", got)
	}
}
