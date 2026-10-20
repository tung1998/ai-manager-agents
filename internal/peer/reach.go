package peer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// What the MCP other_office reaches (ADR-169): a project's agent, the
// projects of other offices its settings link to; the office assistant,
// every project every paired office opens to this one.

// Reach lists the projects in other offices a run may use: all of them for
// the office assistant, otherwise those projectID links to.
func Reach(ctx context.Context, st storage.Store, s *Service, projectID string, all bool) []Link {
	if s == nil {
		return nil
	}
	if !all {
		return Linked(ctx, st, s, projectID)
	}
	list, _ := s.List(ctx)
	var out []Link
	for _, p := range list {
		for _, r := range p.Remote {
			out = append(out, Link{Ref: RefPrefix + p.ID + "/" + r.ID, Peer: p, Project: r})
		}
	}
	return out
}

// Offices are the paired offices among links, in order.
func Offices(links []Link) []Peer {
	var out []Peer
	for _, l := range links {
		if !slices.ContainsFunc(out, func(p Peer) bool { return p.ID == l.Peer.ID }) {
			out = append(out, l.Peer)
		}
	}
	return out
}

// FindOffice finds an office among links by its name or id.
func FindOffice(links []Link, office string) (Peer, error) {
	office = strings.TrimSpace(office)
	for _, p := range Offices(links) {
		if p.ID == office || strings.EqualFold(p.Name, office) {
			return p, nil
		}
	}
	return Peer{}, fmt.Errorf("không có office %q trong các office bạn được dùng (xem offices)", office)
}

// Find finds a project of an office among links, each by its name or id.
func Find(links []Link, office, project string) (Link, error) {
	p, err := FindOffice(links, office)
	if err != nil {
		return Link{}, err
	}
	project = strings.TrimSpace(project)
	for _, l := range links {
		if l.Peer.ID == p.ID && (l.Project.ID == project || strings.EqualFold(l.Project.Name, project)) {
			return l, nil
		}
	}
	return Link{}, fmt.Errorf("office %s không có project %q bạn được dùng (xem offices)", p.Name, project)
}

// PriorSend is the send by which conversationID already handed work to chat
// in the office peerID (its answers come back there, so it may go on reading
// and sending there); peerID or chat "" = any.
func PriorSend(ctx context.Context, st storage.Store, conversationID, peerID, chat string) (storage.Action, bool) {
	if conversationID == "" {
		return storage.Action{}, false
	}
	acts, _ := st.Actions().List(ctx, conversationID, "", "")
	for _, a := range acts {
		if a.Kind == "send_message" && a.Status == "done" && a.Args.Peer != "" && a.TargetID != "" &&
			(peerID == "" || a.Args.Peer == peerID) && (chat == "" || a.TargetID == chat) {
			return a, true
		}
	}
	return storage.Action{}, false
}

// ParseChatLink reads a dashboard link of one of offices: the office, its
// project id there, the chat and the message (may be "").
func ParseChatLink(offices []Peer, link string) (Peer, string, string, string, error) {
	link = strings.TrimSpace(link)
	for _, o := range offices {
		rest, ok := strings.CutPrefix(link, o.URL+"/projects/")
		if !ok || o.URL == "" {
			continue
		}
		path, query, _ := strings.Cut(rest, "?")
		project, err := url.PathUnescape(path)
		if err != nil {
			project = path
		}
		q, _ := url.ParseQuery(query)
		if q.Get("c") == "" {
			return Peer{}, "", "", "", errors.New("ở office khác chỉ đọc được liên kết chat (c=…)")
		}
		return o, project, q.Get("c"), q.Get("m"), nil
	}
	return Peer{}, "", "", "", errors.New("liên kết không thuộc office nào đã ghép nối")
}

// IsOtherOffice: link points into one of the paired offices.
func (s *Service) IsOtherOffice(ctx context.Context, link string) bool {
	if s == nil {
		return false
	}
	list, _ := s.List(ctx)
	return slices.ContainsFunc(list, func(p Peer) bool { return p.URL != "" && strings.HasPrefix(strings.TrimSpace(link), p.URL+"/") })
}
