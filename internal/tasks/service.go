// Package tasks hands a goal to a project's whole org model and runs it by the
// model's governance: solo (one agent), hierarchy (lead plans, members work,
// lead synthesises) or council (planner proposes, peers vote by quorum with
// veto, workers execute, auditor reviews and may block code changes).
package tasks

import (
	"bitbucket.org/senprints/agent-office/internal/attach"
	"bitbucket.org/senprints/agent-office/internal/automation"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

var (
	ErrNoModel    = errors.New("project chưa có mô hình tổ chức")
	ErrNoLead     = errors.New("mô hình chưa có agent lead")
	ErrBusy       = errors.New("project đang chạy một việc khác")
	ErrTaskBudget = errors.New("đã dùng hết ngân sách của việc này")
)

// Event streams task progress to the dashboard.
type Event struct {
	Seq    int               `json:"seq"`
	Type   string            `json:"type"` // step | text | tool | step_done | patch | status | done
	StepID string            `json:"step_id,omitempty"`
	Step   *StepDTO          `json:"step,omitempty"`
	Text   string            `json:"text,omitempty"`
	Tool   *storage.ToolCall `json:"tool,omitempty"`
	Patch  *chat.PatchDTO    `json:"patch,omitempty"`
	Task   *TaskDTO          `json:"task,omitempty"`
}

// Live is a running task's event log (replayable).
type Live struct {
	mu     sync.Mutex
	events []Event
	done   bool
	wake   chan struct{}
	cancel context.CancelFunc
}

func (l *Live) emit(e Event) {
	l.mu.Lock()
	e.Seq = len(l.events)
	l.events = append(l.events, e)
	if e.Type == "done" {
		l.done = true
	}
	close(l.wake)
	l.wake = make(chan struct{})
	l.mu.Unlock()
}

// Since returns events from seq, whether the task ended, and a wake channel.
func (l *Live) Since(seq int) ([]Event, bool, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Event
	if seq < len(l.events) {
		out = append(out, l.events[seq:]...)
	}
	return out, l.done, l.wake
}

// Cancel stops the task.
func (l *Live) Cancel() { l.cancel() }

// Service runs tasks.
type Service struct {
	store  storage.Store
	engine *chat.Engine

	mu   sync.Mutex
	live map[string]*Live  // task id
	busy map[string]string // project id → running task id
}

// Running counts tasks in progress.
func (s *Service) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.busy)
}

// New builds a Service.
func New(store storage.Store, engine *chat.Engine) *Service {
	return &Service{store: store, engine: engine, live: map[string]*Live{}, busy: map[string]string{}}
}

// Live returns a running (or recently finished) task's stream.
func (s *Service) Live(taskID string) (*Live, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.live[taskID]
	return l, ok
}

// Start creates a task and runs it in the background.
func (s *Service) Start(ctx context.Context, projectID, goal string, budgetUSD float64, attachmentIDs []string) (storage.Task, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return storage.Task{}, errors.New("hãy mô tả việc cần làm")
	}
	project, err := s.store.Repos().Get(ctx, projectID)
	if err != nil {
		return storage.Task{}, err
	}
	// "/skill ..." gives every agent the skill's instructions; the task keeps
	// what the person typed
	home, _ := os.UserHomeDir()
	prompt, _, err := automation.ExpandSkillCall(home, project.Path, goal)
	if err != nil {
		return storage.Task{}, err
	}
	files, err := s.engine.Attachments().Resolve(projectID, attachmentIDs)
	if err != nil {
		return storage.Task{}, err
	}
	model, err := s.store.OrgModels().GetForRepo(ctx, projectID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Task{}, ErrNoModel
	}
	if err != nil {
		return storage.Task{}, err
	}
	agents, err := s.store.Agents().List(ctx, model.ID)
	if err != nil {
		return storage.Task{}, err
	}
	mode := model.Governance.Mode
	if mode == "" {
		mode = map[string]string{storage.KindSolo: "single", storage.KindCouncil: "council"}[model.Kind]
		if mode == "" {
			mode = "hierarchy"
		}
	}
	s.mu.Lock()
	if _, busy := s.busy[projectID]; busy {
		s.mu.Unlock()
		return storage.Task{}, ErrBusy
	}
	s.busy[projectID] = "starting"
	s.mu.Unlock()

	task, err := s.store.Tasks().Create(ctx, storage.Task{
		ProjectID: projectID, Title: truncate(strings.Join(strings.Fields(goal), " "), 90), Goal: goal, Mode: mode,
		Status: "running", BudgetUSD: budgetUSD, Attachments: attach.Refs(files), CreatedBy: actor.From(ctx),
	})
	if err != nil {
		s.mu.Lock()
		delete(s.busy, projectID)
		s.mu.Unlock()
		return task, err
	}
	runCtx, cancel := context.WithTimeout(actor.With(context.Background(), actor.From(ctx)), 45*time.Minute)
	runCtx = chat.WithTask(runCtx, task.ID) // proposals made by its agents attach to the task
	live := &Live{wake: make(chan struct{}), cancel: cancel}
	s.mu.Lock()
	s.live[task.ID], s.busy[projectID] = live, task.ID
	s.mu.Unlock()

	r := &run{svc: s, ctx: runCtx, task: task, goal: prompt, files: files, project: project, model: model, agents: agents, live: live}
	go r.execute()
	return task, nil
}

