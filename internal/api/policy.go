package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// allowedMode: members may only ask first; admins may pick any package
// (the project's cap and each agent's own package still apply).
func (s *server) allowedMode(r *http.Request, mode string) string {
	if !perm.Valid(mode) {
		return perm.Propose
	}
	if userFrom(r).Role != storage.RoleAdmin && perm.AtLeast(mode, perm.Check) {
		return perm.Propose
	}
	return mode
}

func (s *server) permissionLevels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"levels": perm.All, "caps": perm.Caps, "presets": presets()})
}

// presets: the capabilities of each package, for the dashboard.
func presets() map[string][]string {
	out := map[string][]string{}
	for _, l := range perm.All {
		out[l.Level] = perm.Preset(l.Level)
	}
	return out
}

func (s *server) getPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := s.cfg.Store.Repos().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	pol := perm.LoadPolicy(r.Context(), s.cfg.Store, p.ID)
	writeJSON(w, http.StatusOK, map[string]any{"policy": pol, "levels": perm.All, "packs": perm.ProjectPacks(p.Path, pol.Packs)})
}

func (s *server) putPolicy(w http.ResponseWriter, r *http.Request) {
	var in perm.Policy
	if !decode(w, r, &in) {
		return
	}
	projectID := r.PathValue("id")
	if _, err := s.cfg.Store.Repos().Get(r.Context(), projectID); err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	if !perm.Valid(in.MaxLevel) {
		writeError(w, http.StatusBadRequest, "gói tối đa không hợp lệ")
		return
	}
	// allow lists must name this project's processes
	procs, _ := s.cfg.Store.Processes().List(r.Context(), projectID)
	known := map[string]bool{}
	for _, p := range procs {
		known[p.ID] = true
	}
	cmds := []string{}
	for _, id := range in.AllowedCommands {
		if known[id] {
			cmds = append(cmds, id)
		}
	}
	in.AllowedCommands = cmds
	clean := func(list []string) []string {
		out := []string{}
		for _, x := range list {
			if x = strings.TrimSpace(x); x != "" {
				out = append(out, x)
			}
		}
		return out
	}
	in.AllowedContainers, in.DenyPaths = clean(in.AllowedContainers), clean(in.DenyPaths)
	patterns := func(list []string) ([]string, error) {
		out := []string{}
		for _, x := range list {
			if strings.TrimSpace(x) == "" {
				continue
			}
			c, err := perm.CleanPattern(x)
			if err != nil {
				return nil, fmt.Errorf("lệnh %q không hợp lệ: chạy không qua shell nên không dùng | ; & > $, và * chỉ ở cuối", x)
			}
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
		return out, nil
	}
	var err error
	if in.Commands, err = patterns(in.Commands); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	packs := []perm.Pack{}
	for i, p := range in.Packs {
		p.Label, p.Custom = strings.TrimSpace(p.Label), false
		if p.Label == "" {
			continue
		}
		if p.Commands, err = patterns(p.Commands); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if p.ID == "" || !strings.HasPrefix(p.ID, "custom-") {
			p.ID = fmt.Sprintf("custom-%d", i+1)
		}
		packs = append(packs, p)
	}
	in.Packs = packs
	if err := perm.SavePolicy(r.Context(), s.cfg.Store, projectID, in); err != nil {
		s.internal(w, r, err)
		return
	}
	s.auditAction(r, "project.policy", projectID, map[string]any{"max_level": in.MaxLevel, "processes": len(in.AllowedCommands), "commands": in.Commands,
		"containers": in.AllowedContainers, "deny_paths": in.DenyPaths})
	writeJSON(w, http.StatusOK, map[string]any{"policy": in})
}
