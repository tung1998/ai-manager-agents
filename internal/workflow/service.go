package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Service installs workflows in projects: a copy of a library one (changed
// there on its own), with the agent that fills each role.
type Service struct {
	Store storage.Store
	Lib   Library
}

// Hash is what tells a library workflow changed since a project copied it.
func Hash(source string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(source)))
	return hex.EncodeToString(h[:8])
}

// ErrBadBinding: a role or an agent of the bindings is not there.
var ErrBadBinding = errors.New("gán vai không hợp lệ")

// agents are a project's agents and its default one's id.
func (s *Service) agents(ctx context.Context, projectID string) ([]storage.Agent, string, error) {
	r, err := s.Store.Repos().Get(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	agents, err := s.Store.Agents().List(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	d, _ := storage.DefaultAgent(r, agents)
	return agents, d.ID, nil
}

// checkBindings keeps the bindings of def's roles to agents of the project.
func (s *Service) checkBindings(ctx context.Context, projectID string, def Def, b map[string]string) (map[string]string, error) {
	out := map[string]string{}
	if len(b) == 0 {
		return out, nil
	}
	agents, _, err := s.agents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for role, id := range b {
		if id == "" {
			continue
		}
		if _, ok := def.Role(role); !ok {
			return nil, fmt.Errorf("%w: quy trình không có vai %q", ErrBadBinding, role)
		}
		if !slices.ContainsFunc(agents, func(a storage.Agent) bool { return a.ID == id }) {
			return nil, fmt.Errorf("%w: agent %q không thuộc project", ErrBadBinding, id)
		}
		out[role] = id
	}
	return out, nil
}

// Suggest fills the roles of def with agents of the project by what their
// hints say, never the default agent (it coordinates), and two roles that
// work at once never with the same agent.
func Suggest(def Def, agents []storage.Agent, defaultID string) map[string]string {
	out := map[string]string{}
	var pool []storage.Agent
	for _, a := range storage.OnAgents(agents) {
		if a.ID != defaultID {
			pool = append(pool, a)
		}
	}
	if len(pool) == 0 {
		return out // only the coordinator: roles are picked when given out
	}
	words := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return r == ',' || r == ' ' || r == '.' || r == ';' || r == ':' || r == '(' || r == ')'
		})
	}
	for _, r := range def.Roles {
		hint := words(r.Hint + " " + r.Name)
		type scored struct {
			a storage.Agent
			n int
		}
		var best []scored
		for _, a := range pool {
			text := strings.ToLower(a.Name + " " + a.Role + " " + a.Description)
			n := 0
			for _, w := range hint {
				if len([]rune(w)) > 2 && strings.Contains(text, w) {
					n++
				}
			}
			if r.Access == AccessEdit && (a.Permissions.ReadOnly && a.Permissions.Level == "") {
				n -= 3 // it cannot edit
			}
			best = append(best, scored{a, n})
		}
		sort.SliceStable(best, func(i, j int) bool { return best[i].n > best[j].n })
		for _, c := range best {
			clash := false
			for other, id := range out {
				if id == c.a.ID && def.SameBatch(r.Key, other) {
					clash = true
				}
			}
			if !clash {
				out[r.Key] = c.a.ID
				break
			}
		}
	}
	return out
}

// Install copies the library workflow key into a project; bindings nil =
// suggested ones.
func (s *Service) Install(ctx context.Context, projectID, key string, bindings map[string]string) (storage.Workflow, error) {
	it, err := s.Lib.Get(key)
	if errors.Is(err, ErrNotFound) && BuiltinSource(key) != "" {
		it, err = Item{Source: BuiltinSource(key)}, nil // taken out of the library, still shipped
	}
	if err != nil {
		return storage.Workflow{}, err
	}
	return s.create(ctx, projectID, it.Source, key, bindings)
}

// Create adds a workflow written for the project.
func (s *Service) Create(ctx context.Context, projectID, source string, bindings map[string]string) (storage.Workflow, error) {
	return s.create(ctx, projectID, source, "", bindings)
}

func (s *Service) create(ctx context.Context, projectID, source, from string, bindings map[string]string) (storage.Workflow, error) {
	def, err := Parse(source)
	if err != nil {
		return storage.Workflow{}, err
	}
	if bindings == nil {
		agents, def0, err := s.agents(ctx, projectID)
		if err != nil {
			return storage.Workflow{}, err
		}
		bindings = Suggest(def, agents, def0)
	}
	b, err := s.checkBindings(ctx, projectID, def, bindings)
	if err != nil {
		return storage.Workflow{}, err
	}
	w := storage.Workflow{ProjectID: projectID, Key: def.Key, Name: def.Name, Description: def.Description, Source: strings.TrimSpace(source) + "\n",
		Bindings: b, Enabled: true}
	if from != "" {
		w.SourceKey, w.SourceHash = from, Hash(source)
	}
	return s.Store.Workflows().Create(ctx, w)
}