// run is one task execution.
type run struct {
	svc     *Service
	ctx     context.Context
	task    storage.Task
	goal    string        // the goal as agents see it (skill call expanded)
	files   []attach.File // attached files, given to every step
	project storage.Repo
	model   storage.OrgModel
	agents  []storage.Agent
	live    *Live

	mu      sync.Mutex
	seq     int
	cost    float64
	patches []storage.Patch
}

func (r *run) execute() {
	defer r.live.cancel()
	defer func() {
		r.svc.mu.Lock()
		delete(r.svc.busy, r.project.ID)
		r.svc.mu.Unlock()
		time.AfterFunc(15*time.Minute, func() {
			r.svc.mu.Lock()
			delete(r.svc.live, r.task.ID)
			r.svc.mu.Unlock()
		})
	}()
	var (
		result, status, detail string
		err                    error
	)
	switch r.task.Mode {
	case "single":
		result, err = r.single()
	case "council":
		result, status, detail, err = r.council()
	default:
		result, err = r.hierarchy()
	}
	switch {
	case errors.Is(r.ctx.Err(), context.Canceled):
		status, detail = "cancelled", "Đã dừng"
	case err != nil:
		status, detail = "failed", err.Error()
	case status == "":
		status = "done"
	}
	now := time.Now().UTC()
	r.mu.Lock()
	r.task.Status, r.task.Result, r.task.Detail, r.task.CostUSD, r.task.FinishedAt = status, result, detail, r.cost, &now
	r.mu.Unlock()
	_ = r.svc.store.Tasks().Update(context.Background(), r.task)
	dto := toTaskDTO(r.task)
	r.live.emit(Event{Type: "done", Task: &dto})
}

func (r *run) status(text string) { r.live.emit(Event{Type: "status", Text: text}) }

func (r *run) leads() []storage.Agent {
	var out []storage.Agent
	for _, a := range r.agents {
		if a.Tier == storage.TierLead {
			out = append(out, a)
		}
	}
	return out
}

func (r *run) members() []storage.Agent {
	var out []storage.Agent
	for _, a := range r.agents {
		if a.Tier != storage.TierLead {
			out = append(out, a)
		}
	}
	return out
}

func (r *run) byKey(key string) (storage.Agent, bool) {
	for _, a := range r.agents {
		if a.Key == strings.TrimSpace(key) {
			return a, true
		}
	}
	return storage.Agent{}, false
}

