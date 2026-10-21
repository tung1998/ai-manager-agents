package peer

import (
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ids"
)

// Pairing by address (ADR-177): office B enters office A's address; A's
// admin accepts the request it gets, after the two read the same match code.
//
//  1. B makes a nonce and the key A will call it with (keeping only its
//     hash), and POSTs them to A's /api/peer/request (no key: public).
//  2. A keeps the request (that key sealed), shows it to its admins.
//  3. Accepted, A makes the key B will call it with (sealed, kept for a
//     retry) and POSTs it to B's /api/peer/request/answer, authenticated
//     with B's key: B checks nonce and key against its request and pairs;
//     then A pairs.
//
// The match code is over the nonce, both addresses and B's key: one in the
// middle changing any of them changes the code each side shows.

const (
	outgoingKey = "peer_outgoing"
	requestsKey = "peer_requests"
	requestTTL  = 24 * time.Hour
	maxRequests = 20
)

// outgoing is a request this office made to another.
type outgoing struct {
	ID      string    `json:"id"`  // the nonce
	URL     string    `json:"url"` // the other office, as entered here
	Name    string    `json:"name"`
	SelfURL string    `json:"self_url"`
	KeyHash string    `json:"key_hash"` // the key it calls this office with
	Shared  []string  `json:"shared"`
	As      string    `json:"as"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
}

// request is one another office made to this one.
type request struct {
	ID        string    `json:"id"`
	Nonce     string    `json:"nonce"`
	Name      string    `json:"name"`
	URL       string    `json:"url"` // the other office
	To        string    `json:"to"`  // this office, as it reached it
	KeyEnc    string    `json:"key_enc"`
	AnswerEnc string    `json:"answer_enc,omitempty"` // the key it will call with, once accepted (a retry sends the same)
	Code      string    `json:"code"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
	Expires   time.Time `json:"expires"`
}

// Request is a pairing request as an admin sees it.
type Request struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Code      string    `json:"code"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// Outgoing is a request this office waits on, as an admin sees it.
type Outgoing struct {
	ID      string    `json:"id"`
	URL     string    `json:"url"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
}

