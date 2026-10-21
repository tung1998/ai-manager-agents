package api

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/assistant"
	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/transfer"
)

// Other offices (ADR-167): admins pair this office with another and pick the
// projects it may reach; the other office calls /api/peer/* with its key.

func (s *server) peerRoutes(mux *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.Handler { return s.requireRole(storage.RoleAdmin, h) }
	mux.Handle("GET /api/peers", admin(s.listPeers))
	mux.Handle("POST /api/peers/invite", admin(s.invitePeer))
	mux.Handle("POST /api/peers/join", admin(s.joinPeer))
	mux.Handle("PUT /api/peers/{id}/shared", admin(s.sharePeer))
	mux.Handle("PUT /api/peers/{id}/admin", admin(s.adminPeer))
	mux.Handle("POST /api/peers/{id}/refresh", admin(s.refreshPeer))
	mux.Handle("DELETE /api/peers/{id}", admin(s.removePeer))
	// pairing by address (ADR-177)
	mux.Handle("POST /api/peers/connect", admin(s.connectPeer))
	mux.Handle("DELETE /api/peers/outgoing/{id}", admin(s.cancelConnect))
	mux.Handle("POST /api/peers/requests/{id}/accept", admin(s.acceptRequest))
	mux.Handle("DELETE /api/peers/requests/{id}", admin(s.dropRequest))

	// the other office: no session, its key (the pairing: the invite)
	mux.HandleFunc("POST /api/peer/pair", s.peerPair)
	mux.HandleFunc("POST /api/peer/request", s.peerRequest)
	mux.HandleFunc("POST /api/peer/request/answer", s.peerAnswer)
	mux.HandleFunc("POST /api/peer/relay", s.peerCall(s.peerRelay))
	mux.HandleFunc("GET /api/peer/projects", s.peerCall(s.peerProjects))
	mux.HandleFunc("PUT /api/peer/projects", s.peerCall(s.peerSetProjects))
	mux.HandleFunc("GET /api/peer/chats/{id}", s.peerCall(s.peerReadChat))
	mux.HandleFunc("GET /api/peer/projects/{id}/pack", s.peerCall(s.peerPack))
	mux.HandleFunc("POST /api/peer/search", s.peerCall(s.peerSearch))
	mux.HandleFunc("POST /api/peer/consult", s.peerCall(s.peerConsult))
	mux.HandleFunc("POST /api/peer/consult/answer", s.peerCall(s.peerConsultAnswer))
}

type peerDTO struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	URL        string               `json:"url"`
	SelfURL    string               `json:"self_url"`
	Shared     []string             `json:"shared"`
	Remote     []peer.RemoteProject `json:"remote"`
	Admin      bool                 `json:"admin"`
	As         string               `json:"as"`
	CreatedAt  time.Time            `json:"created_at"`
	LastUsedAt *time.Time           `json:"last_used_at,omitempty"`
}

func toPeerDTO(p peer.Peer) peerDTO { // never its keys
	return peerDTO{ID: p.ID, Name: p.Name, URL: p.URL, SelfURL: p.SelfURL, Shared: nonNil(p.Shared), Remote: p.Remote, Admin: p.Admin,
		As: strings.TrimPrefix(p.As, "human:"), CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt}
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func (s *server) listPeers(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Peers.List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]peerDTO, 0, len(list))
	for _, p := range list {
		out = append(out, toPeerDTO(p))
	}
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{"peers": out, "self_name": host,
		"requests": s.cfg.Peers.Requests(r.Context()), "outgoing": s.cfg.Peers.OutgoingList(r.Context())})
}

func (s *server) invitePeer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Shared []string `json:"shared"`
	}
	if !decode(w, r, &in) {
		return
	}
	code, expires, err := s.cfg.Peers.Invite(r.Context(), "human:"+userFrom(r).Email, in.Name, in.URL, in.Shared)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditAction(r, "peer.invite", "", map[string]any{"url": in.URL, "shared": in.Shared})
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expires_at": expires}) // shown once
}

func (s *server) joinPeer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.cfg.Peers.Join(r.Context(), "human:"+userFrom(r).Email, in.Code, in.Name, in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditAction(r, "peer.join", p.ID, map[string]any{"name": p.Name, "url": p.URL})
	writeJSON(w, http.StatusCreated, toPeerDTO(p))
}