// step runs one agent turn and records it.
func (r *run) step(phase string, agent storage.Agent, instruction, prompt string) (storage.TaskStep, error) {
	r.mu.Lock()
	if r.task.BudgetUSD > 0 && r.cost >= r.task.BudgetUSD {
		r.mu.Unlock()
		return storage.TaskStep{}, ErrTaskBudget
	}
	r.seq++
	seq := r.seq
	r.mu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return storage.TaskStep{}, err
	}
	st, err := r.svc.store.Tasks().AddStep(r.ctx, storage.TaskStep{
		TaskID: r.task.ID, Seq: seq, Phase: phase, AgentID: agent.ID, AgentKey: agent.Key, AgentName: agent.Name,
		Instruction: instruction, Status: "running",
	})
	if err != nil {
		return st, err
	}
	sd := toStepDTO(st)
	r.live.emit(Event{Type: "step", StepID: st.ID, Step: &sd})
	res, runErr := r.svc.engine.Invoke(r.ctx, r.project, agent, prompt, r.files, "task", func(e chat.Event) {
		switch e.Type {
		case "text":
			r.live.emit(Event{Type: "text", StepID: st.ID, Text: e.Text})
		case "tool":
			r.live.emit(Event{Type: "tool", StepID: st.ID, Tool: e.Tool})
		}
	})
	now := time.Now().UTC()
	st.Output, st.Tools, st.RunID, st.CostUSD, st.FinishedAt = res.Text, res.Tools, res.RunID, res.CostUSD, &now
	st.Status = "done"
	if runErr != nil {
		st.Status, st.Error = "failed", runErr.Error()
	}
	if res.CostUSD != nil {
		r.mu.Lock()
		r.cost += *res.CostUSD
		r.mu.Unlock()
	}
	_ = r.svc.store.Tasks().UpdateStep(context.Background(), st)
	sd = toStepDTO(st)
	r.live.emit(Event{Type: "step_done", StepID: st.ID, Step: &sd})
	if runErr == nil && phase == "work" && chat.CanPropose(agent) && r.project.Path != "" {
		r.collectPatches(st)
	}
	return st, runErr
}

func (r *run) collectPatches(st storage.TaskStep) {
	for _, diff := range chat.ExtractPatches(st.Output) {
		p := storage.Patch{TaskID: r.task.ID, StepID: st.ID, Diff: diff}
		files, err := chat.PatchFiles(diff)
		p.Files = files
		if err != nil {
			p.Status, p.Detail = "failed", err.Error()
		} else if cerr := chat.CheckPatch(r.ctx, r.project.Path, diff); cerr != nil {
			p.Status, p.Detail = "failed", cerr.Error()
		}
		saved, err := r.svc.store.Chat().AddPatch(context.Background(), p)
		if err != nil {
			continue
		}
		r.mu.Lock()
		r.patches = append(r.patches, saved)
		r.mu.Unlock()
		pd := chat.PatchDTO{ID: saved.ID, Diff: saved.Diff, Files: saved.Files, Status: saved.Status, Detail: saved.Detail}
		r.live.emit(Event{Type: "patch", StepID: st.ID, Patch: &pd})
	}
}

// work runs assignments in parallel (at most 3 at a time).
func (r *run) work(assigner storage.Agent, goal string, assignments []Assignment) []storage.TaskStep {
	type item struct {
		i     int
		agent storage.Agent
		task  string
	}
	var items []item
	for i, a := range assignments {
		ag, ok := r.byKey(a.Agent)
		if !ok || ag.Tier == storage.TierLead || strings.TrimSpace(a.Task) == "" {
			r.status(fmt.Sprintf("Bỏ qua việc giao cho %q: không có agent phù hợp", a.Agent))
			continue
		}
		items = append(items, item{i, ag, a.Task})
		if len(items) == maxAssignments {
			break
		}
	}
	results := make([]storage.TaskStep, len(items))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for idx, it := range items {
		wg.Add(1)
		go func(idx int, it item) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			st, err := r.step("work", it.agent, it.task, workPrompt(goal, assigner.Name, it.task))
			if err != nil && st.ID == "" {
				st = storage.TaskStep{AgentKey: it.agent.Key, AgentName: it.agent.Name, Instruction: it.task, Status: "failed", Error: err.Error()}
			}
			results[idx] = st
		}(idx, it)
	}
	wg.Wait()
	return results
}

