package officetools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/memory"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// ADR-134: remember puts a note under a topic; only the topic's line is in
// the prompt, recall reads its notes, by topic or by words.
func TestRememberTopicAndRecall(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	solo, _ := team.PackByKey("solo")
	team.NewService(st, nil).ApplyPack(ctx, p.ID, solo, false)
	agents, _ := st.Agents().List(ctx, p.ID)
	mem := memory.New(st, nil)
	mem.SetAuto(ctx, p.ID, true)
	acts := actions.New(st, nil)
	acts.SetMemory(mem)
	tb := New(st, nil, acts)
	sc := Scope{ProjectID: p.ID, RunRef: "r1", Agent: agents[0].Name, Level: perm.Propose}

	out, isErr := tb.Call(ctx, sc, "remember", []byte(`{"note":"Hoàn tiền PayPal chạy qua job refund_sync","topic":"Thanh toán","summary":"hoàn tiền, webhook PayPal/Stripe"}`))
	if isErr || !strings.Contains(out, "topic thanh-toan") {
		t.Fatalf("remember = %v %s", isErr, out)
	}
	sc.RunRef = "r2"
	tb.Call(ctx, sc, "remember", []byte(`{"note":"Repo dùng pnpm"}`))
	list, _ := st.Memories().List(ctx, p.ID, agents[0].ID)
	if len(list) != 2 || list[0].Topic != "thanh-toan" || list[0].Summary != "hoàn tiền, webhook PayPal/Stripe" || list[1].Topic != "" {
		t.Fatalf("notes = %+v", list)
	}
	b := mem.Block(ctx, p.ID, agents[0].ID)
	if !strings.Contains(b, "- Repo dùng pnpm") || !strings.Contains(b, "- thanh-toan: hoàn tiền, webhook PayPal/Stripe") || strings.Contains(b, "refund_sync") {
		t.Fatalf("block = %s", b)
	}

	if out, isErr = tb.Call(ctx, sc, "recall", []byte(`{"topic":"thanh-toan"}`)); isErr || !strings.Contains(out, "refund_sync") || strings.Contains(out, "pnpm") {
		t.Fatalf("recall topic = %v %s", isErr, out)
	}
	if out, isErr = tb.Call(ctx, sc, "recall", []byte(`{"query":"hoan tien paypal"}`)); isErr || !strings.Contains(out, "[thanh-toan] Hoàn tiền") {
		t.Fatalf("recall query = %v %s", isErr, out)
	}
	if out, _ = tb.Call(ctx, sc, "recall", []byte(`{"query":"pnpm"}`)); !strings.Contains(out, "[core] Repo dùng pnpm") {
		t.Fatalf("recall core = %s", out)
	}
	if out, _ = tb.Call(ctx, sc, "recall", []byte(`{}`)); !strings.Contains(out, "- thanh-toan: hoàn tiền") {
		t.Fatalf("recall topics = %s", out)
	}
	if out, _ = tb.Call(ctx, sc, "recall", []byte(`{"topic":"deploy"}`)); !strings.Contains(out, "No notes match. Topics: thanh-toan") {
		t.Fatalf("recall unknown = %s", out)
	}
	if !tb.Has(sc, "recall") {
		t.Fatal("recall not offered where remember is")
	}
}
