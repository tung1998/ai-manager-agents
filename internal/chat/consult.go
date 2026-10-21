package chat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/peer"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Asking an expert (ADR-177): an agent of a chat asks an agent a linked
// project lends (in this office or another) with ask_expert. The expert
// answers in a hidden chat of its own project (Purpose "consult"), read only,
// from what that project knows; the answer comes back into the asking chat
// as the expert's message and the agent that asked is called again.

const (
	consultPurpose = "consult"
	// consultWait: an unanswered question fails after this.
	consultWait = 30 * time.Minute
	// consultIdle: an expert's chat idle this long starts afresh (a new
	// session: a question is self-contained, an old thread only bloats it).
	consultIdle = 2 * time.Hour
	// questions one person's message may lead to, and waiting at once per chat
	maxConsultsPerMessage = 10
	maxConsultsWaiting    = 3
	// questions another office may ask here in an hour
	maxConsultsPerPeerHour = 60
)

// Expert is an agent a chat's agents may ask.
type Expert struct {
	Label     string // how ask_expert names it: project/agent, or office/project/agent
	Agent     string
	AgentID   string
	Role      string
	Project   string
	ProjectID string
	Peer      *peer.Peer // its office (nil = this one)
}

// Experts are the experts project projectID's agents may ask: the ones each
// project it links to lends (never the office assistant's).
func (e *Engine) Experts(ctx context.Context, projectID string) []Expert {
	var out []Expert
	for _, id := range perm.LoadPolicy(ctx, e.store, projectID).LinkedProjects {
		if _, _, isPeer := peer.SplitRef(id); isPeer || id == projectID || e.isAssistant(ctx, id) {
			continue
		}
		q, err := e.store.Repos().Get(ctx, id)
		if err != nil {
			continue
		}
		for _, a := range e.lent(ctx, q) {
			out = append(out, Expert{Label: q.Name + "/" + a.Name, Agent: a.Name, AgentID: a.ID, Role: a.Role, Project: q.Name, ProjectID: q.ID})
		}
	}
	if e.peers != nil {
		for _, l := range e.peers.LinksOf(ctx, projectID) {
			p := l.Peer
			for _, x := range l.Project.Experts {
				out = append(out, Expert{Label: p.Name + "/" + l.Project.Name + "/" + x.Name, Agent: x.Name, AgentID: x.ID, Role: x.Role,
					Project: l.Project.Name, ProjectID: l.Project.ID, Peer: &p})
			}
		}
	}
	return out
}

// lent are the agents project q lends as experts (Policy.Experts), on.
func (e *Engine) lent(ctx context.Context, q storage.Repo) []storage.Agent {
	var out []storage.Agent
	for _, id := range perm.LoadPolicy(ctx, e.store, q.ID).ExpertIDs(q.DefaultAgentID) {
		if a, err := e.store.Agents().Get(ctx, id); err == nil && a.ProjectID == q.ID && !a.Disabled {
			out = append(out, a)
		}
	}
	return out
}