func (s *server) sharePeer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Shared []string `json:"shared"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if err := s.cfg.Peers.SetShared(r.Context(), id, in.Shared); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.writeDomainError(w, r, err)
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	s.auditAction(r, "peer.share", id, map[string]any{"shared": in.Shared})
	w.WriteHeader(http.StatusNoContent)
}

// adminPeer lets the paired office run this office's assistant as
// administrator on this machine, or stops it.
func (s *server) adminPeer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Admin bool `json:"admin"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if err := s.cfg.Peers.SetAdmin(r.Context(), id, in.Admin); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "peer.admin", id, map[string]any{"admin": in.Admin})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) refreshPeer(w http.ResponseWriter, r *http.Request) {
	p, err := s.cfg.Peers.Refresh(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		s.writeDomainError(w, r, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toPeerDTO(p))
}

func (s *server) removePeer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Peers.Remove(r.Context(), id); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.auditAction(r, "peer.remove", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) connectPeer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL     string   `json:"url"`
		Name    string   `json:"name"`
		SelfURL string   `json:"self_url"`
		Shared  []string `json:"shared"`
	}
	if !decode(w, r, &in) {
		return
	}
	o, err := s.cfg.Peers.Connect(r.Context(), "human:"+userFrom(r).Email, in.URL, in.Name, in.SelfURL, in.Shared)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditAction(r, "peer.connect", o.ID, map[string]any{"url": o.URL, "shared": in.Shared})
	writeJSON(w, http.StatusCreated, o)
}

func (s *server) cancelConnect(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Peers.CancelOutgoing(r.Context(), r.PathValue("id")); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) acceptRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string   `json:"name"`
		Shared []string `json:"shared"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.cfg.Peers.AcceptRequest(r.Context(), "human:"+userFrom(r).Email, r.PathValue("id"), in.Name, in.Shared)
	if errors.Is(err, peer.ErrNotFoundRequest) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.auditAction(r, "peer.accept", p.ID, map[string]any{"name": p.Name, "url": p.URL, "shared": in.Shared})
	go s.pushIncidents(context.Background(), false)
	writeJSON(w, http.StatusCreated, toPeerDTO(p))
}

func (s *server) dropRequest(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Peers.DropRequest(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.auditAction(r, "peer.reject", r.PathValue("id"), nil)
	go s.pushIncidents(context.Background(), false)
	w.WriteHeader(http.StatusNoContent)
}

// ---- called by the other office ----

// peerRequests caps the public pairing requests: per address and in all.
var peerRequests = struct {
	sync.Mutex
	n   map[string]int
	all int
	t   time.Time
}{n: map[string]int{}}

// peerRequest: another office asks to pair (no key yet). Small, few, kept
// for an admin; nothing is called from here.
func (s *server) peerRequest(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = h
	}
	peerRequests.Lock()
	if time.Since(peerRequests.t) > time.Hour {
		peerRequests.n, peerRequests.all, peerRequests.t = map[string]int{}, 0, time.Now()
	}
	peerRequests.n[ip]++
	peerRequests.all++
	over := peerRequests.n[ip] > 10 || peerRequests.all > 60
	peerRequests.Unlock()
	if over {
		writeError(w, http.StatusTooManyRequests, "quá nhiều yêu cầu ghép nối, thử lại sau")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "yêu cầu quá lớn")
		return
	}
	// from this machine: its address and the host it asked for both local
	// (behind a reverse proxy here every request comes from 127.0.0.1)
	addr, _ := netip.ParseAddr(ip)
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	hostIP, herr := netip.ParseAddr(strings.Trim(host, "[]"))
	local := addr.IsLoopback() && (strings.EqualFold(host, "localhost") || herr == nil && hostIP.IsLoopback())
	switch err := s.cfg.Peers.TakeRequest(r.Context(), body, ip, local); {
	case errors.Is(err, peer.ErrDenied):
		writeError(w, http.StatusBadRequest, "yêu cầu ghép nối không hợp lệ")
		return
	case errors.Is(err, peer.ErrRequestsFull):
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.log.Info("peer: pairing request", "remote", ip)
	go s.pushIncidents(context.Background(), false) // its admins see it in the bell
	w.WriteHeader(http.StatusAccepted)
}

// peerAnswer: the office this one asked accepted, with the key this one gave it.
func (s *server) peerAnswer(w http.ResponseWriter, r *http.Request) {
	key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "quá lớn")
		return
	}
	out, err := s.cfg.Peers.Answer(r.Context(), strings.TrimSpace(key), body)
	if errors.Is(err, peer.ErrDenied) {
		writeError(w, http.StatusForbidden, "không có yêu cầu ghép nối này")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.log.Info("peer: paired by address", "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

func (s *server) peerPair(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	out, err := s.cfg.Peers.Accept(r.Context(), body)
	if errors.Is(err, peer.ErrDenied) {
		writeError(w, http.StatusForbidden, "mã mời không đúng hoặc đã hết hạn")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.log.Info("peer: paired", "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

// peerLimit: calls one paired office may make a minute.
const peerLimit = 120

var peerCalls = struct {
	sync.Mutex
	n map[string]int
	t time.Time
}{n: map[string]int{}}

// peerCall authenticates the other office's key and caps its calls.
func (s *server) peerCall(h func(w http.ResponseWriter, r *http.Request, p peer.Peer)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		p, found := s.cfg.Peers.Auth(r.Context(), strings.TrimSpace(key))
		if !ok || !found {
			writeError(w, http.StatusUnauthorized, "khóa office không hợp lệ")
			return
		}
		peerCalls.Lock()
		if time.Since(peerCalls.t) > time.Minute {
			peerCalls.n, peerCalls.t = map[string]int{}, time.Now()
		}
		peerCalls.n[p.ID]++
		over := peerCalls.n[p.ID] > peerLimit
		peerCalls.Unlock()
		if over {
			writeError(w, http.StatusTooManyRequests, "quá nhiều lần gọi, thử lại sau")
			return
		}
		h(w, r, p)
	}
}

func (s *server) peerRelay(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	var m peer.Message
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil { // lenient: the other office may be newer
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat không chạy")
		return
	}
	reply, err := s.cfg.Chat.Receive(r.Context(), p, m)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

// peerConsult: the other office asks an expert here (ADR-177).
func (s *server) peerConsult(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	var q peer.ConsultRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&q); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat không chạy")
		return
	}
	reply, err := s.cfg.Chat.ReceiveConsult(r.Context(), p, q)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

// peerConsultAnswer: the other office answers a question this one asked.
func (s *server) peerConsultAnswer(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	var a peer.ConsultAnswer
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&a); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if s.cfg.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat không chạy")
		return
	}
	if err := s.cfg.Chat.ConsultAnswered(r.Context(), p, a); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) peerProjects(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	writeJSON(w, http.StatusOK, map[string]any{"projects": s.cfg.Peers.SharedProjects(r.Context(), p)})
}

// peerPack: the other office takes a pack (.zip) of a project shared with
// it (ADR-170): config and notes as chosen, chats only those between the two.
func (s *server) peerPack(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	id := r.PathValue("id")
	if !slices.Contains(p.Shared, id) {
		writeError(w, http.StatusForbidden, "project này không mở cho office của bạn")
		return
	}
	if s.cfg.Transfer == nil {
		writeError(w, http.StatusServiceUnavailable, "không xuất project được ở đây")
		return
	}
	keep := func(c storage.Conversation) bool { return s.cfg.Chat != nil && s.cfg.Chat.PeerChat(r.Context(), c, p) }
	pack, err := s.cfg.Transfer.ExportPack(r.Context(), id, strings.Split(r.URL.Query().Get("include"), ","), keep)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	var buf bytes.Buffer
	if err := transfer.WriteZip(&buf, pack); err != nil {
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	_, _ = w.Write(buf.Bytes())
}

func (s *server) peerSetProjects(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	var in struct {
		Projects []peer.RemoteProject `json:"projects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.cfg.Peers.SetRemote(r.Context(), p.ID, in.Projects); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// peerReadChat: the other office reads a chat between the two (read_link).
