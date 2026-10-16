package burn_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/burn"
)

// An automation's quests (ADR-132): a project with no Burn yet gets one with
// the defaults; a title an open piece has (case and spaces aside) is not
// given twice, one done is; without start the Burn stays stopped.
func TestAddQuestsDedupes(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	added, started, err := f.svc.AddQuests(ctx, f.project.ID, []burn.Quest{{Title: "Sửa lỗi đăng nhập", Detail: "trang /login"}, {Title: "Thêm test"}}, false, "auto:x")
	if err != nil || added != 2 || started {
		t.Fatalf("added %d started %v err %v", added, started, err)
	}
	b, err := f.st.Burn().Session(ctx, f.project.ID)
	if err != nil || b.State != "stopped" || b.AgentID == "" {
		t.Fatalf("session = %+v, %v", b, err)
	}
	items, _ := f.st.Burn().Items(ctx, b.ID)
	for _, it := range items {
		if it.Title == "Thêm test" { // done: the same title may come again
			it.Status = "done"
			f.st.Burn().UpdateItem(ctx, it)
		}
	}
	added, _, err = f.svc.AddQuests(ctx, f.project.ID, []burn.Quest{{Title: "  sửa LỖI   đăng nhập "}, {Title: "Thêm test"}}, false, "auto:x")
	if err != nil || added != 1 {
		t.Fatalf("again: added %d, %v", added, err)
	}
	items, _ = f.st.Burn().Items(ctx, b.ID)
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
}

// With start, a stopped Burn starts on its settings once a quest was added,
// with the suggested stop time when it had none.
func TestAddQuestsStartsTheBurn(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	if added, started, err := f.svc.AddQuests(ctx, f.project.ID, nil, true, "auto:x"); added != 0 || started || err != nil {
		t.Fatalf("no quest: %d %v %v", added, started, err)
	}
	added, started, err := f.svc.AddQuests(ctx, f.project.ID, []burn.Quest{{Title: "Dọn log"}}, true, "auto:sáng")
	if err != nil || added != 1 || !started {
		t.Fatalf("added %d started %v err %v", added, started, err)
	}
	defer f.svc.Stop(context.Background(), f.project.ID)
	b, _ := f.st.Burn().Session(ctx, f.project.ID)
	if !b.Active() || b.StartedBy != "auto:sáng" || b.EndsAt == nil || b.EndsAt.Before(time.Now().Add(7*time.Hour)) {
		t.Fatalf("session = %+v", b)
	}
	// running already: quests are added, nothing more starts
	if _, started, _ := f.svc.AddQuests(ctx, f.project.ID, []burn.Quest{{Title: "Khác"}}, true, "auto:x"); started {
		t.Fatal("started again")
	}
}

// burn_quest from any chat of the project: added, and read back with burn_quests.
func TestQuestTools(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sc := actions.Scope{ProjectID: f.project.ID, Agent: "Thực thi"}
	if _, err := f.svc.Tool(ctx, sc, "burn_quest", burn.ToolInput{Title: "Không mô tả"}); err == nil {
		t.Fatal("a quest with no detail was added")
	}
	out, err := f.svc.Tool(ctx, sc, "burn_quest", burn.ToolInput{Title: "Viết test", Detail: "cho gói features"})
	if err != nil || !strings.Contains(out, "added to the board") {
		t.Fatalf("quest: %q, %v", out, err)
	}
	if _, err := f.svc.Tool(ctx, sc, "burn_quest", burn.ToolInput{Title: "viết  TEST", Detail: "lại"}); err == nil {
		t.Fatal("the same open quest was added twice")
	}
	out, err = f.svc.Tool(ctx, sc, "burn_quests", burn.ToolInput{})
	if err != nil || !strings.Contains(out, "[found] Viết test") {
		t.Fatalf("quests: %q, %v", out, err)
	}
}