// findExpert finds name among list: its label, or its agent's name when only
// one expert has it (any case).
func findExpert(list []Expert, name string) (Expert, error) {
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "@"))
	for _, x := range list {
		if strings.EqualFold(x.Label, name) {
			return x, nil
		}
	}
	var hits []Expert
	for _, x := range list {
		if strings.EqualFold(x.Agent, name) {
			hits = append(hits, x)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	labels := make([]string, 0, len(list))
	for _, x := range list {
		labels = append(labels, x.Label)
	}
	if len(hits) > 1 {
		return Expert{}, fmt.Errorf("có nhiều chuyên gia tên %q, ghi rõ project: %s", name, strings.Join(labels, ", "))
	}
	return Expert{}, fmt.Errorf("không có chuyên gia %q (có: %s)", name, strings.Join(labels, ", "))
}

// expertsBrief tells a chat's agent which experts it may ask.
func (e *Engine) expertsBrief(ctx context.Context, projectID string) string {
	list := e.Experts(ctx, projectID)
	if len(list) == 0 {
		return ""
	}
	return "\n" + prompts.Render("chat/experts", map[string]any{"Experts": list}) + "\n"
}

// consultInfo marks a message of the consult flow (its page context): the
// question in an expert's chat, or the expert's answer in the asking chat.
type consultInfo struct {
	ID      string `json:"consult"`
	Agent   string `json:"consult_agent,omitempty"`
	Project string `json:"consult_project,omitempty"`
	Office  string `json:"consult_office,omitempty"`
	Link    string `json:"consult_link,omitempty"` // the expert's chat, in this office
}

// Consult says which expert wrote an answer in a chat.
type Consult struct {
	Agent   string `json:"agent"`
	Project string `json:"project"`
	Office  string `json:"office,omitempty"`
	Link    string `json:"link,omitempty"`
}

// ConsultOf is the expert whose answer m is, or nil.
func ConsultOf(m storage.Message) *Consult {
	var info consultInfo
	if m.Role != "assistant" || m.Context == "" || json.Unmarshal([]byte(m.Context), &info) != nil || info.ID == "" {
		return nil
	}
	return &Consult{Agent: info.Agent, Project: info.Project, Office: info.Office, Link: info.Link}
}

var consultMu sync.Mutex // one question recorded at a time: the caps hold

// AskExpert records a question of the agent answering in sc (ask_expert) and
// sends it; the answer comes back to the chat on its own.
func (e *Engine) AskExpert(ctx context.Context, sc officetools.Scope, name, question string) (string, error) {
	question = strings.TrimSpace(question)
	if sc.ConversationID == "" || sc.TaskID != "" || sc.Office || sc.Consult {
		return "", errors.New("chỉ hỏi chuyên gia được trong chat của project")
	}
	if question == "" {
		return "", errors.New("hãy ghi câu hỏi, kèm ngữ cảnh đủ để bên kia trả lời")
	}
	if len([]rune(question)) > peer.MaxQuestion {
		return "", fmt.Errorf("câu hỏi quá dài (tối đa %d ký tự): tóm lại phần cần thiết", peer.MaxQuestion)
	}
	if _, inRun := e.WorkflowScope(sc); inRun {
		return "", errors.New("đang trong một quy trình: không hỏi chuyên gia được")
	}
	conv, err := e.store.Chat().GetConversation(ctx, sc.ConversationID)
	if err != nil || conv.Purpose != "" || conv.TaskID != "" {
		return "", errors.New("chỉ hỏi chuyên gia được trong chat của project (không phải bot, tự động hóa hay quy trình)")
	}
	e.mu.Lock()
	turn := e.turns[sc.RunRef]
	e.mu.Unlock()
	if turn == nil || !strings.HasPrefix(turn.actor, "human:") {
		return "", errors.New("chỉ hỏi chuyên gia được trong lượt do một người gửi")
	}
	root, ok := e.personMessage(ctx, conv.ID)
	if !ok {
		return "", errors.New("chỉ hỏi chuyên gia được từ tin một người viết ở đây (không phải tin chuyển từ chat khác)")
	}
	ex, err := findExpert(e.Experts(ctx, conv.ProjectID), name)
	if err != nil {
		return "", err
	}
	consultMu.Lock()
	if n, _ := e.store.Consults().CountRoot(ctx, conv.ID, root); n >= maxConsultsPerMessage {
		consultMu.Unlock()
		return "", fmt.Errorf("đã hỏi chuyên gia %d lần cho tin này của người dùng: hỏi người dùng trước khi hỏi tiếp", maxConsultsPerMessage)
	}
	if list, _ := e.store.Consults().Asked(ctx, conv.ID); len(list) >= maxConsultsWaiting {
		consultMu.Unlock()
		return "", fmt.Errorf("đang chờ %d câu trả lời của chuyên gia: đợi chúng về rồi hỏi tiếp", len(list))
	}
	rec := storage.Consult{Side: storage.ConsultLocal, ConversationID: conv.ID, ProjectID: conv.ProjectID, CallerAgentID: turn.agentID,
		Actor: turn.actor, Ceiling: turn.ceiling, RootMessageID: root, ExpertProject: ex.ProjectID, ExpertAgentID: ex.AgentID,
		ExpertName: ex.Agent, ExpertProjectName: ex.Project, Question: question, Deadline: time.Now().Add(consultWait).UTC()}
	if ex.Peer != nil {
		rec.Side, rec.PeerID = storage.ConsultOut, ex.Peer.ID
	} else {
		rec.FromLabel = e.fromLabel(ctx, conv.ProjectID, turn.agentName)
	}
	rec, err = e.store.Consults().Create(ctx, rec)
	consultMu.Unlock()
	if err != nil {
		return "", err
	}
	if ex.Peer != nil {
		from := peer.ConsultFrom{Chat: conv.ID, Agent: turn.agentName}
		if r, err := e.store.Repos().Get(ctx, conv.ProjectID); err == nil {
			from.Project = r.Name
		}
		if _, err := e.peers.Consult(ctx, ex.Peer.ID, peer.ConsultRequest{ID: rec.ID, Project: ex.ProjectID, Agent: ex.AgentID, Question: question, From: from}); err != nil {
			_, _ = e.store.Consults().Finish(ctx, rec.ID, storage.ConsultFailed, "", err.Error())
			return "", fmt.Errorf("không hỏi được %s: %w", ex.Label, err)
		}
	} else {
		safeGo("consult", func() { e.consultStart(context.Background(), rec) })
	}
	return prompts.Render("handoff/asked", map[string]any{"Expert": ex.Label}), nil
}

// personMessage is the last message in the chat a person wrote here (not
// relayed from another chat, not another office's): the root a question
// chain is counted from.
func (e *Engine) personMessage(ctx context.Context, conversationID string) (string, bool) {
	msgs, err := e.store.Chat().ListMessages(ctx, conversationID)
	if err != nil {
		return "", false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != "user" || strings.HasPrefix(m.Content, "[office] ") {
			continue
		}
		if !strings.HasPrefix(m.Author, "human:") || m.Sender != nil || strings.Contains(m.Context, `"`+relayKey+`"`) {
			return "", false
		}
		return m.ID, true
	}
	return "", false
}

// fromLabel is who asks, as the expert's chat shows it.
func (e *Engine) fromLabel(ctx context.Context, projectID, agent string) string {
	name := projectID
	if r, err := e.store.Repos().Get(ctx, projectID); err == nil {
		name = r.Name
	}
	return name + " · " + agent
}

// ReceiveConsult takes a question the paired office p asks an expert here.
func (e *Engine) ReceiveConsult(ctx context.Context, p peer.Peer, q peer.ConsultRequest) (peer.ConsultReply, error) {
	q.Question = strings.TrimSpace(q.Question)
	switch {
	case !e.activePerson(ctx, p.As):
		return peer.ConsultReply{}, errors.New("người đứng tên ghép nối không còn hoạt động ở office này")
	case q.ID == "" || q.Question == "" || q.From.Chat == "":
		return peer.ConsultReply{}, errors.New("thiếu câu hỏi")
	case len([]rune(q.Question)) > peer.MaxQuestion:
		return peer.ConsultReply{}, errors.New("câu hỏi quá dài")
	// only a shared project, never the office assistant (not even with Administrator)
	case q.Project == "" || e.isAssistant(ctx, q.Project) || !slices.Contains(p.Shared, q.Project):
		return peer.ConsultReply{}, errors.New("project này không mở cho office của bạn")
	}
	proj, err := e.store.Repos().Get(ctx, q.Project)
	if err != nil {
		return peer.ConsultReply{}, errors.New("project này không mở cho office của bạn")
	}
	i := slices.IndexFunc(e.lent(ctx, proj), func(a storage.Agent) bool { return a.ID == q.Agent })
	if i < 0 {
		return peer.ConsultReply{}, errors.New("agent này không nhận câu hỏi từ office khác")
	}
	expert := e.lent(ctx, proj)[i]
	reply := peer.ConsultReply{Agent: expert.Name, Project: proj.Name}
	consultMu.Lock()
	defer consultMu.Unlock()
	if _, err := e.store.Consults().GetIn(ctx, p.ID, q.ID); err == nil {
		return reply, nil // asked again (a retry): once
	}
	if n, _ := e.store.Consults().CountInSince(ctx, p.ID, time.Now().Add(-time.Hour)); n >= maxConsultsPerPeerHour {
		return peer.ConsultReply{}, fmt.Errorf("office của bạn đã hỏi %d câu trong một giờ: thử lại sau", maxConsultsPerPeerHour)
	}
	from := p.Name + "/" + cmp.Or(oneLine(q.From.Project), "?")
	if a := oneLine(q.From.Agent); a != "" {
		from += " · " + a
	}
	rec, err := e.store.Consults().Create(ctx, storage.Consult{Side: storage.ConsultIn, PeerID: p.ID, RemoteID: q.ID, ConversationID: q.From.Chat,
		Actor: p.As, ExpertProject: proj.ID, ExpertAgentID: expert.ID, ExpertName: expert.Name, ExpertProjectName: proj.Name,
		FromLabel: truncate(from, 200), Question: q.Question, Deadline: time.Now().Add(consultWait).UTC()})
	if err != nil {
		return peer.ConsultReply{}, err
	}
	safeGo("consult", func() { e.consultStart(context.Background(), rec) })
	return reply, nil
}

// consultStart puts a question to its expert: into the expert's chat for
// the asking chat (a new one when there is none, or it has been idle).
func (e *Engine) consultStart(ctx context.Context, rec storage.Consult) {
	fail := func(why string) { e.consultReplied(ctx, rec, "", why) }
	proj, err := e.store.Repos().Get(ctx, rec.ExpertProject)
	if err != nil || e.isAssistant(ctx, proj.ID) {
		fail("không còn project của chuyên gia")
		return
	}
	if !slices.ContainsFunc(e.lent(ctx, proj), func(a storage.Agent) bool { return a.ID == rec.ExpertAgentID }) {
		fail(rec.ExpertName + " không còn nhận câu hỏi")
		return
	}
	subject := "consult:" + cmp.Or(rec.PeerID, "local") + ":" + rec.ConversationID + ":" + rec.ExpertAgentID
	conv, err := e.store.Chat().SubjectConversation(ctx, proj.ID, consultPurpose, subject, "")
	if err != nil || conv.Cleaned != "" || time.Since(conv.UpdatedAt) > consultIdle {
		conv, err = e.store.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: proj.ID, AgentID: rec.ExpertAgentID, AgentName: rec.ExpertName,
			Title: truncate("Hỏi từ "+rec.FromLabel, 80), Mode: perm.Read, Purpose: consultPurpose, Subject: subject, CreatedBy: rec.Actor})
		if err != nil {
			fail(err.Error())
			return
		}
	}
	if err := e.store.Consults().SetConsultConversation(ctx, rec.ID, conv.ID); err != nil {
		fail(err.Error())
		return
	}
	rec.ConsultConvID = conv.ID
	raw, _ := json.Marshal(consultInfo{ID: rec.ID})
	// an @tag in the question must not bring the project's other agents in
	text := prompts.Render("handoff/consult", map[string]any{"From": rec.FromLabel, "Question": strings.ReplaceAll(rec.Question, "@", "＠")})
	as := WithCeiling(actor.With(context.Background(), rec.Actor), perm.Read)
	e.deliver(as, conv.ID, text, string(raw), func(err error) { fail(err.Error()) })
}

