package chat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Linked projects in another office (ADR-167): send_to_chat hands work to
// it over its /api/peer/relay, its answer comes back the same way, and a
// message from it goes into a chat here like a linked project's would.

// Peers reaches the offices this one is paired with.
type Peers interface {
	Get(ctx context.Context, id string) (peer.Peer, error)
	Send(ctx context.Context, id string, m peer.Message) (peer.Reply, error)
	// LinksOf: the projects in other offices a project links to
	LinksOf(ctx context.Context, projectID string) []peer.Link
	// Consult asks an expert there; AnswerConsult answers its question (ADR-177)
	Consult(ctx context.Context, id string, q peer.ConsultRequest) (peer.ConsultReply, error)
	AnswerConsult(ctx context.Context, id string, a peer.ConsultAnswer) error
}

// SetPeers turns on linked projects in other offices.
func (e *Engine) SetPeers(p Peers) { e.peers = p }

// relayPeer sends an approved send_message to the linked project's office.
func (e *Engine) relayPeer(ctx context.Context, a storage.Action) (string, string, error) {
	if e.peers == nil {
		return "", "", errors.New("không có office liên kết")
	}
	p, err := e.peers.Get(ctx, a.Args.Peer)
	if err != nil {
		return "", "", errors.New("office liên kết không còn (đã gỡ ghép nối?)")
	}
	from := peer.Origin{Chat: a.ConversationID, Agent: a.ProposedBy, ProjectID: a.ProjectID, Hops: 1}
	if r, err := e.store.Repos().Get(ctx, a.ProjectID); err == nil {
		from.Project = r.Name
	}
	if c, err := e.store.Chat().GetConversation(ctx, a.ConversationID); err == nil {
		from.Title = c.Title
	}
	if msgs, err := e.store.Chat().ListMessages(ctx, a.ConversationID); err == nil {
		if o, _, ok := relayOrigin(msgs); ok {
			from.Hops = o.Hops + 1
		}
	}
	m := peer.Message{Chat: a.TargetID, Project: a.Args.Project, Text: a.Args.Message, From: from}
	r, err := e.peers.Send(ctx, p.ID, m)
	if err != nil {
		return "", "", err
	}
	link := peer.ChatLink(p.URL, cmp.Or(r.Project, a.Args.Project), r.Chat)
	return fmt.Sprintf("đã gửi sang office %s (%q): %s", p.Name, cmp.Or(r.Title, "chat mới"), link), r.Chat, nil
}

// Receive takes a message the paired office p sent: a new chat in a project
// it is shared, or on in a chat already between the two offices. It goes in
// as the person who paired, with where it came from; the chat's agent
// answers as for a linked project's hand-off.
func (e *Engine) Receive(ctx context.Context, p peer.Peer, m peer.Message) (peer.Reply, error) {
	text := strings.TrimSpace(m.Text)
	switch {
	case !e.activePerson(ctx, p.As):
		return peer.Reply{}, errors.New("người đứng tên ghép nối không còn hoạt động ở office này")
	case text == "" || m.From.Chat == "":
		return peer.Reply{}, errors.New("thiếu nội dung hoặc chat gửi")
	case m.From.Hops < 0 || m.From.Hops > maxRelayHops:
		return peer.Reply{}, fmt.Errorf("đã quá %d lượt qua lại, cần người xem", maxRelayHops)
	}
	var conv storage.Conversation
	if m.Chat == "" { // a new chat: only in a project shared with it
		if m.From.Report || !p.Opens(m.Project, e.assistantID(ctx)) {
			return peer.Reply{}, errors.New("project này không mở cho office của bạn")
		}
		c, err := e.StartConversationFor(actor.With(ctx, p.As), m.Project, "")
		if err != nil {
			return peer.Reply{}, err
		}
		if conv, err = e.store.Chat().CreateConversation(ctx, c); err != nil {
			return peer.Reply{}, err
		}
	} else {
		c, err := e.store.Chat().GetConversation(ctx, m.Chat)
		_, sent := e.peerChat(ctx, m.Chat, p.ID)
		// one it may read, or one that handed it work (its answer comes back)
		// with its project (ADR-169): a chat of that project only
		if err != nil || c.Cleaned != "" || !(sent || e.PeerChat(ctx, c, p)) || (m.Project != "" && c.ProjectID != m.Project) {
			return peer.Reply{}, errors.New("không có chat này giữa hai office")
		}
		conv = c
	}
	name := p.Name + "/" + m.From.Project
	info := relayInfo{ConversationID: m.From.Chat, Title: m.From.Title, Agent: m.From.Agent, Project: name, Name: name,
		ProjectID: m.From.ProjectID, Hops: m.From.Hops, Report: m.From.Report, Peer: p.ID, PeerURL: p.URL}
	raw, _ := json.Marshal(info)
	// what it starts knows it came from p: the office assistant runs it as
	// administrator only while p has Admin here, never higher than manage else
	as := withPeerFrom(actor.With(context.Background(), p.As), p.ID)
	e.deliver(as, conv.ID, text, string(raw), func(err error) { e.note(conv, "Không nhận được tin từ office "+p.Name+": "+err.Error()) })
	return peer.Reply{Chat: conv.ID, Title: conv.Title, Project: conv.ProjectID}, nil
}

