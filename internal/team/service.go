package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// WorkflowInstaller puts shipped workflows into a project (workflow.Service).
type WorkflowInstaller interface {
	InstallDefaults(ctx context.Context, projectID string, keys []string) error
}

// Service is the use-case layer for a project's agents.
type Service struct {
	store     storage.Store
	workflows WorkflowInstaller // nil: packs install no workflows
}

// NewService builds a Service.
func NewService(store storage.Store, workflows WorkflowInstaller) *Service {
	return &Service{store: store, workflows: workflows}
}

// ErrHasAgents: the project has agents already (apply a pack with replace).
var ErrHasAgents = errors.New("project đã có agent")

// ApplyPack gives a project a starter pack's agents and workflows. replace
// puts the pack's agents in place of the project's (kept by key, so their
// chats stay theirs); otherwise the project must have none yet.
func (s *Service) ApplyPack(ctx context.Context, projectID string, p Pack, replace bool) error {
	if !replace {
		if list, err := s.store.Agents().List(ctx, projectID); err != nil {
			return err
		} else if len(list) > 0 {
			return ErrHasAgents
		}
	}
	if err := s.Replace(ctx, projectID, Snapshot{Default: p.Default, Agents: p.Agents}, "pack:"+p.Key); err != nil {
		return err
	}
	if s.workflows != nil && len(p.Workflows) > 0 {
		return s.workflows.InstallDefaults(ctx, projectID, p.Workflows)
	}
	return nil
}

// Load is a project's agents as a snapshot.
func (s *Service) Load(ctx context.Context, projectID string) (Snapshot, error) {
	r, err := s.store.Repos().Get(ctx, projectID)
	if err != nil {
		return Snapshot{}, err
	}
	agents, err := s.store.Agents().List(ctx, projectID)
	if err != nil {
		return Snapshot{}, err
	}
	return Export(r, agents), nil
}

// SaveAgent creates (a.ID empty) or updates an agent of a.ProjectID after
// checking the project's agents as a whole. The first agent of a project
// becomes its default.
func (s *Service) SaveAgent(ctx context.Context, a storage.Agent) (storage.Agent, error) {
	r, err := s.store.Repos().Get(ctx, a.ProjectID)
	if err != nil {
		return a, err
	}
	agents, err := s.store.Agents().List(ctx, a.ProjectID)
	if err != nil {
		return a, err
	}
	specs := make([]AgentSpec, 0, len(agents)+1)
	found := a.ID == ""
	for _, x := range agents {
		if x.ID == a.ID {
			found = true
			continue
		}
		specs = append(specs, SpecOf(x))
	}
	if !found {
		return a, storage.ErrNotFound
	}
	if a.ID == "" && a.Sort == 0 {
		a.Sort = len(agents)
	}
	if err := Validate(append(specs, SpecOf(a))); err != nil {
		return a, err
	}
	action := "agent.update:" + a.Key
	if a.ID == "" {
		action = "agent.create:" + a.Key
	}
	err = s.store.InTx(ctx, func(tx storage.Store) error {
		if err := snapshot(ctx, tx, a.ProjectID, action); err != nil {
			return err
		}
		if a.ID != "" {
			return tx.Agents().Update(ctx, a)
		}
		created, err := tx.Agents().Create(ctx, a)
		if err != nil {
			return err
		}
		a = created
		if _, ok := storage.DefaultAgent(r, agents); !ok && r.DefaultAgentID == "" {
			r.DefaultAgentID = a.ID
			return tx.Repos().Update(ctx, r)
		}
		return nil
	})
	return a, err
}

// DeleteAgent removes an agent; if it was the default, the next one is.
func (s *Service) DeleteAgent(ctx context.Context, id string) error {
	a, err := s.store.Agents().Get(ctx, id)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(tx storage.Store) error {
		if err := snapshot(ctx, tx, a.ProjectID, "agent.delete:"+a.Key); err != nil {
			return err
		}
		if err := tx.Agents().Delete(ctx, id); err != nil {
			return err
		}
		return fixDefault(ctx, tx, a.ProjectID)
	})
}

// SetDefault makes agentID the project's default agent.
func (s *Service) SetDefault(ctx context.Context, projectID, agentID string) error {
	a, err := s.store.Agents().Get(ctx, agentID)
	if err != nil {
		return err
	}
	if a.ProjectID != projectID {
		return storage.ErrNotFound
	}
	r, err := s.store.Repos().Get(ctx, projectID)
	if err != nil {
		return err
	}
	r.DefaultAgentID = agentID
	return s.store.Repos().Update(ctx, r)
}

// Revisions lists snapshots of a project's agents, newest first.
func (s *Service) Revisions(ctx context.Context, projectID string, limit int) ([]storage.Revision, error) {
	return s.store.Revisions().List(ctx, projectID, limit)
}

// Restore puts a project's agents back to a snapshot. The current state is
// snapshotted first, so a restore can itself be undone.
func (s *Service) Restore(ctx context.Context, revisionID string) (storage.Revision, error) {
	rev, err := s.store.Revisions().Get(ctx, revisionID)
	if err != nil {
		return rev, err
	}
	snap, err := RevisionSnapshot(rev)
	if err != nil {
		return rev, err
	}
	return rev, s.Replace(ctx, rev.ProjectID, snap, "restore:"+rev.ID)
}