// consultTurnDone: the expert's chat answered (reply "" = it failed, why).
//
// The question it answers is the one in the chat's last message: nothing
// else comes in while it answers (a question waits its turn), and a message
// a person wrote there (opening the hidden chat) answers none.
func (e *Engine) consultTurnDone(conv storage.Conversation, reply, why string) {
	ctx := context.Background()
	msgs, err := e.store.Chat().ListMessages(ctx, conv.ID)
	if err != nil {
		return
	}
	var info consultInfo
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			_ = json.Unmarshal([]byte(msgs[i].Context), &info)
			break
		}
	}
	if info.ID == "" {
		return
	}
	rec, err := e.store.Consults().Get(ctx, info.ID)
	if err != nil || rec.Status != storage.ConsultPending || rec.ConsultConvID != conv.ID {
		return
	}
	if strings.TrimSpace(reply) == "" && why == "" {
		why = "chuyên gia không trả lời"
	}
	e.consultReplied(ctx, rec, reply, why)
}

// consultReplied: the expert answered rec (or could not, why): back to the
// chat that asked, here or in its office.
func (e *Engine) consultReplied(ctx context.Context, rec storage.Consult, answer, why string) {
	answer = truncate(strings.TrimSpace(answer), peer.MaxQuestion)
	if rec.Side == storage.ConsultLocal {
		e.consultDone(ctx, rec, answer, why)
		return
	}
	status := map[bool]string{true: storage.ConsultAnswered, false: storage.ConsultFailed}[why == ""]
	if ok, err := e.store.Consults().Finish(ctx, rec.ID, status, answer, why); err != nil || !ok {
		return
	}
	if e.peers == nil {
		return
	}
	// out of the turn's way; the other office may be away a moment
	safeGo("consult answer", func() {
		a := peer.ConsultAnswer{ID: rec.RemoteID, Answer: answer, Error: why}
		for i, wait := range []time.Duration{0, 5 * time.Second, 30 * time.Second, 2 * time.Minute} {
			time.Sleep(wait)
			err := e.peers.AnswerConsult(context.Background(), rec.PeerID, a)
			if err == nil {
				return
			}
			slog.Warn("chat: answer consult", "consult", rec.ID, "peer", rec.PeerID, "try", i+1, "err", err)
		}
	})
}

