package memory_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/memory"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func open(t *testing.T) (storage.Store, string) {
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	t.Cleanup(func() { st.Close() })
	st.Migrate(context.Background())
	p, _ := st.Repos().Create(context.Background(), storage.Repo{Name: "p", Path: t.TempDir()})
	return st, p.ID
}

// Notes go into every new conversation; too long, they are compacted (the
// old ones kept, and restorable).
func TestMemory(t *testing.T) {
	ctx := context.Background()
	st, pid := open(t)
	compacted := 0
	svc := memory.New(st, func(_ context.Context, _, _ string, items []storage.Memory) ([]string, error) {
		compacted++
		return []string{"gộp " + items[0].Text}, nil
	})
	svc.Limit = 60
	if _, err := svc.Add(ctx, pid, "agt_1", "  ", "person", "a"); err == nil {
		t.Fatal("an empty note was taken")
	}
	svc.Add(ctx, pid, "agt_1", "project dùng pnpm", "person", "human:a")
	if b := svc.Block(ctx, pid, "agt_1"); !strings.Contains(b, "- project dùng pnpm") {
		t.Fatalf("block = %q", b)
	}
	if svc.Block(ctx, pid, "agt_2") != "" {
		t.Fatal("another agent has a block")
	}
	svc.Add(ctx, pid, "agt_1", "không sửa file generated, chạy pnpm test trước khi báo xong", "agent", "Trợ lý")
	if compacted != 1 {
		t.Fatalf("compacted %d times", compacted)
	}
	list, _ := st.Memories().List(ctx, pid, "agt_1")
	if len(list) != 1 || list[0].Text != "gộp project dùng pnpm" || list[0].Source != "compact" {
		t.Fatalf("after compaction = %+v", list)
	}
	revs, _ := st.Memories().Revisions(ctx, pid, "agt_1")
	if len(revs) != 1 || len(revs[0].Items) != 2 {
		t.Fatalf("revisions = %+v", revs)
	}
	if err := svc.Restore(ctx, revs[0].ID, "human:a"); err != nil {
		t.Fatal(err)
	}
	if list, _ = st.Memories().List(ctx, pid, "agt_1"); len(list) != 2 {
		t.Fatalf("restored = %+v", list)
	}
	if revs, _ = st.Memories().Revisions(ctx, pid, "agt_1"); len(revs) != 2 { // what it was before the restore, too
		t.Fatalf("revisions after restore = %d", len(revs))
	}
	svc.SetAuto(ctx, pid, true)
	if !svc.Auto(ctx, pid) {
		t.Fatal("auto not kept")
	}
}

// A note added while the model compacts is not lost with the old ones.
func TestCompactKeepsNewNotes(t *testing.T) {
	ctx := context.Background()
	st, pid := open(t)
	svc := memory.New(st, func(ctx context.Context, _, _ string, items []storage.Memory) ([]string, error) {
		st.Memories().Create(ctx, storage.Memory{ProjectID: pid, AgentID: "agt_1", Text: "ghi trong lúc gộp"})
		return []string{"gộp"}, nil
	})
	svc.Add(ctx, pid, "agt_1", "a", "person", "human:a")
	svc.Add(ctx, pid, "agt_1", "b", "person", "human:a")
	if err := svc.Compact(ctx, pid, "agt_1", "thử"); err != nil {
		t.Fatal(err)
	}
	list, _ := st.Memories().List(ctx, pid, "agt_1")
	if len(list) != 2 || list[0].Text != "gộp" || list[1].Text != "ghi trong lúc gộp" {
		t.Fatalf("after compaction = %+v", list)
	}
}