func (s *server) peerReadChat(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	if s.cfg.Chat == nil || s.cfg.Office == nil {
		writeError(w, http.StatusServiceUnavailable, "chat không chạy")
		return
	}
	c, err := s.cfg.Store.Chat().GetConversation(r.Context(), r.PathValue("id"))
	if pid := r.URL.Query().Get("p"); err == nil && pid != "" && c.ProjectID != pid { // asked as a chat of that project (ADR-169)
		err = storage.ErrNotFound
	}
	if err != nil || !s.cfg.Chat.PeerChat(r.Context(), c, p) {
		writeError(w, http.StatusNotFound, "không có chat này giữa hai office")
		return
	}
	text, err := s.cfg.Office.ReadChat(r.Context(), c.ProjectID, c.ID, r.URL.Query().Get("m"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text, "project": c.ProjectID}) // its project: the caller checks it (ADR-169)
}

// peerSearch: the other office searches a project shared with it, only in
// the chats between the two (search_history).
func (s *server) peerSearch(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	var q peer.Search
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil || strings.TrimSpace(q.Query) == "" {
		writeError(w, http.StatusBadRequest, "thiếu từ khóa")
		return
	}
	if s.cfg.Chat == nil || !p.Opens(q.Project, assistant.ID(r.Context(), s.cfg.Store)) {
		writeError(w, http.StatusForbidden, "project này không mở cho office của bạn")
		return
	}
	days := q.Days
	if days <= 0 {
		days = 30
	}
	found, err := s.cfg.Store.Chat().SearchMessages(r.Context(), q.Query, storage.SearchFilter{ProjectIDs: []string{q.Project},
		Since: time.Now().UTC().AddDate(0, 0, -min(days, 365)), Author: q.Author, Limit: 200})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	hits, ok := []peer.Hit{}, map[string]bool{}
	for _, h := range found {
		if len(hits) == 10 {
			break
		}
		if _, seen := ok[h.ConversationID]; !seen {
			c, err := s.cfg.Store.Chat().GetConversation(r.Context(), h.ConversationID)
			ok[h.ConversationID] = err == nil && s.cfg.Chat.PeerChat(r.Context(), c, p)
		}
		if ok[h.ConversationID] {
			hits = append(hits, peer.Hit{Chat: h.ConversationID, Message: h.MessageID, Project: h.ProjectID, Title: h.Title,
				Author: cmp.Or(h.Author, h.Role), Snippet: h.Snippet, CreatedAt: h.CreatedAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}