type requestBody struct {
	Nonce string `json:"nonce"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	To    string `json:"to"`
	Key   string `json:"key"`
}

type answerBody struct {
	Nonce    string          `json:"nonce"`
	Key      string          `json:"key"`
	Name     string          `json:"name"`
	Projects []RemoteProject `json:"projects"`
}

// MatchCode is what both offices show for one request.
func MatchCode(nonce, to, from, key string) string {
	sum := sha256.Sum256([]byte("agent-office pair v1\n" + nonce + "\n" + to + "\n" + from + "\n" + Hash(key)))
	h := strings.ToUpper(hex.EncodeToString(sum[:3]))
	return h[:3] + "-" + h[3:]
}

// Connect asks the office at theirURL to pair: this office is selfURL to it,
// named name, and shares shared. It returns the match code to read out.
func (s *Service) Connect(ctx context.Context, as, theirURL, name, selfURL string, shared []string) (Outgoing, error) {
	their, err := CleanURL(theirURL)
	if err != nil {
		return Outgoing{}, err
	}
	self, err := CleanURL(selfURL)
	if err != nil {
		return Outgoing{}, err
	}
	if name = oneLine(name, 80); name == "" {
		return Outgoing{}, errors.New("thiếu tên office này")
	}
	if their == self {
		return Outgoing{}, errors.New("đây là địa chỉ của chính office này")
	}
	if shared, err = s.projects(ctx, shared); err != nil {
		return Outgoing{}, err
	}
	key := NewKey()
	o := outgoing{ID: randomHex(16), URL: their, Name: name, SelfURL: self, KeyHash: Hash(key), Shared: shared, As: as, Expires: s.now().Add(requestTTL).UTC()}
	o.Code = MatchCode(o.ID, their, self, key)
	if err := s.setOutgoing(ctx, func(list []outgoing) []outgoing {
		list = slices.DeleteFunc(list, func(x outgoing) bool { return x.URL == their }) // asked again: the latest only
		return append(list[max(0, len(list)-maxRequests+1):], o)
	}); err != nil {
		return Outgoing{}, err
	}
	if err := s.do(ctx, http.MethodPost, their+"/api/peer/request", "", requestBody{Nonce: o.ID, Name: name, URL: self, To: their, Key: key}, nil); err != nil {
		_ = s.CancelOutgoing(ctx, o.ID)
		return Outgoing{}, err
	}
	return Outgoing{ID: o.ID, URL: o.URL, Code: o.Code, Expires: o.Expires}, nil
}

func (s *Service) outgoing(ctx context.Context) []outgoing {
	var list []outgoing
	_, _ = s.st.Settings().Get(ctx, outgoingKey, &list)
	now := s.now()
	return slices.DeleteFunc(list, func(o outgoing) bool { return now.After(o.Expires) })
}

func (s *Service) setOutgoing(ctx context.Context, fn func([]outgoing) []outgoing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Settings().Set(ctx, outgoingKey, fn(s.outgoing(ctx)))
}

// OutgoingList are the requests this office waits on.
func (s *Service) OutgoingList(ctx context.Context) []Outgoing {
	out := []Outgoing{}
	for _, o := range s.outgoing(ctx) {
		out = append(out, Outgoing{ID: o.ID, URL: o.URL, Code: o.Code, Expires: o.Expires})
	}
	return out
}

// CancelOutgoing drops a request this office made: an answer to it fails.
func (s *Service) CancelOutgoing(ctx context.Context, id string) error {
	return s.setOutgoing(ctx, func(list []outgoing) []outgoing {
		return slices.DeleteFunc(list, func(o outgoing) bool { return o.ID == id })
	})
}

// ErrRequestsFull: too many requests wait for an admin.
var ErrRequestsFull = errors.New("office này đang có quá nhiều yêu cầu ghép nối chờ duyệt")

// TakeRequest keeps a pairing request another office sent (from ip; local:
// it came from this machine). It calls nothing: this office reaches the
// other only once an admin accepts.
func (s *Service) TakeRequest(ctx context.Context, raw []byte, ip string, local bool) error {
	var in requestBody
	if json.Unmarshal(raw, &in) != nil || len(in.Nonce) < 16 || len(in.Nonce) > 64 || !strings.HasPrefix(in.Key, "ofp_") || len(in.Key) > 100 {
		return ErrDenied
	}
	their, err := CleanURL(in.URL)
	if err != nil {
		return err
	}
	to, err := CleanURL(in.To)
	if err != nil {
		return err
	}
	if their == to {
		return ErrDenied
	}
	// an office on this machine only when the request came from it: what the
	// sender says of its own address ("to") proves nothing (SSRF)
	if loopback(their) && !local {
		return errors.New("địa chỉ office là máy này: chỉ được khi office được hỏi cũng ở máy này")
	}
	name := oneLine(in.Name, 80)
	if name == "" {
		return errors.New("thiếu tên office")
	}
	keyEnc, err := s.box.Seal(in.Key)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	r := request{ID: ids.New("prq"), Nonce: in.Nonce, Name: name, URL: their, To: to, KeyEnc: keyEnc, Code: MatchCode(in.Nonce, to, their, in.Key),
		IP: oneLine(ip, 64), CreatedAt: now, Expires: now.Add(requestTTL)}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := slices.DeleteFunc(s.requests(ctx), func(x request) bool { return x.URL == their }) // asked again: the latest only
	if len(list) >= maxRequests {
		return ErrRequestsFull
	}
	return s.st.Settings().Set(ctx, requestsKey, append(list, r))
}

// requests are the open ones; the caller holds mu, or only reads.
func (s *Service) requests(ctx context.Context) []request {
	var list []request
	_, _ = s.st.Settings().Get(ctx, requestsKey, &list)
	now := s.now()
	return slices.DeleteFunc(list, func(r request) bool { return now.After(r.Expires) })
}

// Requests are the pairing requests waiting for an admin.
func (s *Service) Requests(ctx context.Context) []Request {
	out := []Request{}
	for _, r := range s.requests(ctx) {
		out = append(out, Request{ID: r.ID, Name: r.Name, URL: r.URL, Code: r.Code, IP: r.IP, CreatedAt: r.CreatedAt})
	}
	return out
}

// DropRequest turns a request down.
func (s *Service) DropRequest(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.requests(ctx)
	n := len(list)
	list = slices.DeleteFunc(list, func(r request) bool { return r.ID == id })
	if len(list) == n {
		return ErrNotFoundRequest
	}
	return s.st.Settings().Set(ctx, requestsKey, list)
}

// ErrNotFoundRequest: no such request (taken, turned down or expired).
var ErrNotFoundRequest = errors.New("không còn yêu cầu ghép nối này (đã xử lý hoặc hết hạn)")

// AcceptRequest pairs with the office that sent request id: this office is
// named name to it, shares shared, and what it sends runs as as. Safe to
// retry: the other office gets the same key again.
func (s *Service) AcceptRequest(ctx context.Context, as, id, name string, shared []string) (Peer, error) {
	if name = oneLine(name, 80); name == "" {
		return Peer{}, errors.New("thiếu tên office này")
	}
	shared, err := s.projects(ctx, shared)
	if err != nil {
		return Peer{}, err
	}
	s.mu.Lock()
	list := s.requests(ctx)
	i := slices.IndexFunc(list, func(r request) bool { return r.ID == id })
	if i < 0 {
		s.mu.Unlock()
		return Peer{}, ErrNotFoundRequest
	}
	r := &list[i]
	if r.AnswerEnc == "" { // kept before calling: a retry sends this one
		if r.AnswerEnc, err = s.box.Seal(NewKey()); err == nil {
			err = s.st.Settings().Set(ctx, requestsKey, list)
		}
	}
	req := *r
	s.mu.Unlock()
	if err != nil {
		return Peer{}, err
	}
	theirKey, err := s.box.Open(req.KeyEnc)
	if err != nil {
		return Peer{}, err
	}
	ourKey, err := s.box.Open(req.AnswerEnc)
	if err != nil {
		return Peer{}, err
	}
	p := Peer{ID: ids.New("peer"), Name: req.Name, URL: req.URL, SelfURL: req.To, OutKeyEnc: req.KeyEnc, InKeyHash: Hash(ourKey), Shared: shared, As: as, CreatedAt: s.now().UTC()}
	var out pairReply
	if err := s.do(ctx, http.MethodPost, req.URL+"/api/peer/request/answer", theirKey,
		answerBody{Nonce: req.Nonce, Key: ourKey, Name: name, Projects: s.SharedProjects(ctx, p)}, &out); err != nil {
		return Peer{}, err
	}
	p.Name = oneLine(cmp.Or(out.Name, req.Name), 80)
	p.Remote = cleanRemote(out.Projects)
	if err := s.update(ctx, func(list []Peer) ([]Peer, error) { return upsert(list, &p), nil }); err != nil {
		return Peer{}, err
	}
	_ = s.DropRequest(ctx, id)
	return p, nil
}

// Answer takes the answer to a request this office made, authenticated with
// the key it gave (key): it pairs. The same answer again is fine.
func (s *Service) Answer(ctx context.Context, key string, raw []byte) ([]byte, error) {
	var in answerBody
	if json.Unmarshal(raw, &in) != nil || in.Nonce == "" || !strings.HasPrefix(in.Key, "ofp_") || !strings.HasPrefix(key, "ofp_") {
		return nil, ErrDenied
	}
	h := Hash(key)
	s.mu.Lock()
	list := s.outgoing(ctx)
	i := slices.IndexFunc(list, func(o outgoing) bool {
		return subtle.ConstantTimeCompare([]byte(o.ID), []byte(in.Nonce)) == 1 && subtle.ConstantTimeCompare([]byte(o.KeyHash), []byte(h)) == 1
	})
	if i < 0 {
		s.mu.Unlock()
		// answered before (its OK got lost): the pairing it made stands
		if p, ok := s.Auth(ctx, key); ok {
			return json.Marshal(pairReply{Name: s.selfName(ctx, p), Projects: s.SharedProjects(ctx, p)})
		}
		return nil, ErrDenied
	}
	o := list[i]
	list = slices.Delete(list, i, i+1) // used once
	err := s.st.Settings().Set(ctx, outgoingKey, list)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	p := Peer{ID: ids.New("peer"), Name: oneLine(cmp.Or(in.Name, o.URL), 80), URL: o.URL, SelfURL: o.SelfURL, InKeyHash: o.KeyHash, Shared: o.Shared,
		Remote: cleanRemote(in.Projects), As: o.As, CreatedAt: s.now().UTC()}
	if p.OutKeyEnc, err = s.box.Seal(in.Key); err != nil {
		return nil, err
	}
	if err := s.update(ctx, func(list []Peer) ([]Peer, error) { return upsert(list, &p), nil }); err != nil {
		return nil, err
	}
	return json.Marshal(pairReply{Name: o.Name, Projects: s.SharedProjects(ctx, p)})
}

// selfName is this office's name as p was told it (its outgoing request is
// gone): the host's, when not kept.
func (s *Service) selfName(ctx context.Context, p Peer) string {
	return fmt.Sprintf("office %s", strings.TrimPrefix(strings.TrimPrefix(p.SelfURL, "https://"), "http://"))
}