// ADR-134: core notes are in the prompt within their cap; a topic is one
// index line (its newest summary), within the index's cap.
func TestRender(t *testing.T) {
	list := []storage.Memory{
		{Text: "cũ nhất, bị cắt"},
		{Text: "Repo dùng pnpm"},
		{Text: "Hoàn tiền qua job refund_sync", Topic: "thanh-toan", Summary: "hoàn tiền"},
		{Text: "Webhook Stripe ở /hooks/stripe", Topic: "thanh-toan", Summary: "hoàn tiền, webhook"},
		{Text: "Deploy bằng make release", Topic: "deploy"},
		{Text: "Không sửa file generated"},
	}
	b := memory.Render(list, 40, 1000)
	for _, want := range []string{"- Repo dùng pnpm\n- Không sửa file generated", "1 older notes not shown", "- deploy: Deploy bằng make release\n- thanh-toan: hoàn tiền, webhook"} {
		if !strings.Contains(b, want) {
			t.Errorf("block lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(b, "cũ nhất") || strings.Contains(b, "refund_sync") || strings.Contains(b, "/hooks/stripe") {
		t.Errorf("block has what it should not:\n%s", b)
	}
	if b = memory.Render(list, 1000, 30); !strings.Contains(b, "- deploy:") || strings.Contains(b, "- thanh-toan:") || !strings.Contains(b, "1 more topics") {
		t.Errorf("index cap:\n%s", b)
	}
	if b = memory.Render(list[2:5], 1000, 1000); !strings.Contains(b, "Topics you keep notes on") || strings.Contains(b, "older notes") {
		t.Errorf("topics only:\n%s", b)
	}
	if memory.Render(nil, 10, 10) != "" {
		t.Error("no notes, a block")
	}
	if got := memory.Slug("  Thanh toán / PayPal!! "); got != "thanh-toan-paypal" {
		t.Errorf("slug = %q", got)
	}
}

// A topic grown past its limit is compacted on its own; the core notes and
// other topics stay, the summary too, and the revision keeps everything.
func TestCompactTopic(t *testing.T) {
	ctx := context.Background()
	st, pid := open(t)
	var got []storage.Memory
	svc := memory.New(st, func(_ context.Context, _, _ string, items []storage.Memory) ([]string, error) {
		got = items
		return []string{"gộp thanh toán"}, nil
	})
	svc.Limit, svc.TopicLimit = 1000, 30
	svc.Add(ctx, pid, "agt_1", "Repo dùng pnpm", "person", "a")
	svc.Keep(ctx, storage.Memory{ProjectID: pid, AgentID: "agt_1", Text: "Deploy bằng make", Topic: "deploy"})
	svc.Keep(ctx, storage.Memory{ProjectID: pid, AgentID: "agt_1", Text: "Hoàn tiền qua refund_sync", Topic: "Thanh toán", Summary: "hoàn tiền"})
	if got != nil {
		t.Fatal("compacted under the limit")
	}
	svc.Keep(ctx, storage.Memory{ProjectID: pid, AgentID: "agt_1", Text: "Webhook Stripe ở /hooks", Topic: "thanh-toan"})
	if len(got) != 2 || got[0].Topic != "thanh-toan" {
		t.Fatalf("compacted = %+v", got)
	}
	list, _ := st.Memories().List(ctx, pid, "agt_1")
	if len(list) != 3 || list[0].Text != "Repo dùng pnpm" || list[1].Topic != "deploy" ||
		list[2].Text != "gộp thanh toán" || list[2].Topic != "thanh-toan" || list[2].Summary != "hoàn tiền" {
		t.Fatalf("after = %+v", list)
	}
	revs, _ := st.Memories().Revisions(ctx, pid, "agt_1")
	if len(revs) != 1 || len(revs[0].Items) != 4 {
		t.Fatalf("revisions = %+v", revs)
	}
	svc.Restore(ctx, revs[0].ID, "a")
	if list, _ = st.Memories().List(ctx, pid, "agt_1"); len(list) != 4 || list[2].Topic != "thanh-toan" || list[2].Summary != "hoàn tiền" {
		t.Fatalf("restored = %+v", list)
	}
}
