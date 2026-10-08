package prompts

// Handoff is work one agent gives another (handoff/task.md): a task handed
// over, a tag in the chat, the call back once a handed task is done, or a
// workflow's role being asked. One shape for all of them, so every hand-off
// reads the same way to the agent taking it.
type Handoff struct {
	Kind       string // "" (a task) | "tagged" | "report" (the task it handed over is done)
	From       string // who hands it over: an agent, "The person", "X (coordinator of the workflow Y)"
	Role       string // a task: the role it is asked as ("" = none)
	Task       string // a task: what to do
	ReportBack string // what to do with it ("" = the Kind's default)
}

// String is the hand-off as the agent taking it reads it.
func (h Handoff) String() string { return Render("handoff/task", h) }