// ConsultAnswered takes the answer the paired office p gives to a question
// asked there: only one of this office's questions to it, once.
func (e *Engine) ConsultAnswered(ctx context.Context, p peer.Peer, a peer.ConsultAnswer) error {
	rec, err := e.store.Consults().Get(ctx, a.ID)
	if err != nil || rec.Side != storage.ConsultOut || rec.PeerID != p.ID {
		return errors.New("không có câu hỏi này")
	}
	why := truncate(strings.TrimSpace(a.Error), 500)
	if strings.TrimSpace(a.Answer) == "" && why == "" {
		why = "chuyên gia không trả lời"
	}
	e.consultDone(ctx, rec, truncate(strings.TrimSpace(a.Answer), peer.MaxQuestion), why)
	return nil
}

// consultDone ends a question in the chat that asked it, once: the answer
// goes in as the expert's message (or a line saying why there is none), and
// the agent that asked goes on from it.
func (e *Engine) consultDone(ctx context.Context, rec storage.Consult, answer, why string) {
	status := map[bool]string{true: storage.ConsultAnswered, false: storage.ConsultFailed}[why == ""]
	if ok, err := e.store.Consults().Finish(ctx, rec.ID, status, answer, why); err != nil || !ok {
		return
	}
	conv, err := e.store.Chat().GetConversation(ctx, rec.ConversationID)
	if err != nil || conv.Cleaned != "" {
		return
	}
	label := rec.ExpertName + " (" + rec.ExpertProjectName + ")"
	if why != "" {
		e.note(conv, fmt.Sprintf("Chuyên gia %s không trả lời được: %s", label, why))
	} else {
		info := consultInfo{ID: rec.ID, Agent: rec.ExpertName, Project: rec.ExpertProjectName}
		if rec.PeerID != "" && e.peers != nil {
			if p, err := e.peers.Get(ctx, rec.PeerID); err == nil {
				info.Office = p.Name
			}
		} else if rec.ConsultConvID != "" {
			info.Link = "/projects/" + rec.ExpertProject + "?tab=chat&c=" + rec.ConsultConvID // i18n-ignore
		}
		raw, _ := json.Marshal(info)
		if _, err := e.store.Chat().AddMessage(ctx, storage.Message{ConversationID: conv.ID, Role: "assistant", Content: answer,
			Author: label, Context: string(raw)}); err != nil { // never an agent of this chat's name: it is not one
			return
		}
	}
	e.resumeAsker(conv, rec, prompts.Render("handoff/expert-answer", map[string]any{"Expert": label, "Answer": answer, "Error": why}))
}