// Replace makes a project's agents those of snap: an agent whose key is
// there keeps its id (and its chats), the others are made or removed.
func (s *Service) Replace(ctx context.Context, projectID string, snap Snapshot, action string) error {
	if err := Validate(snap.Agents); err != nil {
		return err
	}
	return s.store.InTx(ctx, func(tx storage.Store) error {
		r, err := tx.Repos().Get(ctx, projectID)
		if err != nil {
			return err
		}
		if err := snapshot(ctx, tx, projectID, action); err != nil {
			return err
		}
		current, err := tx.Agents().List(ctx, projectID)
		if err != nil {
			return err
		}
		byKey := map[string]storage.Agent{}
		for _, a := range current {
			byKey[a.Key] = a
		}
		keep := map[string]bool{}
		for i, spec := range snap.Agents {
			if spec.ProviderID != "" {
				if _, err := tx.Providers().Get(ctx, spec.ProviderID); errors.Is(err, storage.ErrNotFound) {
					spec.ProviderID = "" // gone since: the default connection
				} else if err != nil {
					return err
				}
			}
			next := spec.Agent(projectID, i)
			if old, ok := byKey[spec.Key]; ok {
				// an existing agent keeps its access an admin gave it (ADR-074)
				next.ID, next.Disabled = old.ID, old.Disabled
				next.Permissions.FullAccess, next.Permissions.FullAccessBy, next.Permissions.ExtraDirs =
					old.Permissions.FullAccess, old.Permissions.FullAccessBy, old.Permissions.ExtraDirs
				if err := tx.Agents().Update(ctx, next); err != nil {
					return fmt.Errorf("agent %s: %w", spec.Key, err)
				}
			} else if next, err = tx.Agents().Create(ctx, next); err != nil {
				return fmt.Errorf("agent %s: %w", spec.Key, err)
			}
			keep[spec.Key] = true
			if spec.Key == snap.Default {
				r.DefaultAgentID = next.ID
			}
		}
		for _, a := range current {
			if !keep[a.Key] {
				if err := tx.Agents().Delete(ctx, a.ID); err != nil {
					return err
				}
			}
		}
		if err := tx.Repos().Update(ctx, r); err != nil {
			return err
		}
		return fixDefault(ctx, tx, projectID)
	})
}

// InstallWorkflows installs a pack's workflows into a project (keys already
// installed are skipped by the workflow service).
func (s *Service) InstallWorkflows(ctx context.Context, projectID string, keys []string) error {
	if s.workflows == nil || len(keys) == 0 {
		return nil
	}
	return s.workflows.InstallDefaults(ctx, projectID, keys)
}

// Merge adds a pack's agents to a project without touching or removing the
// ones it already has: an existing key is left exactly as the project has
// it, a new key is created, and the default is set only if the project has
// none yet. Used when a project already has agents an admin may have
// hand-edited, so a rescan's setup assistant doesn't clobber them.
func (s *Service) Merge(ctx context.Context, projectID string, snap Snapshot, action string) error {
	if err := Validate(snap.Agents); err != nil {
		return err
	}
	return s.store.InTx(ctx, func(tx storage.Store) error {
		r, err := tx.Repos().Get(ctx, projectID)
		if err != nil {
			return err
		}
		current, err := tx.Agents().List(ctx, projectID)
		if err != nil {
			return err
		}
		haveKey := map[string]bool{}
		for _, a := range current {
			haveKey[a.Key] = true
		}
		var toAdd []AgentSpec
		for _, spec := range snap.Agents {
			if !haveKey[spec.Key] {
				toAdd = append(toAdd, spec)
			}
		}
		if len(toAdd) == 0 {
			return nil
		}
		if err := snapshot(ctx, tx, projectID, action); err != nil {
			return err
		}
		for i, spec := range toAdd {
			if spec.ProviderID != "" {
				if _, err := tx.Providers().Get(ctx, spec.ProviderID); errors.Is(err, storage.ErrNotFound) {
					spec.ProviderID = "" // gone since: the default connection
				} else if err != nil {
					return err
				}
			}
			next := spec.Agent(projectID, len(current)+i)
			created, err := tx.Agents().Create(ctx, next)
			if err != nil {
				return fmt.Errorf("agent %s: %w", spec.Key, err)
			}
			if r.DefaultAgentID == "" && spec.Key == snap.Default {
				r.DefaultAgentID = created.ID
			}
		}
		if err := tx.Repos().Update(ctx, r); err != nil {
			return err
		}
		return fixDefault(ctx, tx, projectID)
	})
}

// fixDefault points the project's default at an agent that exists.
func fixDefault(ctx context.Context, tx storage.Store, projectID string) error {
	r, err := tx.Repos().Get(ctx, projectID)
	if err != nil {
		return err
	}
	agents, err := tx.Agents().List(ctx, projectID)
	if err != nil {
		return err
	}
	for _, a := range agents {
		if a.ID == r.DefaultAgentID {
			return nil
		}
	}
	r.DefaultAgentID = ""
	if len(agents) > 0 {
		r.DefaultAgentID = agents[0].ID
	}
	return tx.Repos().Update(ctx, r)
}

// snapshot stores a project's agents before a change.
func snapshot(ctx context.Context, tx storage.Store, projectID, action string) error {
	r, err := tx.Repos().Get(ctx, projectID)
	if err != nil {
		return err
	}
	agents, err := tx.Agents().List(ctx, projectID)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return nil // nothing to go back to
	}
	raw, err := json.Marshal(Export(r, agents))
	if err != nil {
		return err
	}
	if _, err := tx.Revisions().Create(ctx, storage.Revision{
		ProjectID: projectID, Action: action, Actor: actor.From(ctx), AgentCount: len(agents), Snapshot: raw,
	}); err != nil {
		return err
	}
	return tx.Revisions().Prune(ctx, projectID, KeepRevisions)
}
