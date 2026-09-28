package api

import (
	"bitbucket.org/senprints/agent-office/internal/assistant"
	"bitbucket.org/senprints/agent-office/internal/attach"
	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/automation"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

type conversationDTO struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	AgentID      string    `json:"agent_id"`
	AgentName    string    `json:"agent_name"`
	Title        string    `json:"title"`
	CreatedBy    string    `json:"created_by"`
	UpdatedAt    time.Time `json:"updated_at"`
	ActiveTurn   string    `json:"active_turn,omitempty"`
	Mode         string    `json:"mode"`
	EditMode     string    `json:"edit_mode"`
	Purpose      string    `json:"purpose"`
	AutomationID string    `json:"automation_id"`
	// how full the model's context was after the last answer (0 = unknown)
	ContextTokens int `json:"context_tokens"`
	ContextWindow int `json:"context_window"`
}

func (s *server) toConvDTO(c storage.Conversation) conversationDTO {
	d := conversationDTO{ID: c.ID, ProjectID: c.ProjectID, AgentID: c.AgentID, AgentName: c.AgentName, Title: c.Title, CreatedBy: c.CreatedBy, UpdatedAt: c.UpdatedAt, Mode: c.Mode, EditMode: c.EditMode, Purpose: c.Purpose, AutomationID: c.AutomationID,
		ContextTokens: c.ContextTokens, ContextWindow: c.ContextWindow}
	if t, ok := s.cfg.Chat.Active(c.ID); ok {
		d.ActiveTurn = t.ID
	}
	return d
}

func (s *server) chatError(w http.ResponseWriter, r *http.Request, err error) {
	var be *usage.BudgetError
	switch {
	case errors.As(err, &be):
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": be.Error(), "code": "budget"})
	case errors.Is(err, chat.ErrBusy), errors.Is(err, chat.ErrAgentBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, chat.ErrNoModel), errors.Is(err, chat.ErrNoAgent), errors.Is(err, chat.ErrNoFolder), errors.Is(err, chat.ErrDecided), errors.Is(err, automation.ErrUnknownSkill),
		errors.Is(err, attach.ErrNotFound), errors.Is(err, attach.ErrTooMany):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.writeDomainError(w, r, err)
	}
}

func (s *server) chatSkills(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Chat.Skills(r.Context(), r.PathValue("id"))
	if err != nil {
		s.chatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": list})
}

func (s *server) chatAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.cfg.Chat.Agents(r.Context(), r.PathValue("id"))
	if err != nil {
		s.chatError(w, r, err)
		return
	}
	out := make([]agentDTO, 0, len(agents))
	for _, a := range agents {
		out = append(out, toAgentDTO(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

func (s *server) listConversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Chat().ListConversations(r.Context(), r.PathValue("id"), 100)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]conversationDTO, 0, len(list))
	private := r.PathValue("id") == assistant.ID(r.Context(), s.cfg.Store) // assistant chats are each person's own
	me := "human:" + userFrom(r).Email
	for _, c := range list {
		if private && c.CreatedBy != me {
			continue
		}
		out = append(out, s.toConvDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

func (s *server) createConversation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentID string `json:"agent_id"`
		Purpose string `json:"purpose"` // "automation": a chat that builds one automation (ADR-042)
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	var (
		c   storage.Conversation
		err error
	)
	if in.Purpose == "automation" {
		c, err = s.cfg.Chat.StartConversationPurpose(r.Context(), r.PathValue("id"), in.AgentID, in.Purpose)
	} else {
		c, err = s.cfg.Chat.StartConversation(r.Context(), r.PathValue("id"), in.AgentID)
	}
	if err != nil {
		s.chatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"conversation": s.toConvDTO(c)})
}

// taskConversation opens (or returns) the follow-up talk about a task.
func (s *server) taskConversation(w http.ResponseWriter, r *http.Request) {
	c, err := s.cfg.Chat.TaskConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.chatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": s.toConvDTO(c)})
}

func (s *server) getConversation(w http.ResponseWriter, r *http.Request) {
	c, err := s.cfg.Store.Chat().GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	msgs, err := s.cfg.Chat.History(r.Context(), c.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": s.toConvDTO(c), "messages": msgs, "members": s.chatMembers(r, c.ID),
		"running": s.cfg.Chat.Running(c.ID)})
}

// chatMembers: the agents in a chat (ADR-044), with their rights and context.
func (s *server) chatMembers(r *http.Request, conversationID string) []map[string]any {
	list, _ := s.cfg.Store.Chat().Members(r.Context(), conversationID)
	out := make([]map[string]any, 0, len(list))
	for _, m := range list {
		level := ""
		if a, err := s.cfg.Store.Agents().Get(r.Context(), m.AgentID); err == nil {
			level = perm.Agent(a)
		}
		out = append(out, map[string]any{"agent_id": m.AgentID, "agent_name": m.AgentName, "level": level,
			"context_tokens": m.ContextTokens, "context_window": m.ContextWindow})
	}
	return out
}

func (s *server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Chat.DeleteConversation(r.Context(), r.PathValue("id")); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text        string   `json:"text"`
		Attachments []string `json:"attachments"`
		Mode        string   `json:"mode"`      // permission mode for this chat from now on
		EditMode    string   `json:"edit_mode"` // where it changes code from now on
		Context     string   `json:"context"`   // the page the person is on (ADR-042)
		AgentID     string   `json:"agent_id"`  // who answers from now on
	}
	if !decode(w, r, &in) {
		return
	}
	if in.AgentID != "" {
		if err := s.cfg.Chat.SetAgent(r.Context(), r.PathValue("id"), in.AgentID); err != nil {
			s.chatError(w, r, err)
			return
		}
	}
	if in.Mode != "" {
		if err := s.cfg.Chat.SetMode(r.Context(), r.PathValue("id"), s.allowedMode(r, in.Mode)); err != nil {
			s.chatError(w, r, err)
			return
		}
	}
	if in.EditMode != "" {
		if err := s.cfg.Chat.SetEditMode(r.Context(), r.PathValue("id"), s.allowedEditMode(r, in.EditMode)); err != nil {
			s.chatError(w, r, err)
			return
		}
	}
	turn, msg, err := s.cfg.Chat.SendWithContext(r.Context(), r.PathValue("id"), in.Text, in.Context, in.Attachments)
	if err != nil {
		s.chatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"turn_id": turn.ID,
		"message": chat.MessageDTO{ID: msg.ID, Role: msg.Role, Content: msg.Content, Attachments: msg.Attachments, Author: msg.Author, CreatedAt: msg.CreatedAt,
			Tools: []storage.ToolCall{}, Patches: []chat.PatchDTO{}},
	})
}

