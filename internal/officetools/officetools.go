// Package officetools gives agents read-only access to what office knows
// about a project at run time: processes (build/dev/test), docker compose
// services, health checks and their incidents. The same tools are served to
// Claude Code over MCP (internal/mcpserver) and to API agents directly.
package officetools

import (
	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/gitops"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ops"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Tool is one callable tool.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	// ReadOnly: the tool never changes state (MCP readOnlyHint). Defaults to
	// false so a new tool must be marked read-only on purpose.
	ReadOnly bool
}

// Toolbox runs the tools for one project at a time.
type Toolbox struct {
	store   storage.Store
	ops     *ops.Manager
	actions *actions.Service
	// delegate hands a task to another agent of the chat (ADR-044), set by the chat engine
	delegate func(ctx context.Context, sc Scope, agent, task string) (string, error)
	sendFile func(ctx context.Context, sc Scope, path, caption string) (string, error)
	burn     func(ctx context.Context, sc Scope, name string, in BurnInput) (string, error)
	// config reads settings for describe/list/get (the API's registry, ADR-045)
	config ConfigReader
	// assistant is the office assistant's own project (hidden from the list)
	assistant func(ctx context.Context) string
	// wf runs the workflow_* tools (the chat engine)
	wf Workflows
}

// Workflows runs the workflow_* tools for the coordinator of a workflow
// running in a chat (spec 2026-10-07-workflows-design).
type Workflows interface {
	// WorkflowScope: coordinator = the run of sc coordinates a workflow;
	// inRun = it is a turn of a running workflow (no delegate then).
	WorkflowScope(sc Scope) (coordinator, inRun bool)
	WorkflowCall(ctx context.Context, sc Scope, name string, raw json.RawMessage) (string, error)
}

// SetWorkflow turns on the workflow_* tools.
func (t *Toolbox) SetWorkflow(w Workflows) { t.wf = w }

// SetOffice tells the tools which project is the office assistant's.
func (t *Toolbox) SetOffice(fn func(ctx context.Context) string) { t.assistant = fn }

// ConfigReader reads settings for the generic tools.
type ConfigReader interface {
	DescribeConfig(kind string) (string, error)
	ListConfig(ctx context.Context, projectID, kind string) (string, error)
	GetConfig(ctx context.Context, projectID, kind, id string) (string, error)
}

// SetConfig turns on describe, list, get and propose_change.
func (t *Toolbox) SetConfig(c ConfigReader) { t.config = c }

// SetDelegate turns on the delegate tool (the chat engine runs hand-offs).
func (t *Toolbox) SetDelegate(fn func(ctx context.Context, sc Scope, agent, task string) (string, error)) {
	t.delegate = fn
}

// BurnInput is what the burn_* tools take.
type BurnInput struct{ Title, Kind, Detail, Item, Summary, Reason, What, Scanned string }

// SetBurn turns on the burn_* tools (a Burn's conversation only).
func (t *Toolbox) SetBurn(fn func(ctx context.Context, sc Scope, name string, in BurnInput) (string, error)) {
	t.burn = fn
}

// SetSendFile turns on the send_file tool (a bot's chat posts the file, ADR-083).
func (t *Toolbox) SetSendFile(fn func(ctx context.Context, sc Scope, path, caption string) (string, error)) {
	t.sendFile = fn
}

// Scope is who calls a tool: the project, and the conversation/task and run
// that proposals are attached to.
type Scope = actions.Scope