// Change is what an edit of a project's workflow sets (nil = as it is).
type Change struct {
	Source   *string
	Bindings map[string]string // nil = as they are; roles no longer there are dropped
	Enabled  *bool
}

// Update changes a project's workflow.
func (s *Service) Update(ctx context.Context, id string, c Change) (storage.Workflow, error) {
	w, err := s.Store.Workflows().Get(ctx, id)
	if err != nil {
		return w, err
	}
	src := w.Source
	if c.Source != nil {
		src = strings.TrimSpace(*c.Source) + "\n"
	}
	def, err := Parse(src)
	if err != nil {
		return w, err
	}
	bindings := w.Bindings
	if c.Bindings != nil {
		bindings = c.Bindings
	}
	keep := map[string]string{}
	for role, agent := range bindings {
		if _, ok := def.Role(role); ok {
			keep[role] = agent
		}
	}
	if keep, err = s.checkBindings(ctx, w.ProjectID, def, keep); err != nil {
		return w, err
	}
	w.Source, w.Key, w.Name, w.Description, w.Bindings = src, def.Key, def.Name, def.Description, keep
	if c.Enabled != nil {
		w.Enabled = *c.Enabled
	}
	return w, s.Store.Workflows().Update(ctx, w)
}

// FromLibrary replaces a project's workflow with the library's newer one
// (the bindings of roles still there are kept).
func (s *Service) FromLibrary(ctx context.Context, id string) (storage.Workflow, error) {
	w, err := s.Store.Workflows().Get(ctx, id)
	if err != nil {
		return w, err
	}
	it, err := s.Lib.Get(cmpOr(w.SourceKey, w.Key))
	if err != nil {
		return w, err
	}
	w, err = s.Update(ctx, id, Change{Source: &it.Source})
	if err != nil {
		return w, err
	}
	w.SourceKey, w.SourceHash = it.Def.Key, Hash(it.Source)
	return w, s.Store.Workflows().Update(ctx, w)
}

// HasUpdate: the library workflow a project's copy came from changed since.
func (s *Service) HasUpdate(w storage.Workflow) bool {
	if w.SourceKey == "" {
		return false
	}
	it, err := s.Lib.Get(w.SourceKey)
	return err == nil && Hash(it.Source) != w.SourceHash
}

// InstallDefaults puts the shipped workflows named in keys into a project
// (those it has not got yet); the ones missing from the library are skipped.
func (s *Service) InstallDefaults(ctx context.Context, projectID string, keys []string) error {
	for _, k := range keys {
		if _, err := s.Store.Workflows().GetByKey(ctx, projectID, k); err == nil {
			continue
		}
		if _, err := s.Install(ctx, projectID, k, nil); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("quy trình %s: %w", k, err)
		}
	}
	return nil
}

// FillMigrated gives the workflows a migration installed without a body
// (a council project's org model became "hoi-dong-3-ben", ADR-099) the
// shipped one; a role bound to an agent no longer there is left unbound.
func (s *Service) FillMigrated(ctx context.Context) (int, error) {
	list, err := s.Store.Workflows().List(ctx, "")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, w := range list {
		if strings.TrimSpace(w.Source) != "" || w.SourceKey == "" {
			continue
		}
		src := BuiltinSource(w.SourceKey)
		if it, err := s.Lib.Get(w.SourceKey); err == nil {
			src = it.Source
		}
		def, err := Parse(src)
		if err != nil {
			continue
		}
		agents, _, err := s.agents(ctx, w.ProjectID)
		if err != nil {
			return n, err
		}
		keep := map[string]string{}
		for role, id := range w.Bindings {
			if _, ok := def.Role(role); ok && slices.ContainsFunc(agents, func(a storage.Agent) bool { return a.ID == id }) {
				keep[role] = id
			}
		}
		w.Source, w.Key, w.Name, w.Description, w.Bindings = strings.TrimSpace(src)+"\n", def.Key, def.Name, def.Description, keep
		w.SourceHash = Hash(src)
		if err := s.Store.Workflows().Update(ctx, w); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