// resumeAsker calls the agent that asked again, as its turn would have run
// (the person, the ceiling), once the chat is free.
func (e *Engine) resumeAsker(conv storage.Conversation, rec storage.Consult, prompt string) {
	safeGo("consult resume", func() {
		ctx := context.Background()
		agent, err := e.store.Agents().Get(ctx, rec.CallerAgentID)
		if err != nil || agent.ProjectID != conv.ProjectID {
			return
		}
		project, err := e.store.Repos().Get(ctx, conv.ProjectID)
		if err != nil {
			return
		}
		for try := 0; try < 120; try++ {
			t, why := e.startTurn(conv, project, turnSpec{agent: agent, actor: rec.Actor, ceiling: rec.Ceiling, limit: -1, report: true,
				title: rec.ExpertName + " → " + agent.Name, prompt: prompt})
			if t != nil {
				return
			}
			if why != "" {
				e.note(conv, why)
				return
			}
			time.Sleep(e.reportWait())
		}
		e.note(conv, agent.Name+" chưa được gọi lại với câu trả lời của "+rec.ExpertName+" (chat bận quá lâu): nhắn lại để agent đọc câu trả lời ở trên.")
	})
}

// SweepConsults fails the questions nobody answered in time.
func (e *Engine) SweepConsults(ctx context.Context) {
	list, err := e.store.Consults().Expired(ctx, time.Now())
	if err != nil {
		return
	}
	for _, rec := range list {
		why := fmt.Sprintf("quá %d phút không có câu trả lời", int(consultWait.Minutes()))
		if rec.Side == storage.ConsultIn {
			e.consultReplied(ctx, rec, "", why)
		} else {
			e.consultDone(ctx, rec, "", why)
		}
	}
}
