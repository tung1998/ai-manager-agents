package channels

import (
	"context"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// The person who wrote to a bot is shown as the office account their id is linked to.
func TestSenderOfLinkedAccount(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := st.Users().Create(ctx, storage.User{Email: "a@b.co", Name: "Tùng", Role: storage.RoleAdmin, PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Users().SetChatIDs(ctx, u.ID, "7", ""); err != nil {
		t.Fatal(err)
	}
	m := &Manager{store: st}
	ch := storage.Channel{Kind: "discord"}
	s := m.senderOf(ctx, ch, Incoming{UserID: "7", UserName: "an", FullName: "An", ChatID: "c1", MessageID: "m1", Private: true})
	if s == nil || s.AccountID != u.ID || s.Account != "Tùng" || s.Link != "https://discord.com/channels/@me/c1/m1" {
		t.Fatalf("linked = %+v", s)
	}
	if s := m.senderOf(ctx, ch, Incoming{UserID: "8", UserName: "binh"}); s == nil || s.AccountID != "" || s.Username != "binh" {
		t.Fatalf("not linked = %+v", s)
	}
	if s := m.senderOf(ctx, storage.Channel{Kind: "telegram"}, Incoming{UserID: "7"}); s.AccountID != "" { // a Discord id, not a Telegram one
		t.Fatalf("other platform = %+v", s)
	}
}