// New builds a Toolbox; ops may be nil (processes/containers unavailable),
// acts may be nil (no propose_action).
func New(store storage.Store, o *ops.Manager, acts *actions.Service) *Toolbox {
	return &Toolbox{store: store, ops: o, actions: acts}
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var linesProp = map[string]any{"type": "integer", "description": "Number of last log lines (default 200, max 1000)"}

// Tools lists the tools (names are stable: agents and the UI refer to them).
func (t *Toolbox) Tools() []Tool {
	list := []Tool{
		{Name: "ops_overview", Description: "Operations overview: processes (dev, build, test…) with status/exit code/port, docker compose services, monitors and recent incidents. Call it first for build or runtime errors, deploys or monitoring.",
			Schema: obj(map[string]any{}), ReadOnly: true},
		{Name: "process_logs", Description: "Read the latest log of a process office runs for the project (e.g. dev, build, test), with its command, status and exit code.",
			Schema: obj(map[string]any{"name": map[string]any{"type": "string", "description": "Process name, see ops_overview"}, "lines": linesProp}, "name"), ReadOnly: true},
		{Name: "container_logs", Description: "Read the latest log of a docker compose service of the project, with the container status.",
			Schema: obj(map[string]any{"service": map[string]any{"type": "string"}, "lines": linesProp}, "service"), ReadOnly: true},
		{Name: "monitor_detail", Description: "Details of a monitor: config, recent checks, Up/Down events and earlier AI analysis.",
			Schema: obj(map[string]any{"name": map[string]any{"type": "string", "description": "Monitor name, see ops_overview"}}, "name"), ReadOnly: true},
	}
	list = append(list,
		Tool{Name: "git_status", Description: "Git status of the project: branch, unpushed commits, changed files.", Schema: obj(map[string]any{}), ReadOnly: true},
		Tool{Name: "git_diff", Description: "Diff of uncommitted changes (against HEAD), optionally limited to files.",
			Schema: obj(map[string]any{"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}), ReadOnly: true},
		Tool{Name: "git_log", Description: "The latest commits.", Schema: obj(map[string]any{"lines": map[string]any{"type": "integer"}}), ReadOnly: true},
	)
	if t.actions != nil {
		list = append(list, Tool{Name: "run_command", Description: "Run one command in the project folder (no shell: no | ; & > $), e.g. test, lint, build. Commands on your allowed list run at once; others become a proposal awaiting the person's approval.",
			Schema: obj(map[string]any{
				"command": map[string]any{"type": "string", "description": "Command line, e.g. go test ./internal/..."},
				"reason":  map[string]any{"type": "string", "description": "Why it needs to run"},
			}, "command", "reason")})
		list = append(list, Tool{Name: "propose_action", Description: "Propose an action for the person to approve (runs on its own if your permissions allow; git_push always needs approval). Use it to re-run build/test after a fix, restart a stuck service, or commit/push when asked.",
			Schema: obj(map[string]any{
				"action":  map[string]any{"type": "string", "enum": []string{"run_process", "restart_process", "stop_process", "start_container", "restart_container", "stop_container", "git_commit", "git_branch", "git_push"}},
				"target":  map[string]any{"type": "string", "description": "*_process: process name; *_container: docker compose service"},
				"reason":  map[string]any{"type": "string", "description": "Why this action is needed"},
				"message": map[string]any{"type": "string", "description": "git_commit: commit message following the repo's conventions"},
				"files":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "git_commit: files to commit (empty = all changes)"},
				"branch":  map[string]any{"type": "string", "description": "git_branch: new branch name"},
			}, "action", "reason")})
		list = append(list, Tool{Name: "send_to_chat", Description: "Send a message into ANOTHER office chat as the person you chat with, for its agent to carry on, when they ask to pass work there. Find the chat with search_history or a link they pasted.",
			Schema: obj(map[string]any{
				"chat":   map[string]any{"type": "string", "description": "Target chat: dashboard link (…?tab=chat&c=…) or id cnv_…"},
				"text":   map[string]any{"type": "string", "description": "Message text, clear enough for the agent there to act without asking back"},
				"reason": map[string]any{"type": "string", "description": "Why it needs sending"},
			}, "chat", "text", "reason")})
		list = append(list, Tool{Name: "remember", Description: "Save one short point worth keeping long term to your notes for this project (seen in every later chat): conventions, decisions, the person's preferences, fixes. Nothing one-off, no secrets.",
			Schema: obj(map[string]any{
				"note":   map[string]any{"type": "string", "description": "What to remember, e.g. The repo uses pnpm; run pnpm test before reporting done"},
				"reason": map[string]any{"type": "string", "description": "Why it is worth remembering"},
			}, "note")})
		str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
		list = append(list, Tool{Name: "propose_automation", Description: "Propose an automation (the person approves it). Prefer action=script (NO AI tokens); chat/task only when every run needs AI; workflow runs a project workflow. With automation_id it edits that one.",
			Schema: obj(map[string]any{
				"automation_id": str("Edit this automation (empty = create a new one)"),
				"name":          str("Short name"),
				"source":        map[string]any{"type": "string", "enum": []string{"schedule", "webhook"}},
				"every_minutes": map[string]any{"type": "integer"},
				"cron":          str("5-field cron, e.g. 0 8 * * 1-5"),
				"timezone":      str("IANA, e.g. Asia/Ho_Chi_Minh"),
				"action":        map[string]any{"type": "string", "enum": []string{"script", "chat", "task", "workflow"}},
				"workflow":      str("workflow: key of the workflow to run (agent_id is the coordinating agent)"),
				"agent_id":      str("chat: answering agent (empty = team lead)"),
				"prompt":        str("chat/task: text sent to the agent; workflow: the workflow's input; supports {{payload}}, {{today}}…"),
				"script": obj(map[string]any{
					"lang":      map[string]any{"type": "string", "enum": []string{"bash", "node", "python"}},
					"body":      str("Source; runs in the project folder, payload on stdin and $OFFICE_PAYLOAD, result on stdout, non-zero exit on error, a line '@@agent: <text>' when an agent should look"),
					"timeout_s": map[string]any{"type": "integer", "description": "Default 300, max 3600"},
				}),
				"escalate": obj(map[string]any{
					"when":     map[string]any{"type": "string", "enum": []string{"never", "failure", "signal"}, "description": "failure (default): the script fails; signal: it prints an @@agent line"},
					"action":   map[string]any{"type": "string", "enum": []string{"chat", "task"}},
					"agent_id": str("Handling agent (empty = team lead)"),
					"prompt":   str("Text sent to the agent; supports {{output}}, {{exit_code}}, {{message}}"),
				}),
				"ends_at": str("schedule: time to stop on its own (RFC3339, e.g. 2026-10-12T00:00:00+07:00); empty = runs until turned off"),
				"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "chat: tags put on each run's chat (max 10; empty = keep the current tags)"},
				"reason":  str("Why this automation is needed"),
			}, "name", "source", "action", "reason")})
	}
	if t.config != nil && t.actions != nil {
		kind := map[string]any{"type": "string", "description": "Setting type, see describe"}
		list = append(list,
			Tool{Name: "describe", Description: "The setting types that can be changed (automation, agent, workflow, monitor, process, policy, project, usage_settings, provider); with resource, lists its editable fields.",
				Schema: obj(map[string]any{"resource": kind}), ReadOnly: true},
			Tool{Name: "list", Description: "The settings of one type in the project (id, name, status).", Schema: obj(map[string]any{"resource": kind}, "resource"), ReadOnly: true},
			Tool{Name: "get", Description: "One setting in full (secrets masked). policy, project and usage_settings need no id.",
				Schema: obj(map[string]any{"resource": kind, "id": map[string]any{"type": "string"}}, "resource"), ReadOnly: true},
			Tool{Name: "propose_change", Description: "PROPOSE changing a setting; office applies it once the person approves the card. patch holds only the fields to change (see describe/get); never include API keys: the person pastes them on the card.",
				Schema: obj(map[string]any{
					"resource": kind,
					"op":       map[string]any{"type": "string", "enum": []string{"create", "update", "delete"}},
					"id":       map[string]any{"type": "string", "description": "Setting to edit/delete (empty when creating, and for policy/project/usage_settings)"},
					"patch":    map[string]any{"type": "object"},
					"reason":   map[string]any{"type": "string", "description": "Why it needs changing"},
				}, "resource", "op", "reason")},
		)
	}
	list = append(list, Tool{Name: "search_history", Description: "Search chat history (web, Discord, Telegram, automations) by keyword, ignoring accents. When the person refers to earlier work, search first, don't guess; read a result in full with read_link.",
		Schema: obj(map[string]any{
			"query":  map[string]any{"type": "string", "description": "Keywords, a few words are enough"},
			"days":   map[string]any{"type": "integer", "description": "How many recent days to search (default 30, max 365)"},
			"author": map[string]any{"type": "string", "description": "Only messages from this person/agent (optional)"},
		}, "query"), ReadOnly: true})
	list = append(list, Tool{Name: "read_link", Description: "Read an office link the person pasted: a chat (…?tab=chat&c=…), a message (&m=…) or a task (…?tab=tasks&task=…) of this project.",
		Schema: obj(map[string]any{"url": map[string]any{"type": "string", "description": "Office dashboard link"}}, "url"), ReadOnly: true})
	if t.burn != nil {
		item := map[string]any{"type": "string", "description": "Piece id (bit_…)"}
		list = append(list,
			Tool{Name: "burn_add", Description: "Burn: record a piece of work found (no duplicates).", Schema: obj(map[string]any{
				"title":  map[string]any{"type": "string", "description": "Short title"},
				"kind":   map[string]any{"type": "string", "enum": []string{"unfinished", "upgrade", "bug", "idea"}},
				"detail": map[string]any{"type": "string", "description": "Enough to do it: where, why, and what done looks like"},
			}, "title", "kind")},
			Tool{Name: "burn_pick", Description: "Burn: pick the next piece to work on.", Schema: obj(map[string]any{"item": item}, "item")},
			Tool{Name: "burn_skip", Description: "Burn: drop a piece not worth doing for good (permanent, never picked again). A piece that is only outside the current focus or not its turn yet: do NOT skip it, leave it in the queue.", Schema: obj(map[string]any{"item": item, "reason": map[string]any{"type": "string"}}, "item", "reason")},
			Tool{Name: "burn_done", Description: "Burn: report the current piece done.", Schema: obj(map[string]any{"item": item, "summary": map[string]any{"type": "string", "description": "What was done and how it was checked"}}, "item", "summary")},
			Tool{Name: "burn_claim", Description: "Burn worker: claim the one piece you found, before changing code (refused if another piece is the same: find another).", Schema: obj(map[string]any{
				"item":    item,
				"title":   map[string]any{"type": "string", "description": "Short title"},
				"kind":    map[string]any{"type": "string", "enum": []string{"unfinished", "upgrade", "bug", "idea"}, "description": "idea: a new feature, integration or flow"},
				"detail":  map[string]any{"type": "string", "description": "Where (file:line), why, what done looks like"},
				"scanned": map[string]any{"type": "string", "description": "The areas you looked at, briefly"},
			}, "item", "title", "kind")},
			Tool{Name: "burn_none", Description: "Burn worker: nothing worth doing found after looking thoroughly; ends your piece.", Schema: obj(map[string]any{
				"item":    item,
				"reason":  map[string]any{"type": "string"},
				"scanned": map[string]any{"type": "string", "description": "The areas you looked at, briefly"},
			}, "item", "reason")},
			Tool{Name: "burn_list", Description: "Burn: list this Burn's pieces: the open ones in full, all closed ones (done/skipped/failed, to avoid doing one again), or every area looked at.", Schema: obj(map[string]any{
				"what": map[string]any{"type": "string", "enum": []string{"open", "closed", "scanned"}},
			}, "what"), ReadOnly: true},
			Tool{Name: "burn_fail", Description: "Burn: report the current piece could not be done.", Schema: obj(map[string]any{"item": item, "reason": map[string]any{"type": "string"}}, "item", "reason")},
		)
	}
	if t.sendFile != nil {
		list = append(list, Tool{Name: "send_file", Description: "Send a file (a screenshot, chart, log…) into the current Discord/Telegram chat at once; images show inline. It must be inside the project's working folder (copy it there first if needed).",
			Schema: obj(map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path (relative to the working folder, or absolute inside it)"},
				"caption": map[string]any{"type": "string", "description": "Short caption for the file (optional)"},
			}, "path")})
	}
	if t.delegate != nil {
		list = append(list, Tool{Name: "delegate", Description: "Hand part of the work to another agent: it works in the background while you answer the person; you are called again when it is done. Only when needed. @Name in a reply hands over nothing.",
			Schema: obj(map[string]any{
				"agent": map[string]any{"type": "string", "description": "Name of a project agent"},
				"task":  map[string]any{"type": "string", "description": "The work to do, clear enough to do without asking back"},
			}, "agent", "task")})
	}
	if t.wf != nil {
		str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
		brief := map[string]any{"type": "object", "description": "The brief, by the sections the workflow asks for: outcome, question, context, constraints (verified), current_option (open to challenge), tried (and why dropped), files, done_when, must_not. Not how to change each file.",
			"additionalProperties": map[string]any{"type": "string"}}
		list = append(list,
			Tool{Name: "workflow_delegate", Description: "Workflow: give a role its first piece of work; it starts when your turn ends, and you are called again once all roles just given work are done. Pass agent when the role has none assigned.",
				Schema: obj(map[string]any{"role": str("Role key"), "agent": str("Name of the agent taking the role (empty = the agent assigned to the role)"), "brief": brief}, "role", "brief")},
			Tool{Name: "workflow_send", Description: "Workflow: send more to a role already given work (the role keeps its thread and remembers what it did). Each send is one round, and rounds are limited.",
				Schema: obj(map[string]any{"role": str("Role key"), "message": str("What to send, e.g. another role's argument it should answer")}, "role", "message")},
			Tool{Name: "workflow_ask", Description: "Workflow: ask an analyze-only role and WAIT for its answer this turn (up to wait_seconds), for short questions. On timeout it carries on in the background and you are called again.",
				Schema: obj(map[string]any{"role": str("Role key (access analyze)"), "question": str("The question, with the context needed"),
					"wait_seconds": map[string]any{"type": "integer", "description": "Max seconds to wait (10–120, default 60)"}}, "role", "question")},
			Tool{Name: "workflow_vote", Description: "Workflow: put a question to a vote among the workflow's roles (in parallel, analyze only). Office counts the votes by quorum/veto and calls you again with the result.",
				Schema: obj(map[string]any{"question": str("What to vote on, with enough context"),
					"agents": map[string]any{"type": "object", "description": "Assign agents to voting roles that have none: {\"role\": \"agent name\"}", "additionalProperties": map[string]any{"type": "string"}}}, "question")},
			Tool{Name: "workflow_gate", Description: "Workflow: open a gate. approve: a card for the person (note = what to approve), then the turn ends to wait. check: runs a check command (command, if the gate has none), in role's worktree if given.",
				Schema: obj(map[string]any{"gate": str("Gate key"), "note": str("approve: what the person needs to approve"), "command": str("check: check command"), "role": str("check: run in this role's worktree")}, "gate")},
			Tool{Name: "workflow_done", Description: "Workflow: finish when done (and every required gate passed). summary is the workflow's RESULT, the only answer the caller sees (Markdown, complete, readable on its own).",
				Schema: obj(map[string]any{"summary": str("Summary of the result"),
					"outputs":  map[string]any{"type": "object", "description": "Outputs by key, when the workflow declares outputs (required for required keys)", "additionalProperties": map[string]any{"type": "string"}},
					"overrule": str("When a role still OBJECTS and you keep your course: why (the caller sees both the objection and the reason)")}, "summary")},
		)
	}
	return list
}

// ToolsFor lists the tools an agent at level may use (proposals need
// "propose"; below it run_command runs only its safe commands).
func (t *Toolbox) ToolsFor(sc Scope) []Tool {
	var out []Tool
	for _, x := range t.Tools() {
		if (x.Name == "propose_action" || x.Name == "propose_automation" || x.Name == "send_to_chat") && !perm.AtLeast(sc.Level, perm.Propose) {
			continue
		}
		if x.Name == "send_to_chat" && sc.ConversationID == "" {
			continue
		}
		if strings.HasPrefix(x.Name, "burn_") && !t.burnChat(sc) {
			continue // a Burn's conversation only
		}
		if x.Name == "send_file" && !t.botChat(sc) {
			continue // only a bot's chat has somewhere to post it
		}
		if sc.Office && x.Name == "delegate" {
			continue // the assistant hands work to a project's chat instead
		}
		if t.wf != nil && (x.Name == "delegate" || strings.HasPrefix(x.Name, "workflow_")) {
			coord, inRun := t.wf.WorkflowScope(sc)
			if x.Name == "delegate" && inRun {
				continue // a workflow gives work through its own tools
			}
			if strings.HasPrefix(x.Name, "workflow_") && !coord {
				continue // only its coordinator
			}
		}
		if sc.AnswerOnly && proposes(x.Name) {
			continue
		}
		out = append(out, x)
	}
	if sc.Office { // the office assistant (ADR-046)
		for _, x := range t.officeTools() {
			if !sc.AnswerOnly || !proposes(x.Name) {
				out = append(out, x)
			}
		}
	}
	return out
}

// proposes: a tool that proposes a change or runs something (none for an
// assistant that only answers).
func proposes(name string) bool {
	return strings.HasPrefix(name, "propose") || name == "run_automation" || name == "send_to_chat"
}

// Has reports whether name is one of the tools.
func (t *Toolbox) Has(sc Scope, name string) bool {
	for _, x := range t.ToolsFor(sc) {
		if x.Name == name {
			return true
		}
	}
	return false
}

// Call runs a tool for a project and returns text (and whether it failed).
func (t *Toolbox) Call(ctx context.Context, sc Scope, name string, raw json.RawMessage) (string, bool) {
	projectID := sc.ProjectID
	var in struct {
		Project  string          `json:"project"`
		Goal     string          `json:"goal"`
		Days     int             `json:"days"`
		By       string          `json:"by"`
		Status   string          `json:"status"`
		Limit    int             `json:"limit"`
		Name     string          `json:"name"`
		Service  string          `json:"service"`
		Lines    int             `json:"lines"`
		Action   string          `json:"action"`
		Target   string          `json:"target"`
		Reason   string          `json:"reason"`
		Message  string          `json:"message"`
		Files    []string        `json:"files"`
		Path     string          `json:"path"`
		Query    string          `json:"query"`
		Title    string          `json:"title"`
		Kind     string          `json:"kind"`
		Detail   string          `json:"detail"`
		Item     string          `json:"item"`
		What     string          `json:"what"`
		Scanned  string          `json:"scanned"`
		Summary  string          `json:"summary"`
		Author   string          `json:"author"`
		Caption  string          `json:"caption"`
		Branch   string          `json:"branch"`
		Command  string          `json:"command"`
		Agent    string          `json:"agent"`
		Task     string          `json:"task"`
		URL      string          `json:"url"`
		Resource string          `json:"resource"`
		Op       string          `json:"op"`
		ID       string          `json:"id"`
		Patch    json.RawMessage `json:"patch"`
		Note     string          `json:"note"`
		Chat     string          `json:"chat"`
		Text     string          `json:"text"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "Tham số không hợp lệ: " + err.Error(), true
		}
	}
	if sc.Office { // across projects: the one named, if any
		if out, isErr, ok := t.officeCall(ctx, sc, name, raw); ok {
			return out, isErr
		}
		if in.Project != "" {
			p, err := t.findProject(ctx, in.Project)
			if err != nil {
				return err.Error(), true
			}
			projectID, sc.ProjectID = p.ID, p.ID
		}
	} else if in.Project != "" && in.Project != projectID {
		return "Bạn chỉ làm việc trong project của mình", true
	}
	if in.Lines <= 0 {
		in.Lines = 200
	}
	in.Lines = min(in.Lines, 1000)
	var (
		out string
		err error
	)
	switch name {
	case "ops_overview":
		out, err = t.overview(ctx, projectID)
	case "process_logs":
		out, err = t.processLogs(ctx, projectID, in.Name, in.Lines)
	case "container_logs":
		out, err = t.containerLogs(ctx, projectID, in.Service, in.Lines)
	case "monitor_detail":
		out, err = t.monitorDetail(ctx, projectID, in.Name)
	case "git_status", "git_diff", "git_log":
		out, err = t.gitRead(ctx, projectID, sc.Dir, name, in.Files, in.Lines)
	case "run_command":
		if t.actions == nil {
			return "Office không chạy lệnh được lúc này", true
		}
		if !perm.AtLeast(sc.Level, perm.Propose) {
			// read only: checks and reads run, nothing is proposed
			args, err := perm.SplitCommand(in.Command)
			if _, safe := perm.MatchCommand(sc.Access.Safe, args); err != nil || !safe {
				return "Ở mức " + perm.Label(sc.Level) + " bạn chỉ được chạy lệnh kiểm tra an toàn: " + strings.Join(sc.Access.Safe, ", "), true
			}
		}
		var a storage.Action
		if a, err = t.actions.Propose(ctx, sc, "run_command", in.Command, in.Reason); err == nil {
			switch a.Status {
			case "done":
				out = "$ " + a.Target + "\n" + a.Detail
			case "failed":
				return "$ " + a.Target + "\nLỗi: " + a.Detail, true
			default:
				out = fmt.Sprintf("Command %q is not on the list you may run yourself; created proposal %s, waiting for the person's approval. Nothing has run yet; tell the person.", a.Target, a.ID)
			}
		}
	case "describe", "list", "get", "propose_change":
		if t.config == nil || t.actions == nil {
			return "Không có công cụ cài đặt ở đây", true
		}
		switch name {
		case "describe":
			out, err = t.config.DescribeConfig(in.Resource)
		case "list":
			out, err = t.config.ListConfig(ctx, projectID, in.Resource)
		case "get":
			out, err = t.config.GetConfig(ctx, projectID, in.Resource, in.ID)
		default:
			if !sc.Office && !perm.AtLeast(sc.Level, perm.Propose) {
				return "Bạn không có quyền đề xuất đổi cài đặt (gói hiện tại: " + perm.Label(sc.Level) + ")", true
			}
			var a storage.Action
			a, err = t.actions.Propose(ctx, sc, "config_change", "", in.Reason, storage.ActionArgs{Change: &storage.ConfigChange{Resource: in.Resource, Op: in.Op, ID: in.ID, Patch: in.Patch}})
			if err == nil {
				out = "Created approval card: " + a.Target + ". Office changes it only once the person approves the card; tell them briefly what you proposed."
			}
		}
	case "read_link":
		out, err = t.readLink(ctx, sc, in.URL)
	case "search_history":
		out, err = t.searchHistory(ctx, sc, in.Query, in.Days, in.Author)
	case "burn_add", "burn_pick", "burn_skip", "burn_done", "burn_fail", "burn_list", "burn_claim", "burn_none":
		if t.burn == nil || !t.burnChat(sc) {
			return "Các công cụ burn_* chỉ dùng trong hội thoại Burn", true
		}
		out, err = t.burn(ctx, sc, name, BurnInput{Title: in.Title, Kind: in.Kind, Detail: in.Detail, Item: in.Item, Summary: in.Summary, Reason: in.Reason, What: in.What, Scanned: in.Scanned})
	case "send_file":
		if t.sendFile == nil || !t.botChat(sc) {
			return "send_file chỉ dùng trong cuộc chat của bot Discord/Telegram", true
		}
		out, err = t.sendFile(ctx, sc, in.Path, in.Caption)
	case "workflow_delegate", "workflow_send", "workflow_ask", "workflow_vote", "workflow_gate", "workflow_done":
		if t.wf == nil {
			return "Không có quy trình ở đây", true
		}
		out, err = t.wf.WorkflowCall(ctx, sc, name, raw)
	case "delegate":
		if t.delegate == nil {
			return "Không có công cụ giao việc ở đây", true
		}
		out, err = t.delegate(ctx, sc, in.Agent, in.Task)
	case "propose_automation":
		if t.actions == nil || !perm.AtLeast(sc.Level, perm.Propose) {
			return "Bạn không có quyền đề xuất tự động hóa (gói hiện tại: " + perm.Label(sc.Level) + ")", true
		}
		var id struct {
			AutomationID string `json:"automation_id"`
		}
		_ = json.Unmarshal(raw, &id)
		kind, target := "create_automation", in.Name
		if id.AutomationID != "" {
			kind, target = "update_automation", id.AutomationID
		}
		var a storage.Action
		if a, err = t.actions.Propose(ctx, sc, kind, target, in.Reason, storage.ActionArgs{Automation: raw}); err == nil {
			out = fmt.Sprintf("Created proposal %q (id %s), waiting for the person's approval. Nothing runs yet; summarize the script/schedule for the person and remind them to approve it.", a.Target, a.ID)
		}
	case "send_to_chat":
		if t.actions == nil || !perm.AtLeast(sc.Level, perm.Propose) {
			return "Bạn không có quyền gửi tin sang chat khác (gói hiện tại: " + perm.Label(sc.Level) + ")", true
		}
		var a storage.Action
		if a, err = t.actions.Propose(ctx, sc, "send_message", in.Chat, in.Reason, storage.ActionArgs{Message: in.Text}); err == nil {
			switch a.Status {
			case "done":
				out = fmt.Sprintf("Sent the message to chat %q under the person's name; the agent there will answer in that chat.", a.Target)
			case "failed":
				return "Gửi vào chat " + a.Target + " lỗi: " + a.Detail, true
			default:
				out = fmt.Sprintf("Created a proposal to send the message to chat %q (id %s), waiting for the person's approval. Nothing sent yet; tell the person.", a.Target, a.ID)
			}
		}
	case "remember":
		if t.actions == nil {
			return "Không ghi nhớ được ở đây", true
		}
		var a storage.Action
		if a, err = t.actions.Propose(ctx, sc, "remember", in.Note, in.Reason); err == nil {
			if a.Status == "done" {
				out = "Written to memory notes: " + a.Target
			} else {
				out = "Proposed the memory note (id " + a.ID + "), waiting for the person's approval."
			}
		}
	case "propose_action":
		if t.actions == nil || !perm.AtLeast(sc.Level, perm.Propose) {
			return "Bạn không có quyền đề xuất thao tác (gói hiện tại: " + perm.Label(sc.Level) + ")", true
		}
		var a storage.Action
		if a, err = t.actions.Propose(ctx, sc, in.Action, in.Target, in.Reason, storage.ActionArgs{Message: in.Message, Files: in.Files, Branch: in.Branch}); err == nil {
			switch a.Status {
			case "done":
				out = fmt.Sprintf("Did %q for %s on your own, as your permissions allow: %s. Use process_logs/container_logs to see the result.", actions.Kinds[a.Kind], a.Target, a.Detail)
			case "failed":
				out = fmt.Sprintf("Did %q for %s on your own, but it failed: %s", actions.Kinds[a.Kind], a.Target, a.Detail)
			default:
				out = fmt.Sprintf("Created proposal %q for %s (id %s), waiting for the person's approval. Nothing has been done yet; tell the person it needs their approval.", actions.Kinds[a.Kind], a.Target, a.ID)
			}
		}
	default:
		return "Công cụ không tồn tại: " + name, true
	}
	if err != nil {
		return "Lỗi: " + err.Error(), true
	}
	return out, false
}

func when(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Local().Format("02/01 15:04:05")
}

func (t *Toolbox) overview(ctx context.Context, projectID string) (string, error) {
	var b strings.Builder
	procs, err := t.store.Processes().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	b.WriteString("## Processes\n")
	if len(procs) == 0 {
		b.WriteString("(none)\n")
	}
	for _, p := range procs {
		line := fmt.Sprintf("- %s [%s] `%s`", p.Name, p.Kind, p.Command)
		if t.ops != nil {
			st := t.ops.State(p.ID)
			line += " — " + st.Status
			if st.ExitCode != nil {
				line += fmt.Sprintf(", exit code %d", *st.ExitCode)
			}
			if st.Port > 0 {
				line += fmt.Sprintf(", port %d", st.Port)
			}
			if st.FinishedAt != nil && st.Status != "running" {
				line += ", ended " + when(st.FinishedAt)
			}
		}
		b.WriteString(line + "\n")
	}
	if t.ops != nil {
		if v, err := t.ops.Compose(ctx, projectID, "", false); err == nil && len(v.Files) > 0 {
			fmt.Fprintf(&b, "\n## Docker compose (%s)\n", v.File)
			if v.Docker.Error != "" {
				b.WriteString("Docker: " + v.Docker.Error + "\n")
			}
			for _, s := range v.Services {
				if s.Container == nil {
					fmt.Fprintf(&b, "- %s — no container yet\n", s.Name)
					continue
				}
				fmt.Fprintf(&b, "- %s — %s (%s)\n", s.Name, s.Container.State, s.Container.Status)
			}
		}
	}
	mons, err := t.store.Monitors().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	b.WriteString("\n## Monitors\n")
	if len(mons) == 0 {
		b.WriteString("(none)\n")
	}
	for _, m := range mons {
		status := m.Status
		if !m.Enabled {
			status = "paused"
		}
		fmt.Fprintf(&b, "- %s [%s %s] — %s: %s (checked %s)\n", m.Name, m.Type, m.Target, status, m.LastMessage, when(m.LastCheckedAt))
	}
	evs, err := t.store.Monitors().Events(ctx, projectID, 10)
	if err == nil && len(evs) > 0 {
		names := map[string]string{}
		for _, m := range mons {
			names[m.ID] = m.Name
		}
		b.WriteString("\n## Recent events\n")
		for _, e := range evs {
			fmt.Fprintf(&b, "- %s %s %s: %s\n", e.At.Local().Format("02/01 15:04"), strings.ToUpper(e.Kind), names[e.MonitorID], e.Message)
		}
	}
	return b.String(), nil
}

func (t *Toolbox) processLogs(ctx context.Context, projectID, name string, lines int) (string, error) {
	if t.ops == nil {
		return "", errors.New("office không quản lý tiến trình")
	}
	procs, err := t.store.Processes().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, p := range procs {
		if strings.EqualFold(p.Name, name) || p.ID == name {
			st := t.ops.State(p.ID)
			head := fmt.Sprintf("Process %s: `%s` (folder %s) — %s", p.Name, p.Command, p.Cwd, st.Status)
			if st.ExitCode != nil {
				head += fmt.Sprintf(", exit code %d", *st.ExitCode)
			}
			tail := t.ops.Tail(p.ID, lines)
			if strings.TrimSpace(tail) == "" {
				tail = "(no log yet in this office session)"
			}
			return head + "\n\n```\n" + tail + "```", nil
		}
	}
	return "", fmt.Errorf("không có tiến trình %q; xem ops_overview", name)
}

func (t *Toolbox) containerLogs(ctx context.Context, projectID, service string, lines int) (string, error) {
	if t.ops == nil {
		return "", errors.New("office không quản lý container")
	}
	c, err := t.ops.ServiceContainer(ctx, projectID, "", service)
	if err != nil {
		return "", err
	}
	head := "Service " + service + ": no container yet"
	if c != nil {
		head = fmt.Sprintf("Service %s: %s (%s), image %s", service, c.State, c.Status, c.Image)
	}
	tail, err := t.ops.ComposeTail(ctx, projectID, "", service, lines)
	if err != nil {
		return head + "\n(could not read the log: " + err.Error() + ")", nil
	}
	return head + "\n\n```\n" + tail + "```", nil
}

func (t *Toolbox) monitorDetail(ctx context.Context, projectID, name string) (string, error) {
	mons, err := t.store.Monitors().List(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, m := range mons {
		if !strings.EqualFold(m.Name, name) && m.ID != name {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Monitor %s [%s] target %s, every %ds — %s: %s\n", m.Name, m.Type, m.Target, m.IntervalS, m.Status, m.LastMessage)
		if checks, err := t.store.Monitors().Checks(ctx, m.ID, time.Now().Add(-6*time.Hour)); err == nil && len(checks) > 0 {
			b.WriteString("\nRecent checks:\n")
			for _, c := range checks[max(0, len(checks)-20):] {
				ok := "OK"
				if !c.OK {
					ok = "FAIL"
				}
				fmt.Fprintf(&b, "- %s %s %dms %s\n", c.At.Local().Format("15:04:05"), ok, c.LatencyMS, c.Message)
			}
		}
		if evs, err := t.store.Monitors().Events(ctx, projectID, 50); err == nil {
			n := 0
			for _, e := range evs {
				if e.MonitorID != m.ID || n >= 5 {
					continue
				}
				n++
				fmt.Fprintf(&b, "\nEvent %s %s: %s\n", e.At.Local().Format("02/01 15:04"), strings.ToUpper(e.Kind), e.Message)
				if e.AnalysisStatus == "done" && e.Analysis != "" {
					fmt.Fprintf(&b, "Earlier analysis:\n%s\n", e.Analysis)
				}
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("không có giám sát %q; xem ops_overview", name)
}

// gitRead reads git in the project folder, or in the run's worktree (dir).
func (t *Toolbox) gitRead(ctx context.Context, projectID, dir, name string, files []string, lines int) (string, error) {
	p, err := t.store.Repos().Get(ctx, projectID)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", errors.New("project không gắn thư mục")
	}
	if dir != "" {
		p.Path = dir
	}
	switch name {
	case "git_status":
		st, err := gitops.ReadStatus(ctx, p.Path)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Branch %s", st.Branch)
		if st.Upstream != "" {
			fmt.Fprintf(&b, " (tracking %s, ahead %d, behind %d commits)", st.Upstream, st.Ahead, st.Behind)
		}
		fmt.Fprintf(&b, "\n%d changed files:\n", len(st.Changes))
		for _, c := range st.Changes {
			fmt.Fprintf(&b, "- %s %s\n", c.Status, c.Path)
		}
		return b.String(), nil
	case "git_diff":
		return gitops.Diff(ctx, p.Path, files, 60000)
	default:
		return gitops.Log(ctx, p.Path, min(max(lines, 10), 50))
	}
}

// botChat: the run is a bot's chat (Discord/Telegram), where send_file posts.
func (t *Toolbox) botChat(sc Scope) bool {
	if sc.ConversationID == "" {
		return false
	}
	c, err := t.store.Chat().GetConversation(context.Background(), sc.ConversationID)
	return err == nil && c.Purpose == "channel"
}

// searchHistory finds what was said in the chats (ADR-086): the project's,
// or every project's for the office assistant — never someone else's private
// chat with the assistant.
func (t *Toolbox) searchHistory(ctx context.Context, sc Scope, query string, days int, author string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", errors.New("hãy ghi từ khóa cần tìm")
	}
	if days <= 0 {
		days = 30
	}
	f := storage.SearchFilter{Since: time.Now().UTC().AddDate(0, 0, -min(days, 365)), Author: strings.TrimSpace(author), Limit: 10}
	assistant := ""
	if t.assistant != nil {
		assistant = t.assistant(ctx)
	}
	if sc.Office {
		repos, err := t.store.Repos().List(ctx)
		if err != nil {
			return "", err
		}
		for _, r := range repos {
			f.ProjectIDs = append(f.ProjectIDs, r.ID)
		}
	} else {
		f.ProjectIDs = []string{sc.ProjectID}
	}
	if assistant != "" { // the assistant's chats are each person's own
		f.HideOthersIn = assistant
		if c, err := t.store.Chat().GetConversation(ctx, sc.ConversationID); err == nil {
			f.Me = c.CreatedBy
		}
	}
	hits, err := t.store.Chat().SearchMessages(ctx, query, f)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No match for \"%s\" in the last %d days.", query, days), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d results for \"%s\" (newest, last %d days):\n", len(hits), query, days)
	for _, h := range hits {
		who := h.Author
		if who == "" {
			who = h.Role
		}
		fmt.Fprintf(&b, "- %s · %s · %s: %s\n  /projects/%s?tab=chat&c=%s&m=%s\n", h.CreatedAt.Local().Format("02/01 15:04"), cmp.Or(h.Title, "(untitled)"), who,
			strings.ReplaceAll(h.Snippet, "\n", " "), h.ProjectID, h.ConversationID, h.MessageID)
	}
	return b.String(), nil
}

// burnChat: the run is a Burn's conversation or one of its pieces' work
// chats, where the burn_* tools work.
func (t *Toolbox) burnChat(sc Scope) bool {
	if sc.ConversationID == "" {
		return false
	}
	c, err := t.store.Chat().GetConversation(context.Background(), sc.ConversationID)
	return err == nil && (c.Purpose == "burn" || c.Purpose == "burn_work")
}
