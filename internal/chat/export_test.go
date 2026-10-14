package chat

// SetTestStepPanic (tests only): runSteps' per-step goroutine panics
// instead of running the step with this id — forces that path
// deterministically to check a step panic doesn't hang the run.
func (e *Engine) SetTestStepPanic(stepID string) { e.testStepPanic = stepID }
