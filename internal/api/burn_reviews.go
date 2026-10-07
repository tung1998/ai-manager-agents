package api

import (
	"errors"
	"net/http"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/burn"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// A project's Burn review profiles (ADR-113): saved review setups, each
// stage with its own reviewer; a Burn follows one. Admins only.

type burnProfileDTO struct {
	ID     string                             `json:"id"`
	Name   string                             `json:"name"`
	Stages map[string]storage.BurnReviewStage `json:"stages"`
	InUse  bool                               `json:"in_use"` // the project's Burn follows it
}

func (s *server) toBurnProfileDTO(r *http.Request, p storage.BurnReviewProfile) burnProfileDTO {
	d := burnProfileDTO{ID: p.ID, Name: p.Name, Stages: p.Stages}
	if d.Stages == nil {
		d.Stages = map[string]storage.BurnReviewStage{}
	}
	if b, err := s.cfg.Store.Burn().Session(r.Context(), p.ProjectID); err == nil && b.ReviewProfileID == p.ID {
		d.InUse = true
	}
	return d
}

func (s *server) listBurnProfiles(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.Burn().ReviewProfiles(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]burnProfileDTO, 0, len(list))
	for _, p := range list {
		out = append(out, s.toBurnProfileDTO(r, p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

type burnProfileInput struct {
	Name   string                             `json:"name"`
	Stages map[string]storage.BurnReviewStage `json:"stages"`
}

// applyBurnProfile checks what a profile is given: a name, known stages, each
// reviewer an agent that is on and a workflow a chat may run.
func (s *server) applyBurnProfile(r *http.Request, in burnProfileInput, p *storage.BurnReviewProfile) error {
	p.Name = strings.TrimSpace(in.Name)
	if p.Name == "" {
		return errors.New("hãy đặt tên hồ sơ")
	}
	p.Stages = map[string]storage.BurnReviewStage{}
	for _, stage := range burn.ReviewStages {
		st, ok := in.Stages[stage]
		if !ok {
			continue
		}
		st.AgentID = strings.TrimSpace(st.AgentID)
		st.Workflow = strings.TrimPrefix(strings.TrimSpace(st.Workflow), "#")
		if st.AgentID != "" {
			if a, err := s.cfg.Store.Agents().Get(r.Context(), st.AgentID); err != nil || a.ProjectID != p.ProjectID {
				return errors.New("agent review không thuộc project này")
			}
			if s.cfg.Burn != nil {
				if err := s.cfg.Burn.CheckAgent(r.Context(), st.AgentID); err != nil {
					return err
				}
			}
		}
		if st.Workflow != "" { // a workflow of the project a chat may run (ADR-109)
			wf, err := s.cfg.Store.Workflows().GetByKey(r.Context(), p.ProjectID, st.Workflow)
			if err != nil {
				return errors.New("project chưa cài quy trình #" + st.Workflow)
			}
			if def, err := workflow.Parse(wf.Source); err == nil && def.Callable == workflow.CallableSub {
				return errors.New("quy trình #" + st.Workflow + " chỉ để quy trình khác gọi (callable: sub)")
			}
		}
		p.Stages[stage] = st
	}
	return nil
}

func (s *server) createBurnProfile(w http.ResponseWriter, r *http.Request) {
	var in burnProfileInput
	if !decode(w, r, &in) {
		return
	}
	p := storage.BurnReviewProfile{ProjectID: r.PathValue("id")}
	if err := s.applyBurnProfile(r, in, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.cfg.Store.Burn().SaveReviewProfile(r.Context(), p)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "burn.review_profile.create", Resource: "burn_review_profile", ResourceID: p.ID, ProjectID: p.ProjectID, After: p})
	writeJSON(w, http.StatusCreated, map[string]any{"profile": s.toBurnProfileDTO(r, p)})
}

func (s *server) updateBurnProfile(w http.ResponseWriter, r *http.Request) {
	var in burnProfileInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.cfg.Store.Burn().ReviewProfile(r.Context(), r.PathValue("profile"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	before := p
	if err := s.applyBurnProfile(r, in, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if p, err = s.cfg.Store.Burn().SaveReviewProfile(r.Context(), p); err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "burn.review_profile.update", Resource: "burn_review_profile", ResourceID: p.ID, ProjectID: p.ProjectID, Before: before, After: p})
	writeJSON(w, http.StatusOK, map[string]any{"profile": s.toBurnProfileDTO(r, p)})
}

// deleteBurnProfile: the Burn that followed it reviews no more (refused while
// it runs: its pieces may be waiting for a review).
func (s *server) deleteBurnProfile(w http.ResponseWriter, r *http.Request) {
	p, err := s.cfg.Store.Burn().ReviewProfile(r.Context(), r.PathValue("profile"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if b, err := s.cfg.Store.Burn().Session(r.Context(), p.ProjectID); err == nil && b.ReviewProfileID == p.ID && b.State != "stopped" {
		writeError(w, http.StatusConflict, "Burn đang chạy theo hồ sơ này: tắt Burn hoặc chọn hồ sơ khác trước")
		return
	}
	if err := s.cfg.Store.Burn().DeleteReviewProfile(r.Context(), p.ID); err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "burn.review_profile.delete", Resource: "burn_review_profile", ResourceID: p.ID, ProjectID: p.ProjectID, Before: p})
	w.WriteHeader(http.StatusNoContent)
}