// PeerChat: p may read c (read_link, search_history): a chat it opened or
// sent to here, in a project still shared with it. A chat of this office
// that handed it work is its person's own: p only answers into it.
func (e *Engine) PeerChat(ctx context.Context, c storage.Conversation, p peer.Peer) bool {
	from, _ := e.peerChat(ctx, c.ID, p.ID)
	return from && p.Opens(c.ProjectID, e.assistantID(ctx))
}

type peerFromKey struct{}

// withPeerFrom: the turn answers a message of the paired office peerID.
func withPeerFrom(ctx context.Context, peerID string) context.Context {
	return context.WithValue(ctx, peerFromKey{}, peerID)
}

func peerFromOf(ctx context.Context) string { v, _ := ctx.Value(peerFromKey{}).(string); return v }

// peerAdmin: the turn's paired office still has Admin here, and the person
// who paired is still an admin of this office (as for ADR-074).
func (e *Engine) peerAdmin(ctx context.Context) bool {
	id := peerFromOf(ctx)
	if id == "" || e.peers == nil {
		return false
	}
	p, err := e.peers.Get(ctx, id)
	return err == nil && p.Admin && e.isAdmin(ctx, p.As)
}

// activePerson: as (human:<email>) is an account here, not disabled.
func (e *Engine) activePerson(ctx context.Context, as string) bool {
	email, ok := strings.CutPrefix(as, "human:")
	if !ok {
		return false
	}
	u, err := e.store.Users().GetByEmail(ctx, email)
	return err == nil && !u.Disabled
}

// peerChat: the chat got a message from office peerID (from), or sent it one (sent).
func (e *Engine) peerChat(ctx context.Context, conversationID, peerID string) (from, sent bool) {
	if msgs, err := e.store.Chat().ListMessages(ctx, conversationID); err == nil {
		for _, m := range msgs {
			var info relayInfo
			if m.Role == "user" && json.Unmarshal([]byte(m.Context), &info) == nil && info.Peer == peerID {
				from = true
				break
			}
		}
	}
	if acts, err := e.store.Actions().List(ctx, conversationID, "", ""); err == nil {
		sent = slices.ContainsFunc(acts, func(a storage.Action) bool {
			return a.Kind == "send_message" && a.Status == "done" && a.Args.Peer == peerID
		})
	}
	return from, sent
}

// reportPeer: the answer to another office's hand-off goes back to its chat.
func (e *Engine) reportPeer(conv storage.Conversation, project storage.Repo, agent storage.Agent, reply string, origin relayInfo) {
	if e.peers == nil {
		return
	}
	ctx := context.Background()
	p, err := e.peers.Get(ctx, origin.Peer)
	if err != nil {
		e.note(conv, "Không báo lại được: office gửi việc đã gỡ ghép nối.")
		return
	}
	if origin.Hops >= maxRelayHops {
		e.note(conv, fmt.Sprintf("Không tự báo lại office %s nữa: đã quá %d lượt qua lại. Hãy xem và trả lời trực tiếp.", p.Name, maxRelayHops))
		return
	}
	text := prompts.Render("chat/linked-report", map[string]any{"Project": project.Name, "Agent": agent.Name,
		"Link": peer.ChatLink(p.SelfURL, conv.ProjectID, conv.ID), "Reply": truncate(reply, 8000)})
	m := peer.Message{Chat: origin.ConversationID, Text: text, From: peer.Origin{Chat: conv.ID, Title: conv.Title, Agent: agent.Name,
		Project: project.Name, ProjectID: project.ID, Hops: origin.Hops + 1, Report: true}}
	safeGo("report back to office", func() {
		if _, err := e.peers.Send(ctx, p.ID, m); err != nil {
			e.note(conv, "Không báo lại được office "+p.Name+": "+err.Error())
		}
	})
}

// peerChats are the chats in other offices this chat handed work to or
// heard back from (for the prompt: work goes on there).
func (e *Engine) peerChats(ctx context.Context, conversationID string) (out []linkedOpen) {
	if e.peers == nil {
		return nil
	}
	seen := map[string]bool{}
	add := func(o linkedOpen, id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, o)
		}
	}
	if acts, err := e.store.Actions().List(ctx, conversationID, "", ""); err == nil {
		for _, a := range acts {
			if a.Kind != "send_message" || a.Status != "done" || a.Args.Peer == "" || a.TargetID == "" {
				continue
			}
			if p, err := e.peers.Get(ctx, a.Args.Peer); err == nil {
				add(linkedOpen{Project: p.Name, Title: a.Target, Link: peer.ChatLink(p.URL, a.Args.Project, a.TargetID)}, a.Args.Peer+a.TargetID)
			}
		}
	}
	if msgs, err := e.store.Chat().ListMessages(ctx, conversationID); err == nil {
		for _, m := range msgs {
			var info relayInfo
			if m.Role == "user" && json.Unmarshal([]byte(m.Context), &info) == nil && info.Peer != "" {
				add(linkedOpen{Project: info.Name, Title: cmp.Or(info.Title, "(chưa có tiêu đề)"), // i18n-ignore
					Link: peer.ChatLink(info.PeerURL, info.ProjectID, info.ConversationID)}, info.Peer+info.ConversationID)
			}
		}
	}
	return out
}
