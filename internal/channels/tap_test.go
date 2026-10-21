package channels_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/channels"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// takeTicket takes the messages that mention a ticket.
type takeTicket struct {
	mu    sync.Mutex
	taken []string
}

func (x *takeTicket) Take(_ context.Context, _ storage.Channel, in channels.Incoming) bool {
	if !strings.Contains(in.Text, "ticket") {
		return false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.taken = append(x.taken, in.Text)
	return true
}

// ADR-171: a tap takes its messages before the rules; the rest go on as
// before (here: no rule, so the channel's refusal).
func TestTapBeforeRules(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop"})
	st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "telegram", Name: "Hỗ trợ", Enabled: true,
		Allow: []string{"*"}, Refusal: "Không có quy tắc."})
	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	m := channels.NewManager(st, nil, trigger.New(st, chatExec{}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	tap := &takeTicket{}
	m.SetTap(tap)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)

	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", Text: "ticket: hàng lỗi", Private: true, Addressed: true}
	bot.in <- channels.Incoming{ChatID: "42", UserID: "7", Text: "xin chào", Private: true, Addressed: true}
	if got := bot.wait(t, "42", 1); got[0] != "Không có quy tắc." {
		t.Fatalf("not taken: the rules answer, got %v", got)
	}
	time.Sleep(100 * time.Millisecond)
	tap.mu.Lock()
	defer tap.mu.Unlock()
	if len(tap.taken) != 1 || tap.taken[0] != "ticket: hàng lỗi" {
		t.Fatalf("taken = %v", tap.taken)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.sent["42"]) != 1 {
		t.Fatalf("a taken message got the rules' answer too: %v", bot.sent["42"])
	}
}
