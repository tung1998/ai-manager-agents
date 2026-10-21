// Package support is the customer care feature pack (ADR-171): tickets with
// their history, sources (bot, Crisp, form, API), playbooks and hooks. It
// plugs into core through small points that do nothing while the pack is off
// for a project (features.Support, off by default): a bot Tap, routes that
// answer 404, agent tools listed for none. Core never imports this package.
package support

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/collections"

	"bitbucket.org/senprints/agent-office/internal/channels"
	"bitbucket.org/senprints/agent-office/internal/features"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Service is the pack.
type Service struct {
	store       storage.Store
	collections *collections.Service
	ensured     sync.Map // project id → its collections are made
	runner      Runner
	limit       limiter
	mu          sync.Mutex
	watchers    map[string]chan struct{} // ticket id → closed at its next change

	box       Box
	botSend   func(ctx context.Context, channelID, chatID, text string) error
	client    *http.Client
	kick      chan struct{} // something to deliver now
	deliverMu sync.Mutex
	lastSweep time.Time
	lastSync  time.Time
}

// New builds the pack.
func New(store storage.Store) *Service {
	return &Service{store: store, collections: collections.New(store), client: &http.Client{Timeout: 15 * time.Second}, kick: make(chan struct{}, 1)}
}

// On reports whether a project has the pack on (its collections made the
// first time it is).
func (s *Service) On(ctx context.Context, projectID string) bool {
	if projectID == "" || !features.On(ctx, s.store, projectID, features.Support) {
		return false
	}
	if _, done := s.ensured.Load(projectID); !done {
		if err := s.ensure(ctx, projectID); err != nil {
			slog.Error("support: collections", "project", projectID, "err", err)
		} else {
			s.ensured.Store(projectID, true)
		}
	}
	return true
}

// Collections the pack declares (ADR-172): the project can add fields; a
// collection of the same key the project made itself is left as it is.
var Collections = []storage.Collection{
	{Key: "customers", Name: "Khách hàng", Pack: "support", Description: "Khách của các ticket: liên hệ, nhãn, ghi chú.", Fields: []storage.Field{
		{Name: "name", Label: "Tên", Type: storage.FieldText, Required: true},
		{Name: "email", Label: "Email", Type: storage.FieldText, Unique: true},
		{Name: "phone", Label: "Điện thoại", Type: storage.FieldText, Indexed: true},
		{Name: "external_ref", Label: "Mã ở hệ thống khác", Type: storage.FieldText, Indexed: true},
		{Name: "tags", Label: "Nhãn", Type: storage.FieldTags},
		{Name: "notes", Label: "Ghi chú", Type: storage.FieldText},
	}},
	{Key: "playbooks", Name: "Phương án phản hồi", Pack: "support", Description: "Tài liệu, file, link, câu trả lời mẫu agent dùng để trả lời khách.", Fields: []storage.Field{
		{Name: "title", Label: "Tiêu đề", Type: storage.FieldText, Required: true},
		{Name: "kind", Label: "Loại", Type: storage.FieldEnum, Options: []string{"doc", "file", "link", "macro"}, Required: true, Indexed: true},
		{Name: "source", Label: "Nguồn (đường dẫn, link)", Type: storage.FieldText},
		{Name: "content", Label: "Nội dung", Type: storage.FieldText},
		{Name: "case_type", Label: "Loại case", Type: storage.FieldText, Indexed: true},
		{Name: "enabled", Label: "Đang dùng", Type: storage.FieldBool},
		{Name: "synced_hash", Label: "Đồng bộ", Type: storage.FieldText},
	}},
}

// ensure makes the pack's collections a project does not have yet, and
// adds to the pack's own the fields a newer pack has.
func (s *Service) ensure(ctx context.Context, projectID string) error {
	for _, c := range Collections {
		if old, err := s.store.Collections().Get(ctx, projectID, c.Key); err == nil {
			if old.Pack != "support" {
				continue // the project's own collection of that key
			}
			fields, added := old.Fields, false
			for _, f := range c.Fields {
				if !hasField(fields, f.Name) {
					fields, added = append(fields, f), true
				}
			}
			if added {
				old.Fields = fields
				if _, err := s.collections.Define(ctx, projectID, old); err != nil {
					return err
				}
			}
			continue
		} else if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if _, err := s.collections.Define(ctx, projectID, c); err != nil {
			return err
		}
	}
	return nil
}

// Take is the bot Tap: a message of a bot a support source (kind bot) of a
// project with the pack on listens to becomes a customer message; anything
// else goes to the rules as before.
func (s *Service) Take(ctx context.Context, ch storage.Channel, in channels.Incoming) bool {
	if in.Text == "" || !s.On(ctx, ch.ProjectID) {
		return false
	}
	list, err := s.store.Support().Sources(ctx, ch.ProjectID)
	if err != nil {
		return false
	}
	for _, src := range list {
		if src.Kind != "bot" || !src.Enabled || str(src.Config["channel_id"]) != ch.ID {
			continue
		}
		msgID := in.MessageID
		if msgID != "" {
			msgID = ch.Kind + ":" + in.ChatID + ":" + msgID
		}
		inb := Inbound{ExternalID: msgID, Thread: in.ChatID, Text: in.Text,
			Customer: Contact{Name: firstNonEmpty(in.FullName, in.UserName), Ref: ch.Kind + ":" + in.UserID}}
		go func() { // the bot's chat is held meanwhile: answer it after
			if _, err := s.Receive(context.WithoutCancel(ctx), src, inb); err != nil {
				slog.Warn("support: bot message", "source", src.ID, "err", err)
			}
		}()
		return true
	}
	return false
}

func hasField(fields []storage.Field, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// Routes adds the pack's API: /api/projects/{id}/support/... for the
// dashboard (404 where the pack is off) and /hooks/support/{source} for
// senders, with the source's token.
func (s *Service) Routes(mux *http.ServeMux, auth, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/projects/{id}/support", auth(s.gate(s.status)))
	s.adminRoutes(mux, auth, admin)
	mux.Handle("POST /hooks/support/{source}", http.HandlerFunc(s.inbound))
	mux.Handle("GET /hooks/support/{source}/messages/{event}", http.HandlerFunc(s.inboundResult))
	mux.Handle("GET /hooks/support/{source}/tickets/{ticket}", http.HandlerFunc(s.inboundTicket))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
