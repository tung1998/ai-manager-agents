package transfer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// failOnNthAgentList wraps a real store but makes Agents().List fail on the
// nth call, so putWorkflows' roles() resolution errors partway through a
// bundle with more than one workflow.
type failOnNthAgentList struct {
	storage.Store
	n     int
	calls int
}

func (f *failOnNthAgentList) Agents() storage.AgentRepo {
	return failingAgentRepo{storage.AgentRepo(f.Store.Agents()), f}
}

type failingAgentRepo struct {
	storage.AgentRepo
	f *failOnNthAgentList
}

func (a failingAgentRepo) List(ctx context.Context, projectID string) ([]storage.Agent, error) {
	a.f.calls++
	if a.f.calls == a.f.n {
		return nil, errors.New("boom: agents list unavailable")
	}
	return a.AgentRepo.List(ctx, projectID)
}

func TestPutWorkflowsResolveFailureLeavesEarlierWorkflowsUnwritten(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	real, err := sqlite.Open(filepath.Join(dir, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { real.Close() })
	if err := real.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := real.Repos().Create(ctx, storage.Repo{Name: "shop"})
	if err != nil {
		t.Fatal(err)
	}

	handoff := workflow.BuiltinSource("handoff")
	review := strings.Replace(workflow.BuiltinSource("handoff"), "key: handoff", "key: review", 1)

	in := []ProjectWorkflow{
		// Roles present so putWorkflows calls s.roles() -> Agents().List for both.
		{Key: "handoff", Source: handoff, Enabled: true, Roles: map[string]string{"owner": "lead"}},
		{Key: "review", Source: review, Enabled: true, Roles: map[string]string{"owner": "lead"}},
	}

	// Fail on the 2nd Agents().List call, i.e. while resolving the 2nd workflow.
	fs := &failOnNthAgentList{Store: real, n: 2}
	svc := &Service{store: fs}

	if err := svc.putWorkflows(ctx, repo.ID, in); err == nil {
		t.Fatal("expected error from putWorkflows when roles() fails on the 2nd workflow")
	}

	if _, err := real.Workflows().GetByKey(ctx, repo.ID, "handoff"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("1st workflow must not be written when the 2nd fails to resolve, got err=%v", err)
	}
	if _, err := real.Workflows().GetByKey(ctx, repo.ID, "review"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("2nd workflow must not be written either, got err=%v", err)
	}
}

// TestPutWorkflowsDuplicateKeyLastOneWins guards against the resolve-then-write
// split regressing a bundle that lists the same workflow key twice: resolving
// both entries against the DB before any write means both see ErrNotFound and
// both try to Create, which violates the (project_id, key) unique index. The
// two entries must instead collapse into a single write, last one wins.
func TestPutWorkflowsDuplicateKeyLastOneWins(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	real, err := sqlite.Open(filepath.Join(dir, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { real.Close() })
	if err := real.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := real.Repos().Create(ctx, storage.Repo{Name: "shop"})
	if err != nil {
		t.Fatal(err)
	}

	handoff := workflow.BuiltinSource("handoff")
	in := []ProjectWorkflow{
		{Key: "handoff", Source: handoff, Enabled: true},
		{Key: "handoff", Source: handoff, Enabled: false},
	}

	svc := &Service{store: real}
	if err := svc.putWorkflows(ctx, repo.ID, in); err != nil {
		t.Fatalf("putWorkflows() = %v, want nil", err)
	}

	got, err := real.Workflows().GetByKey(ctx, repo.ID, "handoff")
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatalf("got Enabled=true, want the 2nd (last) entry's Enabled=false to win")
	}
}
