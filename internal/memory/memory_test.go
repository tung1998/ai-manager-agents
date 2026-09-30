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
