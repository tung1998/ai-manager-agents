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

	"bitbucket.org/senprints/agent-office/internal/collections"

	"bitbucket.org/senprints/agent-office/internal/channels"
	"bitbucket.org/senprints/agent-office/internal/features"
	"bitbucket.org/senprints/agent-office/internal/officetools"
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
}

// New builds the pack.
func New(store storage.Store) *Service {
	return &Service{store: store, collections: collections.New(store)}
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
	}},
}

// ensure makes the pack's collections a project does not have yet.
func (s *Service) ensure(ctx context.Context, projectID string) error {
	for _, c := range Collections {
		if _, err := s.store.Collections().Get(ctx, projectID, c.Key); err == nil {
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

// Take is the bot Tap: a message of a channel the pack has as a source, in a
// project with the pack on. Anything else goes to the rules as before.
func (s *Service) Take(ctx context.Context, ch storage.Channel, in channels.Incoming) bool {
	if !s.On(ctx, ch.ProjectID) {
		return false
	}
	return false // no sources yet
}

// Tools lists the pack's agent tools (none where the pack is off).
func (s *Service) Tools(sc officetools.Scope) []officetools.Tool {
	if !s.On(context.Background(), sc.ProjectID) {
		return nil
	}
	return nil
}

// Call runs one of the pack's tools.
func (s *Service) Call(ctx context.Context, sc officetools.Scope, name string, raw json.RawMessage) (string, bool) {
	return "Không có công cụ " + name + ".", true
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
