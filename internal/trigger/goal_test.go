package trigger_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// goalExec is an agent that makes the file "done" in dir at its turn doneAt
// (0 = never), and a judge that answers with the verdicts given, in order.
type goalExec struct {
	mu       sync.Mutex
	dir      string
	doneAt   int
	prompts  []string
	verdicts []string
	judged   []string // the ceiling each judge ran under
}

func (g *goalExec) RunChat(ctx context.Context, projectID, agentID, conv, prompt, edit string) (string, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prompts = append(g.prompts, prompt)
	if len(g.prompts) == g.doneAt {
		_ = os.WriteFile(filepath.Join(g.dir, "done"), []byte("ok"), 0o644)
	}
	return "cnv_goal", "lượt " + string(rune('0'+len(g.prompts))), nil
}

func (g *goalExec) Judge(ctx context.Context, projectID, agentID, prompt string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.judged = append(g.judged, trigger.CeilingOf(ctx))
	v := g.verdicts[0]
	g.verdicts = g.verdicts[1:]
	return v, nil
}

func goalAutomation(t *testing.T, st storage.Store, p storage.Repo, g storage.AutomationGoal) storage.Automation {
	a, err := st.Automations().Create(context.Background(), storage.Automation{ProjectID: p.ID, Name: "fix", Source: "schedule", Action: "chat", Enabled: true,
		Prompt: "sửa cho tới khi xong", Config: storage.AutomationConfig{EveryMinutes: 60, Goal: &g}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func mainJob(t *testing.T, st storage.Store, a storage.Automation) storage.Job {
	t.Helper()
	list, _ := st.Jobs().List(context.Background(), storage.JobFilter{OriginID: a.ID})
	for _, j := range list {
		if j.ParentJobID == "" {
			return j
		}
	}
	t.Fatal("no job")
	return storage.Job{}
}

// The check fails after the first turn: the chat is sent its output, and the
// second turn reaches the goal (ADR-132).
func TestGoalCheckThenPasses(t *testing.T) {
	st, p := openStore(t)
	ex := &goalExec{dir: p.Path, doneAt: 2}
	r := trigger.New(st, ex)
	a := goalAutomation(t, st, p, storage.AutomationGoal{Check: "test -f done || { echo 'chưa có file done'; exit 3; }"})
	runOnce(t, r, st, a, "")
	if len(ex.prompts) != 2 || !strings.Contains(ex.prompts[1], "chưa có file done") || !strings.Contains(ex.prompts[1], "exit 3") || !strings.Contains(ex.prompts[1], "round 2 of 5") {
		t.Fatalf("prompts = %q", ex.prompts)
	}
	j := mainJob(t, st, a)
	if j.Status != "done" || !strings.HasPrefix(j.Output, "Mục tiêu: đạt sau 2 vòng") || j.ExitCode == nil || *j.ExitCode != 0 {
		t.Fatalf("job = %s %q exit %v", j.Status, j.Output, j.ExitCode)
	}
}