func (r *run) plan(planner storage.Agent, phase, feedback string) (Plan, storage.TaskStep, error) {
	st, err := r.step(phase, planner, "Lập kế hoạch", planPrompt(r.goal, r.members(), feedback))
	if err != nil {
		return Plan{}, st, err
	}
	var p Plan
	if perr := parseJSON(st.Output, &p); perr != nil {
		// No structured plan: treat the whole answer as the plan's analysis and answer.
		p = Plan{Analysis: stripJSON(st.Output), Answer: stripJSON(st.Output)}
	}
	st.Data = map[string]any{"analysis": p.Analysis, "assignments": p.Assignments, "answer": p.Answer}
	st.Output = stripJSON(st.Output)
	_ = r.svc.store.Tasks().UpdateStep(context.Background(), st)
	sd := toStepDTO(st)
	r.live.emit(Event{Type: "step_done", StepID: st.ID, Step: &sd})
	return p, st, nil
}

func (r *run) single() (string, error) {
	leads := r.leads()
	if len(leads) == 0 {
		return "", ErrNoLead
	}
	st, err := r.step("work", leads[0], r.task.Goal, r.goal)
	if err != nil {
		return "", err
	}
	return st.Output, nil
}

func (r *run) hierarchy() (string, error) {
	leads := r.leads()
	if len(leads) == 0 {
		return "", ErrNoLead
	}
	lead := leads[0]
	r.status(lead.Name + " đang lập kế hoạch")
	plan, _, err := r.plan(lead, "plan", "")
	if err != nil {
		return "", err
	}
	if len(plan.Assignments) == 0 {
		return firstNonEmpty(plan.Answer, plan.Analysis), nil
	}
	r.status(fmt.Sprintf("Đội đang làm %d việc", len(plan.Assignments)))
	work := r.work(lead, r.goal, plan.Assignments)
	r.status(lead.Name + " đang tổng hợp")
	st, err := r.step("synthesize", lead, "Tổng hợp kết quả", synthesizePrompt(r.goal, work, r.snapshotPatches(), ""))
	if err != nil {
		return outputsBlock(work), err
	}
	return st.Output, nil
}

// councilRoles picks planner, executor and auditor among the peer leads.
func (r *run) councilRoles() (planner, executor, auditor storage.Agent, err error) {
	leads := r.leads()
	if len(leads) < 2 {
		return planner, executor, auditor, errors.New("mô hình hội đồng cần ít nhất 2 lead")
	}
	byKey := map[string]storage.Agent{}
	for _, l := range leads {
		byKey[l.Key] = l
	}
	a, hasAuditor := byKey["auditor"]
	if !hasAuditor && len(r.model.Governance.Veto) > 0 {
		a, hasAuditor = byKey[r.model.Governance.Veto[0]]
	}
	if !hasAuditor {
		a = leads[len(leads)-1]
	}
	p, okP := byKey["planner"]
	e, okE := byKey["executor"]
	var rest []storage.Agent
	for _, l := range leads {
		if l.Key != a.Key {
			rest = append(rest, l)
		}
	}
	if !okP {
		p = rest[0]
	}
	if !okE {
		e = rest[len(rest)-1]
	}
	return p, e, a, nil
}