// streamTurn sends a turn's events as Server-Sent Events, replaying from
// ?from (or Last-Event-ID) so a reconnect loses nothing.
func (s *server) streamTurn(w http.ResponseWriter, r *http.Request) {
	turn, ok := s.cfg.Chat.Turn(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Lượt trả lời đã kết thúc hoặc không tồn tại")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	seq, _ := strconv.Atoi(r.URL.Query().Get("from"))
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if n, err := strconv.Atoi(last); err == nil {
			seq = n + 1
		}
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, done, wake := turn.Since(seq)
		for _, e := range events {
			raw, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, raw)
			seq = e.Seq + 1
		}
		flusher.Flush()
		if done {
			return
		}
		select {
		case <-wake:
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) cancelTurn(w http.ResponseWriter, r *http.Request) {
	turn, ok := s.cfg.Chat.Turn(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Không tìm thấy lượt trả lời")
		return
	}
	turn.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) decidePatch(w http.ResponseWriter, r *http.Request, approve bool) {
	projectID := ""
	if cur, err := s.cfg.Store.Chat().GetPatch(r.Context(), r.PathValue("id")); err == nil {
		r, projectID = s.patchWho(r, cur, approve)
	}
	p, err := s.cfg.Chat.DecidePatch(r.Context(), r.PathValue("id"), approve)
	if err != nil && !errors.Is(err, chat.ErrDecided) {
		s.chatError(w, r, err)
		return
	}
	if errors.Is(err, chat.ErrDecided) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "patch": p})
		return
	}
	action := "patch.reject"
	if approve {
		action = "patch." + p.Status
	}
	var failed error
	if p.Status == "failed" {
		failed = errors.New(p.Detail)
	}
	s.audit(r, audit.Change{Action: action, ResourceID: p.ID, ProjectID: projectID, Detail: map[string]any{"files": p.Files, "detail": p.Detail}, Err: failed})
	writeJSON(w, http.StatusOK, map[string]any{"patch": p})
}

func (s *server) approvePatch(w http.ResponseWriter, r *http.Request) { s.decidePatch(w, r, true) }
func (s *server) rejectPatch(w http.ResponseWriter, r *http.Request)  { s.decidePatch(w, r, false) }

// patchWho: approving a patch records the agent that wrote it and the person
// who approved it; rejecting is the person's decision (ADR-043).
func (s *server) patchWho(r *http.Request, p storage.Patch, approve bool) (*http.Request, string) {
	u := userFrom(r)
	who := audit.Who{Kind: "human", ID: u.ID, Name: u.Email, Via: "ui", ConversationID: p.ConversationID, TaskID: p.TaskID}
	projectID := ""
	agent := ""
	if p.ConversationID != "" {
		if c, err := s.cfg.Store.Chat().GetConversation(r.Context(), p.ConversationID); err == nil {
			projectID, agent = c.ProjectID, c.AgentName
		}
		// in a group chat the diff is the agent's who wrote the answer (ADR-044)
		if msgs, err := s.cfg.Store.Chat().ListMessages(r.Context(), p.ConversationID); err == nil {
			for _, m := range msgs {
				if m.ID == p.MessageID && m.Author != "" {
					agent = m.Author
				}
			}
		}
	}
	if projectID == "" && p.TaskID != "" {
		if t, err := s.cfg.Store.Tasks().Get(r.Context(), p.TaskID); err == nil {
			projectID = t.ProjectID
		}
	}
	if approve && agent != "" {
		via := "chat"
		if p.TaskID != "" {
			via = "task"
		}
		who = audit.Who{Kind: "agent", Name: agent, ApprovedBy: u.Email, Via: via, ConversationID: p.ConversationID, TaskID: p.TaskID}
	}
	return r.WithContext(audit.With(r.Context(), who)), projectID
}

// stopConversation: Stop in a chat stops its answer and every hand-off
// working in the background.
func (s *server) stopConversation(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"stopped": s.cfg.Chat.StopAll(r.PathValue("id"))})
}

// assistantInfo: the office assistant's project, where its chats live (ADR-046).
func (s *server) assistantInfo(w http.ResponseWriter, r *http.Request) {
	id := assistant.ID(r.Context(), s.cfg.Store)
	if id == "" {
		writeError(w, http.StatusNotFound, "office chưa có trợ lý")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project_id": id})
}
