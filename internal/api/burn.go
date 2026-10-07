package api

import (
	"cmp"
	"errors"
	"net/http"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A project's Burn (spec 2026-10-01-burn-design): its settings, state and
// the pieces of work it found. Admins only: it runs with the machine.

type burnDTO struct {
	ID             string `json:"id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	AgentID        string `json:"agent_id"`
	ModelTier      string `json:"model_tier"`
	MaxSubagents   int    `json:"max_subagents"`
	ResultMode     string `json:"result_mode"`
	Focus          string `json:"focus"`
	Order          string `json:"order"`
	// review (ADR-113): the profile followed ("" = none), each stage's chat
	ReviewProfileID     string            `json:"review_profile_id"`
	ReviewConversations map[string]string `json:"review_conversations"`
	EndsAt              *time.Time        `json:"ends_at"`
	State               string            `json:"state"`
	WaitingUntil        *time.Time        `json:"waiting_until,omitempty"`
	StartedBy           string            `json:"started_by,omitempty"`
	StartedAt           *time.Time        `json:"started_at,omitempty"`
}

type burnItemDTO struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Kind       string    `json:"kind"`
	Detail     string    `json:"detail"`
	Status     string    `json:"status"`
	Priority   int       `json:"priority"`
	Branch     string    `json:"branch"`
	Worktree   string    `json:"worktree"`
	Summary    string    `json:"summary"`
	Subagents  int       `json:"subagents"`
	CostUSD    float64   `json:"cost_usd"`
	Reviewed   []string  `json:"reviewed"`
	ReviewNote string    `json:"review_note"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toBurnDTO(b storage.BurnSession) burnDTO {
	return burnDTO{b.ID, b.ConversationID, b.AgentID, b.ModelTier, b.MaxSubagents, b.ResultMode, b.Focus, cmp.Or(b.Order, "roadmap"),
		b.ReviewProfileID, mapOrEmpty(b.ReviewConversations), b.EndsAt, b.State, b.WaitingUntil, b.StartedBy, b.StartedAt}
}

// burnSession is the project's, or the defaults for a first one (not saved).
func (s *server) burnSession(r *http.Request, projectID string) (storage.BurnSession, error) {
	b, err := s.cfg.Store.Burn().Session(r.Context(), projectID)
	if errors.Is(err, storage.ErrNotFound) {
		b = storage.BurnSession{ProjectID: projectID, ModelTier: storage.TierBalanced, MaxSubagents: 2, ResultMode: "branch", Order: "roadmap", State: "stopped"}
		if a, err := s.cfg.Chat.DefaultAgent(r.Context(), projectID); err == nil && !a.Disabled {
			b.AgentID = a.ID
		}
		return b, nil
	}
	return b, err
}

func (s *server) getBurn(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Burn == nil {
		writeError(w, http.StatusNotImplemented, "Burn chưa bật trên office này")
		return
	}
	pid := r.PathValue("id")
	b, err := s.burnSession(r, pid)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	items := []burnItemDTO{}
	if b.ID != "" {
		list, _ := s.cfg.Store.Burn().Items(r.Context(), b.ID)
		for _, it := range list {
			items = append(items, burnItemDTO{it.ID, it.Title, it.Kind, it.Detail, it.Status, it.Priority, it.Branch, it.Worktree, it.Summary, it.Subagents, it.CostUSD,
				listOrEmpty(it.Reviewed), it.ReviewNote, it.UpdatedAt})
		}
	}
	out := map[string]any{"burn": toBurnDTO(b), "items": items}
	if reset, ok := s.cfg.Burn.WeeklyReset(r.Context(), b.AgentID); ok { // the suggested stop time
		out["weekly_reset"] = reset
	}
	writeJSON(w, http.StatusOK, out)
}

type burnInput struct {
	AgentID         *string    `json:"agent_id"`
	ModelTier       *string    `json:"model_tier"`
	MaxSubagents    *int       `json:"max_subagents"`
	ResultMode      *string    `json:"result_mode"`
	Focus           *string    `json:"focus"`
	Order           *string    `json:"order"`
	ReviewProfileID *string    `json:"review_profile_id"` // "" = no review
	EndsAt          *time.Time `json:"ends_at"`
	NoEnd           bool       `json:"no_end"` // run until stopped by hand
}

func (s *server) applyBurn(r *http.Request, in burnInput, b *storage.BurnSession) error {
	if in.AgentID != nil && *in.AgentID != "" {
		b.AgentID = *in.AgentID
	}
	if in.ModelTier != nil {
		switch *in.ModelTier {
		case storage.TierStrong, storage.TierBalanced, storage.TierFast:
			b.ModelTier = *in.ModelTier
		}
	}
	if in.MaxSubagents != nil {
		b.MaxSubagents = min(max(*in.MaxSubagents, 0), 5)
	}
	if in.ResultMode != nil && (*in.ResultMode == "branch" || *in.ResultMode == "patch") {
		b.ResultMode = *in.ResultMode
	}
	if in.Focus != nil {
		b.Focus = strings.TrimSpace(*in.Focus)
	}
	if in.Order != nil && (*in.Order == "roadmap" || *in.Order == "bugs" || *in.Order == "auto") {
		b.Order = *in.Order
	}
	if in.NoEnd {
		b.EndsAt = nil
	} else if in.EndsAt != nil {
		t := in.EndsAt.UTC()
		b.EndsAt = &t
	}
	if in.ReviewProfileID != nil {
		id := strings.TrimSpace(*in.ReviewProfileID)
		if id != "" {
			if p, err := s.cfg.Store.Burn().ReviewProfile(r.Context(), id); err != nil || p.ProjectID != b.ProjectID {
				return errors.New("không có hồ sơ review này")
			}
		}
		b.ReviewProfileID = id
	}
	if s.cfg.Burn != nil {
		if err := s.cfg.Burn.CheckAgent(r.Context(), b.AgentID); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) saveBurn(w http.ResponseWriter, r *http.Request) {
	var in burnInput
	if !decode(w, r, &in) {
		return
	}
	b, err := s.burnSession(r, r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	before := toBurnDTO(b)
	if err := s.applyBurn(r, in, &b); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if b, err = s.cfg.Store.Burn().SaveSession(r.Context(), b); err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "burn.update", Resource: "burn", ResourceID: b.ID, ProjectID: b.ProjectID, Before: before, After: toBurnDTO(b)})
	writeJSON(w, http.StatusOK, map[string]any{"burn": toBurnDTO(b)})
}

// startBurn: confirmed on the dashboard (the dialog says what it means).
func (s *server) startBurn(w http.ResponseWriter, r *http.Request) {
	var in burnInput
	if !decode(w, r, &in) {
		return
	}
	pid := r.PathValue("id")
	b, err := s.burnSession(r, pid)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.applyBurn(r, in, &b); err != nil { // before saving: nothing changes
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if b.EndsAt == nil && !in.NoEnd { // the suggested stop: the next weekly reset, else 8 hours
		t, ok := s.cfg.Burn.WeeklyReset(r.Context(), b.AgentID)
		if !ok {
			t = time.Now().Add(8 * time.Hour)
		}
		t = t.UTC()
		b.EndsAt = &t
	}
	if _, err := s.cfg.Store.Burn().SaveSession(r.Context(), b); err != nil {
		s.internal(w, r, err)
		return
	}
	b, err = s.cfg.Burn.Begin(r.Context(), pid, userFrom(r).Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.Change{Action: "burn.start", Resource: "burn", ResourceID: b.ID, ProjectID: pid, After: toBurnDTO(b)})
	writeJSON(w, http.StatusOK, map[string]any{"burn": toBurnDTO(b)})
}

func (s *server) stopBurn(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if err := s.cfg.Burn.Stop(r.Context(), pid); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "burn.stop", Resource: "burn", ProjectID: pid})
	w.WriteHeader(http.StatusNoContent)
}

// burnItemAction: skip, first (do it next), or drop its worktree.
func (s *server) burnItemAction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it, err := s.cfg.Store.Burn().Item(ctx, r.PathValue("item"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	switch r.PathValue("action") {
	case "skip":
		if it.Status == "doing" {
			writeError(w, http.StatusConflict, "Việc đang làm: tắt Burn trước")
			return
		}
		it.Status = "skipped"
	case "first":
		if it.Status == "found" || it.Status == "skipped" || it.Status == "failed" {
			it.Status = "queued"
		}
		it.Priority = int(time.Now().Unix())
	case "drop-worktree":
		if it.Status == "doing" {
			writeError(w, http.StatusConflict, "Việc đang làm: tắt Burn trước")
			return
		}
		if err := s.cfg.Burn.DropWorktree(ctx, it); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		it.Worktree = ""
	default:
		writeError(w, http.StatusNotFound, "không có thao tác này")
		return
	}
	if err := s.cfg.Store.Burn().UpdateItem(ctx, it); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listOrEmpty: [] rather than null in JSON.
func listOrEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// mapOrEmpty: {} rather than null in JSON.
func mapOrEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
