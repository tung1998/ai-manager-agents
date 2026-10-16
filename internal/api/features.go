package api

import (
	"net/http"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/features"
)

// Feature packs a project turns off (internal/features).
func (s *server) featureRoutes(mux *http.ServeMux, auth, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/features", auth(s.featurePacks))
	mux.Handle("GET /api/projects/{id}/features", auth(s.projectFeatures))
	mux.Handle("PUT /api/projects/{id}/features", admin(s.saveProjectFeatures))
}

func (s *server) featurePacks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"packs": features.Packs})
}

type featuresDTO struct {
	Config features.Config   `json:"config"`
	On     []string          `json:"on"`
	Off    map[string]string `json:"off"` // feature: why (project | no_folder)
	Packs  []features.Pack   `json:"packs"`
}

func (s *server) featuresOf(w http.ResponseWriter, r *http.Request) (featuresDTO, bool) {
	id := r.PathValue("id")
	p, err := s.cfg.Store.Repos().Get(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, err)
		return featuresDTO{}, false
	}
	c := features.LoadConfig(r.Context(), s.cfg.Store, id)
	on, off := features.Effective(c, p.Path != "")
	return featuresDTO{Config: c, On: on, Off: off, Packs: features.Packs}, true
}

func (s *server) projectFeatures(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.featuresOf(w, r); ok {
		writeJSON(w, http.StatusOK, d)
	}
}

func (s *server) saveProjectFeatures(w http.ResponseWriter, r *http.Request) {
	var in features.Config
	if !decode(w, r, &in) {
		return
	}
	before, ok := s.featuresOf(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := features.Save(r.Context(), s.cfg.Store, id, in); err != nil {
		s.internal(w, r, err)
		return
	}
	if s.cfg.Events != nil {
		s.cfg.Events.Wrote("repos") // the projects' payload carries their features: every open list reloads
	}
	after, _ := s.featuresOf(w, r)
	s.audit(r, audit.Change{Action: "project.features", ResourceID: id, ProjectID: id,
		Before: map[string]any{"features": before.On}, After: map[string]any{"features": after.On}})
	writeJSON(w, http.StatusOK, after)
}
