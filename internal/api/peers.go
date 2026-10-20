package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Other offices (ADR-167): admins pair this office with another and pick the
// projects it may reach; the other office calls /api/peer/* with its key.

func (s *server) peerRoutes(mux *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.Handler { return s.requireRole(storage.RoleAdmin, h) }
	mux.Handle("GET /api/peers", admin(s.listPeers))
	mux.Handle("POST /api/peers/invite", admin(s.invitePeer))
	mux.Handle("POST /api/peers/join", admin(s.joinPeer))
	mux.Handle("PUT /api/peers/{id}/shared", admin(s.sharePeer))
	mux.Handle("POST /api/peers/{id}/refresh", admin(s.refreshPeer))
	mux.Handle("DELETE /api/peers/{id}", admin(s.removePeer))

	// the other office: no session, its key (the pairing: the invite)
	mux.HandleFunc("POST /api/peer/pair", s.peerPair)
	mux.HandleFunc("POST /api/peer/relay", s.peerCall(s.peerRelay))
	mux.HandleFunc("GET /api/peer/projects", s.peerCall(s.peerProjects))
	mux.HandleFunc("PUT /api/peer/projects", s.peerCall(s.peerSetProjects))
}

type peerDTO struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	URL        string               `json:"url"`
	SelfURL    string               `json:"self_url"`
	Shared     []string             `json:"shared"`
	Remote     []peer.RemoteProject `json:"remote"`
	As         string               `json:"as"`
	CreatedAt  time.Time            `json:"created_at"`
	LastUsedAt *time.Time           `json:"last_used_at,omitempty"`
}

func toPeerDTO(p peer.Peer) peerDTO { // never its keys
	return peerDTO{ID: p.ID, Name: p.Name, URL: p.URL, SelfURL: p.SelfURL, Shared: nonNil(p.Shared), Remote: p.Remote,
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
	writeJSON(w, http.StatusOK, map[string]any{"peers": out, "self_name": host})
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

// ---- called by the other office ----

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

func (s *server) peerProjects(w http.ResponseWriter, r *http.Request, p peer.Peer) {
	writeJSON(w, http.StatusOK, map[string]any{"projects": s.cfg.Peers.SharedProjects(r.Context(), p)})
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