func (r *run) council() (result, status, detail string, err error) {
	planner, executor, auditor, err := r.councilRoles()
	if err != nil {
		return "", "", "", err
	}
	quorum := r.model.Governance.Quorum
	if quorum <= 0 {
		quorum = len(r.leads())/2 + 1
	}
	veto := map[string]bool{}
	for _, k := range r.model.Governance.Veto {
		veto[k] = true
	}
	voters := []storage.Agent{}
	for _, l := range r.leads() {
		if l.Key != planner.Key {
			voters = append(voters, l)
		}
	}
	var plan Plan
	feedback := ""
	for round := 1; round <= 2; round++ {
		phase := "plan"
		if round > 1 {
			phase = "revise"
		}
		r.status(planner.Name + " đang lập kế hoạch")
		plan, _, err = r.plan(planner, phase, feedback)
		if err != nil {
			return "", "", "", err
		}
		if len(plan.Assignments) == 0 && plan.Answer != "" {
			return plan.Answer, "", "", nil
		}
		r.status("Hội đồng đang biểu quyết")
		approvals, vetoed := 1, false // the planner backs its own plan
		var reasons []string
		for _, v := range voters {
			st, verr := r.step("vote", v, "Biểu quyết kế hoạch", votePrompt(r.goal, planner, plan, ""))
			if verr != nil {
				reasons = append(reasons, v.Name+": không bỏ phiếu được ("+verr.Error()+")")
				continue
			}
			var vote Vote
			if perr := parseJSON(st.Output, &vote); perr != nil {
				vote = Vote{Vote: "reject", Reason: "không đọc được phiếu"}
			}
			vote.Vote = strings.ToLower(strings.TrimSpace(vote.Vote))
			st.Data = map[string]any{"vote": vote.Vote, "reason": vote.Reason}
			st.Output = stripJSON(st.Output)
			_ = r.svc.store.Tasks().UpdateStep(context.Background(), st)
			sd := toStepDTO(st)
			r.live.emit(Event{Type: "step_done", StepID: st.ID, Step: &sd})
			if vote.Vote == "approve" {
				approvals++
			} else {
				reasons = append(reasons, v.Name+": "+vote.Reason)
				if veto[v.Key] {
					vetoed = true
				}
			}
		}
		if approvals >= quorum && !vetoed {
			r.status(fmt.Sprintf("Kế hoạch được thông qua (%d/%d)", approvals, len(r.leads())))
			break
		}
		feedback = strings.Join(reasons, "\n")
		if round == 2 {
			msg := fmt.Sprintf("Kế hoạch không được thông qua sau 2 vòng (%d/%d phiếu cần %d", approvals, len(r.leads()), quorum)
			if vetoed {
				msg += ", bị phủ quyết"
			}
			msg += ").\n\nLý do:\n" + feedback
			return msg, "rejected", "Không đạt đồng thuận", nil
		}
	}

	r.status(executor.Name + " đang giao việc cho worker")
	work := r.work(executor, r.goal, plan.Assignments)

	r.status(auditor.Name + " đang kiểm tra")
	verdictNote := ""
	if st, verr := r.step("review", auditor, "Kiểm tra kết quả", reviewPrompt(r.goal, work, r.snapshotPatches())); verr == nil {
		var v Verdict
		if perr := parseJSON(st.Output, &v); perr == nil {
			st.Data = map[string]any{"verdict": v.Verdict, "summary": v.Summary, "issues": v.Issues, "block_changes": v.BlockChanges}
			st.Output = stripJSON(st.Output)
			_ = r.svc.store.Tasks().UpdateStep(context.Background(), st)
			sd := toStepDTO(st)
			r.live.emit(Event{Type: "step_done", StepID: st.ID, Step: &sd})
			verdictNote = fmt.Sprintf("Giám sát đánh giá: %s. %s", v.Verdict, v.Summary)
			if v.BlockChanges && veto[auditor.Key] {
				r.vetoPatches(auditor.Name + " phủ quyết: " + v.Summary)
				verdictNote += " Mọi thay đổi code đã bị phủ quyết."
			}
		}
	}
	r.status(executor.Name + " đang tổng hợp")
	st, err := r.step("synthesize", executor, "Tổng hợp kết quả", synthesizePrompt(r.goal, work, r.snapshotPatches(), verdictNote))
	if err != nil {
		return outputsBlock(work), "", "", err
	}
	return st.Output, "", "", nil
}

func (r *run) snapshotPatches() []storage.Patch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]storage.Patch(nil), r.patches...)
}

func (r *run) vetoPatches(reason string) {
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.patches {
		if p.Status != "pending" {
			continue
		}
		if err := r.svc.store.Chat().DecidePatch(context.Background(), p.ID, "rejected", reason, "council", now); err == nil {
			r.patches[i].Status, r.patches[i].Detail = "rejected", reason
			pd := chat.PatchDTO{ID: p.ID, Diff: p.Diff, Files: p.Files, Status: "rejected", Detail: reason, DecidedBy: "council"}
			r.live.emit(Event{Type: "patch", StepID: p.StepID, Patch: &pd})
		}
	}
}
