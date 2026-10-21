package support

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

const maxBody = 64 << 10

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// requestError answers err: a bad request is 400, a missing thing 404.
func requestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "không tìm thấy")
	case errors.Is(err, ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "gửi quá nhiều tin, thử lại sau")
	default:
		writeError(w, http.StatusInternalServerError, "lỗi máy chủ")
	}
}

// --- senders: /hooks/support/{source}, the source's token ---

func token(r *http.Request) string {
	if t, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	if t := r.Header.Get("X-Office-Token"); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

// sender: the source the request is for, when its token is right and its
// project has the pack on (anything else: 404, nothing told).
func (s *Service) sender(w http.ResponseWriter, r *http.Request) (storage.SupportSource, bool) {
	src, err := s.store.Support().Source(r.Context(), r.PathValue("source"))
	if err != nil || !TokenOK(src, token(r)) || !s.On(r.Context(), src.ProjectID) {
		writeError(w, http.StatusNotFound, "không tìm thấy")
		return src, false
	}
	return src, true
}

func waitOf(r *http.Request, fallback int) time.Duration {
	if v, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil {
		fallback = v
	}
	return time.Duration(max(fallback, 0)) * time.Second
}

// inbound takes a customer message: {external_id, thread, customer{name,
// email, phone, ref}, subject, text, meta, wait}. With wait (seconds, at most
// 120) it answers once the reply is there; else 202 and the URL to poll.
func (s *Service) inbound(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sender(w, r)
	if !ok {
		return
	}
	var in struct {
		Inbound
		Wait int `json:"wait"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "tin quá lớn")
		return
	}
	if err != nil || json.Unmarshal(body, &in) != nil {
		writeError(w, http.StatusBadRequest, "cần JSON {text, customer, thread, external_id}")
		return
	}
	res, err := s.Receive(r.Context(), src, in.Inbound)
	if err != nil {
		requestError(w, err)
		return
	}
	if d := waitOf(r, in.Wait); d > 0 && res.Status == statusQueue {
		if res, err = s.Wait(r.Context(), res.EventID, d); err != nil {
			requestError(w, err)
			return
		}
	}
	s.writeResult(w, src, res)
}

func (s *Service) writeResult(w http.ResponseWriter, src storage.SupportSource, res Result) {
	out := map[string]any{"ticket_id": res.TicketID, "message_id": res.EventID, "status": res.Status,
		"poll": "/hooks/support/" + src.ID + "/messages/" + res.EventID}
	if res.Reply != nil {
		out["reply"] = res.Reply
	}
	code := http.StatusOK
	if res.Status == statusQueue {
		code = http.StatusAccepted
	}
	writeJSON(w, code, out)
}

// inboundResult: what became of a message (?wait= seconds to wait for it).
func (s *Service) inboundResult(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sender(w, r)
	if !ok {
		return
	}
	msg, err := s.store.Support().Event(r.Context(), r.PathValue("event"))
	if err != nil || msg.SourceID != src.ID || msg.Kind != "customer_msg" {
		writeError(w, http.StatusNotFound, "không tìm thấy")
		return
	}
	res, err := s.Wait(r.Context(), msg.ID, waitOf(r, 0))
	if err != nil {
		requestError(w, err)
		return
	}
	s.writeResult(w, src, res)
}

// inboundTicket: a ticket's conversation as the customer sees it (their
// messages and the replies sent; no drafts, no notes).
func (s *Service) inboundTicket(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sender(w, r)
	if !ok {
		return
	}
	t, err := s.store.Support().Ticket(r.Context(), r.PathValue("ticket"))
	if err != nil || t.SourceID != src.ID {
		writeError(w, http.StatusNotFound, "không tìm thấy")
		return
	}
	evs, _ := s.store.Support().Events(r.Context(), t.ID)
	msgs := []map[string]any{}
	for _, e := range evs {
		if e.Kind == "customer_msg" || e.Kind == "sent" {
			msgs = append(msgs, map[string]any{"id": e.ID, "from": map[bool]string{true: "customer", false: "support"}[e.Kind == "customer_msg"],
				"text": e.Body, "at": e.CreatedAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket_id": t.ID, "status": t.Status, "messages": msgs})
}

// --- the dashboard: /api/projects/{id}/support/..., the pack on ---

func (s *Service) adminRoutes(mux *http.ServeMux, auth, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/projects/{id}/support/sources", auth(s.gate(s.listSources)))
	mux.Handle("POST /api/projects/{id}/support/sources", admin(s.gate(s.createSource)))
	mux.Handle("PATCH /api/projects/{id}/support/sources/{sid}", admin(s.gate(s.updateSource)))
	mux.Handle("POST /api/projects/{id}/support/sources/{sid}/token", admin(s.gate(s.rotateToken)))
	mux.Handle("GET /api/projects/{id}/support/tickets", auth(s.gate(s.listTickets)))
	mux.Handle("GET /api/projects/{id}/support/tickets/{tid}", auth(s.gate(s.getTicket)))
	mux.Handle("POST /api/projects/{id}/support/tickets/{tid}/reply", admin(s.gate(s.reply)))
	mux.Handle("PATCH /api/projects/{id}/support/tickets/{tid}", admin(s.gate(s.patchTicket)))
	mux.Handle("POST /api/projects/{id}/support/drafts/{eid}/approve", admin(s.gate(s.approve)))
	mux.Handle("POST /api/projects/{id}/support/drafts/{eid}/reject", admin(s.gate(s.reject)))
}

// Who did it: the signed-in person (set by the API's auth), else "person".
var Who = func(r *http.Request) string { return "person" }

type sourceInput struct {
	Kind         *string         `json:"kind"`
	Name         *string         `json:"name"`
	AutomationID *string         `json:"automation_id"`
	Mode         *string         `json:"mode"`
	Config       *map[string]any `json:"config"`
	RatePerMin   *int            `json:"rate_per_min"`
	Enabled      *bool           `json:"enabled"`
}

func (s *Service) applySource(ctx context.Context, in sourceInput, src *storage.SupportSource) error {
	if in.Kind != nil {
		src.Kind = *in.Kind
	}
	if in.Name != nil {
		src.Name = strings.TrimSpace(*in.Name)
	}
	if in.AutomationID != nil {
		src.AutomationID = *in.AutomationID
	}
	if in.Mode != nil {
		src.Mode = *in.Mode
	}
	if in.Config != nil {
		src.Config = *in.Config
	}
	if in.RatePerMin != nil {
		src.RatePerMin = *in.RatePerMin
	}
	if in.Enabled != nil {
		src.Enabled = *in.Enabled
	}
	switch {
	case src.Kind != "api" && src.Kind != "form":
		return invalid("loại nguồn %q chưa hỗ trợ (api, form)", src.Kind)
	case src.Mode != "auto" && src.Mode != "approve":
		return invalid("chế độ phải là auto hoặc approve")
	case src.RatePerMin < 1 || src.RatePerMin > 600:
		return invalid("số tin mỗi phút từ 1 đến 600")
	case src.Name == "":
		return invalid("nguồn cần tên")
	}
	_, err := s.answerer(ctx, *src)
	return err
}

func (s *Service) listSources(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Support().Sources(r.Context(), r.PathValue("id"))
	if err != nil {
		requestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": list})
}

// createSource answers the token once: it is not kept, only its hash.
func (s *Service) createSource(w http.ResponseWriter, r *http.Request) {
	var in sourceInput
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "dữ liệu không hợp lệ")
		return
	}
	src := storage.SupportSource{ProjectID: r.PathValue("id"), Kind: "api", Mode: "approve", RatePerMin: 30, Enabled: true}
	if err := s.applySource(r.Context(), in, &src); err != nil {
		requestError(w, err)
		return
	}
	tok, hash := NewToken()
	src.TokenHash = hash
	src, err := s.store.Support().CreateSource(r.Context(), src)
	if err != nil {
		requestError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"source": src, "token": tok, "url": "/hooks/support/" + src.ID})
}

func (s *Service) source(r *http.Request) (storage.SupportSource, error) {
	src, err := s.store.Support().Source(r.Context(), r.PathValue("sid"))
	if err == nil && src.ProjectID != r.PathValue("id") {
		err = storage.ErrNotFound
	}
	return src, err
}

func (s *Service) updateSource(w http.ResponseWriter, r *http.Request) {
	src, err := s.source(r)
	if err != nil {
		requestError(w, err)
		return
	}
	var in sourceInput
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "dữ liệu không hợp lệ")
		return
	}
	in.Kind = nil // a source keeps its kind
	if err := s.applySource(r.Context(), in, &src); err != nil {
		requestError(w, err)
		return
	}
	if err := s.store.Support().UpdateSource(r.Context(), src); err != nil {
		requestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, src)
}

func (s *Service) rotateToken(w http.ResponseWriter, r *http.Request) {
	src, err := s.source(r)
	if err != nil {
		requestError(w, err)
		return
	}
	tok, hash := NewToken()
	src.TokenHash = hash
	if err := s.store.Support().UpdateSource(r.Context(), src); err != nil {
		requestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok})
}

func (s *Service) listTickets(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Support().Tickets(r.Context(), r.PathValue("id"), r.URL.Query().Get("status"), 200)
	if err != nil {
		requestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": list})
}

func (s *Service) getTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Support().Ticket(r.Context(), r.PathValue("tid"))
	if err != nil || t.ProjectID != r.PathValue("id") {
		requestError(w, storage.ErrNotFound)
		return
	}
	evs, err := s.store.Support().Events(r.Context(), t.ID)
	if err != nil {
		requestError(w, err)
		return
	}
	out := map[string]any{"ticket": t, "events": evs}
	if t.CustomerID != "" {
		if x, _, err := s.collections.Get(r.Context(), t.ProjectID, "customers", t.CustomerID); err == nil {
			out["customer"] = x
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) approve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if r.ContentLength != 0 && json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "dữ liệu không hợp lệ")
		return
	}
	if err := s.Approve(r.Context(), r.PathValue("id"), r.PathValue("eid"), in.Text, Who(r)); err != nil {
		requestError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) reject(w http.ResponseWriter, r *http.Request) {
	if err := s.Reject(r.Context(), r.PathValue("id"), r.PathValue("eid"), Who(r)); err != nil {
		requestError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// gate answers 404 for a project without the pack on.
func (s *Service) gate(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.On(r.Context(), r.PathValue("id")) {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"on": true})
}

func (s *Service) reply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "dữ liệu không hợp lệ")
		return
	}
	if err := s.Answer(r.Context(), r.PathValue("id"), r.PathValue("tid"), in.Text, Who(r)); err != nil {
		requestError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) patchTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "dữ liệu không hợp lệ")
		return
	}
	if err := s.SetStatus(r.Context(), r.PathValue("id"), r.PathValue("tid"), in.Status); err != nil {
		requestError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
