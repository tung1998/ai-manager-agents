package chat_test

import (
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// TestWorkflowStepPanicFailsRunInsteadOfHanging: a panic inside one step's
// goroutine (runSteps' per-step dispatch in workflow_steps.go) must still
// send a result on the results channel — a bare recover there would drop it
// silently and runSteps would wait on that step forever. It must end the run
// as storage.RunFailed with a note mentioning the internal error, not hang.
func TestWorkflowStepPanicFailsRunInsteadOfHanging(t *testing.T) {
	g := newWFGroup(t)
	src := `---
key: boom-wf
name: Panics
steps:
  - id: boom
    type: code
    lang: bash
    script: "echo hi"
    next: fin
  - id: fin
    type: end
    summary: "done"
---
`
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, nil); err != nil {
		t.Fatal(err)
	}
	g.engine.SetTestStepPanic("boom")
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "#boom-wf", nil)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, call0) // waits for awaitRun, which waits for runSteps to end

	r := g.run(t)
	if r.Status != storage.RunFailed {
		t.Fatalf("run status = %q, want %q (a step panic must not hang runSteps); run: %+v", r.Status, storage.RunFailed, r)
	}
	if !strings.Contains(r.Error, "internal error") {
		t.Fatalf("run.Error = %q, want it to mention the panic", r.Error)
	}
}
