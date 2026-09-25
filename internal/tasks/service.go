// Package tasks hands a goal to a project's whole org model and runs it by the
// model's governance: solo (one agent), hierarchy (lead plans, members work,
// lead synthesises) or council (planner proposes, peers vote by quorum with
// veto, workers execute, auditor reviews and may block code changes).
package tasks

import (
	"bitbucket.org/senprints/agent-office/internal/attach"
	"bitbucket.org/senprints/agent-office/internal/automation"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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
func (s *Service) Start(ctx context.Context, projectID, goal string, budgetUSD float64, attachmentIDs []string, permMode string) (storage.Task, error) {
	return s.start(ctx, projectID, goal, budgetUSD, attachmentIDs, permMode, "")
}

// Retry starts the task again with the same goal, files, budget and mode
// (mode "" keeps the old one). learn adds what went wrong last time, so the
// team does not repeat it.
func (s *Service) Retry(ctx context.Context, taskID string, learn bool, permMode, answer string) (storage.Task, error) {
	old, err := s.store.Tasks().Get(ctx, taskID)
	if err != nil {
		return storage.Task{}, err
	}
	if old.Status == "running" {
		return storage.Task{}, ErrBusy
	}
	ids := make([]string, 0, len(old.Attachments))
	for _, a := range old.Attachments {
		ids = append(ids, a.ID)
	}
	if permMode == "" {
		permMode = old.ModeLevel
	}
	lesson := ""
	if learn {
		var b strings.Builder
		fmt.Fprintf(&b, "Lần chạy trước của việc này không đạt (trạng thái: %s).", old.Status)
		if old.Detail != "" {
			fmt.Fprintf(&b, " Lý do: %s.", old.Detail)
		}
		if old.Result != "" {
			fmt.Fprintf(&b, "\nKết luận lần trước:\n%s", truncate(old.Result, 4000))
		}
		if patches, err := s.store.Tasks().ListPatches(ctx, old.ID); err == nil && len(patches) > 0 {
			b.WriteString("\nCác diff lần trước và vì sao không dùng được:")
			for _, p := range patches {
				if p.Status == "applied" {
					continue
				}
				fmt.Fprintf(&b, "\n- %s (%s): %s", strings.Join(p.Files, ", "), p.Status, truncate(p.Detail, 200))
			}
		}
		b.WriteString("\nHãy rút kinh nghiệm: tránh lặp lại các lỗi trên, chia việc gọn hơn và kiểm tra kỹ trước khi đưa diff.")
		lesson = b.String()
	}
	if answer = strings.TrimSpace(answer); answer != "" {
		lesson = strings.TrimSpace(lesson + fmt.Sprintf("\n\nLần trước đội hỏi người dùng: %s\nNgười dùng trả lời: %s\nLàm tiếp theo câu trả lời này.", old.Detail, answer))
	}
	return s.start(ctx, old.ProjectID, old.Goal, old.BudgetUSD, ids, permMode, lesson)
}

func (s *Service) start(ctx context.Context, projectID, goal string, budgetUSD float64, attachmentIDs []string, permMode, lesson string) (storage.Task, error) {
	if !perm.Valid(permMode) {
		permMode = perm.Propose
	}
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
		Status: "running", BudgetUSD: budgetUSD, ModeLevel: permMode, Attachments: attach.Refs(files), CreatedBy: actor.From(ctx),
	})
	if err != nil {
		s.mu.Lock()
		delete(s.busy, projectID)
		s.mu.Unlock()
		return task, err
	}
	runCtx, cancel := context.WithTimeout(actor.With(context.Background(), actor.From(ctx)), 45*time.Minute)
	runCtx = chat.WithTask(runCtx, task.ID, permMode) // proposals attach to the task; mode caps what agents do
	live := &Live{wake: make(chan struct{}), cancel: cancel}
	s.mu.Lock()
	s.live[task.ID], s.busy[projectID] = live, task.ID
	s.mu.Unlock()

	if lesson != "" {
		prompt += "\n\n" + lesson
	}
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
	auto    map[string]bool // patch ids to apply on their own once the task succeeds
	failed  string          // review verdict when the auditor judged the work not good enough
	ask     string          // question for the person; the task stops as needs_input
	jobs    map[int]*job    // plan jobs by number, with their latest step
}

// job is one plan assignment and the step that last worked on it.
type job struct {
	n     int
	a     Assignment
	agent storage.Agent
	step  storage.TaskStep
}

// level is what an agent may do in this task.
func (r *run) level(a storage.Agent) string { return r.access(a).Level }

func (r *run) access(a storage.Agent) perm.Access {
	return r.svc.engine.Access(r.ctx, r.project.ID, a, r.task.ModeLevel)
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
		status, detail = r.outcome()
	}
	if status == "done" {
		r.applyAuto()
	}
	now := time.Now().UTC()
	r.mu.Lock()
	r.task.Status, r.task.Result, r.task.Detail, r.task.CostUSD, r.task.FinishedAt = status, result, detail, r.cost, &now
	r.mu.Unlock()
	_ = r.svc.store.Tasks().Update(context.Background(), r.task)
	dto := toTaskDTO(r.task)
	r.live.emit(Event{Type: "done", Task: &dto})
}

// outcome says whether the work is usable: "done" only if the review did not
// fail and, when code changes were proposed, at least one is still usable.
func (r *run) outcome() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ask != "" {
		return "needs_input", r.ask
	}
	if r.failed != "" {
		return "failed", "Giám sát đánh giá chưa đạt: " + r.failed
	}
	if len(r.patches) == 0 {
		return "done", ""
	}
	usable, rejected, broken := 0, 0, 0
	for _, p := range r.patches {
		switch p.Status {
		case "pending", "applied":
			usable++
		case "rejected":
			rejected++
		default:
			broken++
		}
	}
	if usable == 0 {
		return "failed", fmt.Sprintf("Không có thay đổi code nào dùng được: %d bị từ chối, %d không áp được", rejected, broken)
	}
	return "done", ""
}

// applyAuto applies, once the task succeeded (and was not vetoed), the diffs
// whose agents' packages allow applying on their own; still-pending only, so
// a veto that rejected them wins.
func (r *run) applyAuto() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.auto))
	for id := range r.auto {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		ctx := actor.With(context.Background(), "auto:"+r.task.ID+" ("+perm.Label(perm.Edit)+")")
		if d, err := r.svc.engine.DecidePatch(ctx, id, true); err == nil {
			pd := d
			r.live.emit(Event{Type: "patch", Patch: &pd})
		}
	}
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
	return r.scopedStep(phase, agent, instruction, prompt, nil)
}

// scopedStep runs a step; for work steps, allowed limits the files its diffs may touch.
func (r *run) scopedStep(phase string, agent storage.Agent, instruction, prompt string, allowed []string) (storage.TaskStep, error) {
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
	if runErr == nil && phase == "work" && perm.AtLeast(r.level(agent), perm.Propose) && r.project.Path != "" {
		r.collectPatches(st, allowed, agent)
	}
	return st, runErr
}

func (r *run) collectPatches(st storage.TaskStep, allowed []string, agent storage.Agent) {
	policy := perm.LoadPolicy(r.ctx, r.svc.store, r.project.ID)
	autoApply := r.access(agent).Can(perm.CapApply)
	ok := map[string]bool{}
	for _, f := range allowed {
		ok[cleanPath(f)] = true
	}
	for _, diff := range chat.ExtractPatches(st.Output) {
		p := storage.Patch{TaskID: r.task.ID, StepID: st.ID, Diff: diff}
		files, err := chat.PatchFiles(diff)
		p.Files = files
		var outside []string
		for _, f := range files {
			if len(ok) > 0 && !ok[cleanPath(f)] {
				outside = append(outside, f)
			}
		}
		if err != nil {
			p.Status, p.Detail = "failed", err.Error()
		} else if len(outside) > 0 {
			p.Status, p.Detail = "failed", "sửa file ngoài phạm vi được giao: "+strings.Join(outside, ", ")
		} else if denied := policy.Denied(files); len(denied) > 0 {
			p.Status, p.Detail = "failed", "sửa file cấm của project: "+strings.Join(denied, ", ")
		} else if cerr := chat.CheckPatch(r.ctx, r.project.Path, diff); cerr != nil {
			p.Status, p.Detail = "failed", cerr.Error()
		}
		saved, err := r.svc.store.Chat().AddPatch(context.Background(), p)
		if err != nil {
			continue
		}
		r.mu.Lock()
		r.patches = append(r.patches, saved)
		if autoApply && saved.Status == "pending" {
			if r.auto == nil {
				r.auto = map[string]bool{}
			}
			r.auto[saved.ID] = true
		}
		r.mu.Unlock()
		pd := chat.PatchDTO{ID: saved.ID, Diff: saved.Diff, Files: saved.Files, Status: saved.Status, Detail: saved.Detail}
		r.live.emit(Event{Type: "patch", StepID: st.ID, Patch: &pd})
	}
}

// work runs the plan's jobs: independent ones in parallel (at most 3 at a
// time), a job after the jobs it depends on, each limited to its files and
// told the shared conventions and the results it builds on.
func (r *run) work(assigner storage.Agent, goal string, plan Plan) []storage.TaskStep {
	type item struct {
		n     int // 1-based job number
		agent storage.Agent
		a     Assignment
	}
	var items []item
	for i, a := range plan.Assignments {
		ag, ok := r.byKey(a.Agent)
		if !ok || ag.Tier == storage.TierLead || strings.TrimSpace(a.Task) == "" {
			r.status(fmt.Sprintf("Bỏ qua việc giao cho %q: không có agent phù hợp", a.Agent))
			continue
		}
		items = append(items, item{i + 1, ag, a})
		if len(items) == maxAssignments {
			break
		}
	}
	results := make([]storage.TaskStep, len(items))
	done := map[int]storage.TaskStep{}
	remaining := items
	for len(remaining) > 0 {
		// a wave: jobs whose dependencies are done (or unknown/skipped)
		var wave, later []item
		for _, it := range remaining {
			ready := true
			for _, d := range it.a.DependsOn {
				if _, ok := done[d]; !ok && slices.ContainsFunc(remaining, func(x item) bool { return x.n == d }) {
					ready = false
				}
			}
			if ready {
				wave = append(wave, it)
			} else {
				later = append(later, it)
			}
		}
		if len(wave) == 0 { // cycle: run the rest anyway
			wave, later = later, nil
		}
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, it := range wave {
			var before strings.Builder
			for _, d := range it.a.DependsOn {
				if st, ok := done[d]; ok {
					fmt.Fprintf(&before, "--- Việc %d (%s): %s\n%s\n", d, st.AgentName, st.Instruction, truncate(st.Output, 6000))
				}
			}
			wg.Add(1)
			go func(it item, before string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				st, err := r.scopedStep("work", it.agent, it.a.Task, workPrompt(goal, assigner.Name, it.a, plan.Conventions, before), it.a.Files)
				if err != nil && st.ID == "" {
					st = storage.TaskStep{AgentKey: it.agent.Key, AgentName: it.agent.Name, Instruction: it.a.Task, Status: "failed", Error: err.Error()}
				}
				mu.Lock()
				done[it.n] = st
				mu.Unlock()
				r.mu.Lock()
				if r.jobs == nil {
					r.jobs = map[int]*job{}
				}
				r.jobs[it.n] = &job{n: it.n, a: it.a, agent: it.agent, step: st}
				r.mu.Unlock()
			}(it, before.String())
		}
		wg.Wait()
		remaining = later
	}
	for i, it := range items {
		results[i] = done[it.n]
	}
	return results
}

func (r *run) plan(planner storage.Agent, phase, feedback string) (Plan, storage.TaskStep, error) {
	var (
		p  Plan
		st storage.TaskStep
	)
	canEdit := func(key string) bool {
		a, ok := r.byKey(key)
		return ok && perm.AtLeast(r.level(a), perm.Propose)
	}
	for attempt := 0; attempt < 2; attempt++ {
		var err error
		st, err = r.step(phase, planner, "Lập kế hoạch", planPrompt(r.goal, r.members(), feedback))
		if err != nil {
			return Plan{}, st, err
		}
		p = Plan{}
		if perr := parseJSON(st.Output, &p); perr != nil {
			// No structured plan: treat the whole answer as the plan's analysis and answer.
			p = Plan{Analysis: stripJSON(st.Output), Answer: stripJSON(st.Output)}
		}
		problems := validatePlan(p, canEdit)
		if len(problems) == 0 {
			break
		}
		if attempt == 1 {
			r.status("Kế hoạch vẫn chưa đạt quy tắc chia việc: " + strings.Join(problems, "; "))
			break
		}
		r.status("Kế hoạch chưa đạt quy tắc chia việc, trả lại để lập lại")
		feedback = strings.TrimSpace(feedback + "\nKế hoạch chưa hợp lệ:\n- " + strings.Join(problems, "\n- "))
	}
	st.Data = map[string]any{"analysis": p.Analysis, "conventions": p.Conventions, "assignments": p.Assignments, "answer": p.Answer}
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
	work := r.work(lead, r.goal, plan)
	// no auditor here: diffs that do not apply go back to their workers
	var last string
	for round := 1; round <= maxRepairRounds; round++ {
		targets := r.brokenJobs()
		key := targetsKey(targets)
		if len(targets) == 0 || key == last || r.ctx.Err() != nil {
			break
		}
		last = key
		r.status(fmt.Sprintf("Vòng sửa %d: %d việc có diff không áp được", round, len(targets)))
		r.repair(lead, plan, targets, round)
	}
	work = r.latestSteps(work)
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
	work := r.work(executor, r.goal, plan)

	verdictNote := r.reviewLoop(executor, auditor, plan, veto[auditor.Key])
	r.status(executor.Name + " đang tổng hợp")
	work = r.latestSteps(work)
	st, err := r.step("synthesize", executor, "Tổng hợp kết quả", synthesizePrompt(r.goal, work, r.snapshotPatches(), verdictNote))
	if err != nil {
		return outputsBlock(work), "", "", err
	}
	return st.Output, "", "", nil
}

// reviewLoop reviews the work and sends fixable issues back to the jobs'
// workers until the review passes. It stops when the auditor finds it cannot
// be fixed (fail, with a veto of all changes), needs the person (ask), when
// the same issues come back stuckRounds times, at maxRepairRounds, or when the
// budget runs out. It returns a note for the synthesis.
func (r *run) reviewLoop(assigner, auditor storage.Agent, plan Plan, canVeto bool) string {
	var (
		note    string
		lastKey string
		same    int
	)
	for round := 0; ; round++ {
		r.status(auditor.Name + " đang kiểm tra")
		st, err := r.step("review", auditor, "Kiểm tra kết quả", reviewPrompt(r.goal, r.latestSteps(nil), r.labeledPatches(), round))
		if err != nil {
			return note
		}
		var v Verdict
		if perr := parseJSON(st.Output, &v); perr != nil {
			return note
		}
		st.Data = map[string]any{"verdict": v.Verdict, "summary": v.Summary, "issues": v.Issues, "fixes": v.Fixes, "question": v.Question, "round": round}
		st.Output = stripJSON(st.Output)
		_ = r.svc.store.Tasks().UpdateStep(context.Background(), st)
		sd := toStepDTO(st)
		r.live.emit(Event{Type: "step_done", StepID: st.ID, Step: &sd})
		verdict := strings.ToLower(strings.TrimSpace(v.Verdict))
		note = fmt.Sprintf("Giám sát đánh giá (lần %d): %s. %s", round+1, verdict, v.Summary)

		targets := map[int]string{}
		if verdict == "fix" || verdict == "fail" && len(v.Fixes) > 0 && !v.BlockChanges {
			for _, f := range v.Fixes {
				if strings.TrimSpace(f.Issue) != "" {
					targets[f.Job] = f.Issue
				}
			}
		}
		for n, issue := range r.brokenJobs() { // diffs that do not apply need redoing whatever the verdict
			if _, ok := targets[n]; !ok {
				targets[n] = issue
			}
		}
		r.mu.Lock()
		for n := range targets {
			if r.jobs[n] == nil {
				delete(targets, n)
			}
		}
		r.mu.Unlock()

		switch {
		case verdict == "ask" && strings.TrimSpace(v.Question) != "":
			r.mu.Lock()
			r.ask = v.Question
			r.mu.Unlock()
			return note + " Cần người dùng trả lời: " + v.Question
		case verdict == "pass" && len(targets) == 0:
			return note
		case len(targets) == 0 || verdict == "fail" && (v.BlockChanges || len(v.Fixes) == 0):
			r.mu.Lock()
			r.failed = firstNonEmpty(v.Summary, "không sửa được")
			r.mu.Unlock()
			if canVeto {
				r.vetoPatches(auditor.Name + " phủ quyết: " + v.Summary)
				note += " Mọi thay đổi code đã bị phủ quyết."
			}
			return note
		}
		key := targetsKey(targets)
		if key == lastKey {
			same++
		} else {
			same = 0
		}
		lastKey = key
		if same+1 >= stuckRounds && round > 0 || round+1 >= maxRepairRounds {
			r.mu.Lock()
			r.failed = fmt.Sprintf("sau %d vòng sửa vẫn còn lỗi (%s)", round+1, firstNonEmpty(v.Summary, "không tiến triển"))
			r.mu.Unlock()
			if canVeto {
				r.vetoPatches(auditor.Name + " dừng: sửa nhiều vòng không tiến triển")
			}
			return note + " Dừng vì không tiến triển."
		}
		r.status(fmt.Sprintf("Vòng sửa %d: làm lại %d việc", round+1, len(targets)))
		r.repair(assigner, plan, targets, round+1)
		if r.ctx.Err() != nil {
			return note
		}
	}
}

// brokenJobs lists jobs whose latest diffs do not apply, with the reason.
func (r *run) brokenJobs() map[int]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[int]string{}
	for n, j := range r.jobs {
		for _, p := range r.patches {
			if p.StepID == j.step.ID && p.Status == "failed" {
				out[n] = "Diff cho " + strings.Join(p.Files, ", ") + " không dùng được: " + p.Detail
			}
		}
	}
	return out
}

func targetsKey(t map[int]string) string {
	keys := make([]string, 0, len(t))
	for n, issue := range t {
		keys = append(keys, fmt.Sprintf("%d:%s", n, truncate(issue, 80)))
	}
	slices.Sort(keys)
	return strings.Join(keys, "|")
}

// labeledPatches are the diffs of each job's latest step, with the job number.
func (r *run) labeledPatches() []labeledPatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []labeledPatch
	for _, p := range r.patches {
		for n, j := range r.jobs {
			if p.StepID == j.step.ID {
				out = append(out, labeledPatch{Patch: p, Job: n, Agent: j.agent.Name})
			}
		}
	}
	slices.SortFunc(out, func(a, b labeledPatch) int { return a.Job - b.Job })
	return out
}

// latestSteps replaces each job's step in work by its newest one (after repairs).
func (r *run) latestSteps(work []storage.TaskStep) []storage.TaskStep {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.jobs) == 0 {
		return work
	}
	nums := make([]int, 0, len(r.jobs))
	for n := range r.jobs {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	out := make([]storage.TaskStep, 0, len(nums))
	for _, n := range nums {
		out = append(out, r.jobs[n].step)
	}
	return out
}

// repair redoes the given jobs: their current diffs are set aside and each
// worker gets the issue, its previous attempt and its dependencies' results.
func (r *run) repair(assigner storage.Agent, plan Plan, targets map[int]string, round int) {
	now := time.Now().UTC()
	reason := fmt.Sprintf("Thay bằng bản sửa ở vòng %d", round)
	r.mu.Lock()
	type redo struct {
		j      job
		issue  string
		before string
	}
	var list []redo
	for n, issue := range targets {
		j := r.jobs[n]
		if j == nil {
			continue
		}
		for i, p := range r.patches {
			if p.StepID == j.step.ID && (p.Status == "pending" || p.Status == "failed") {
				if err := r.svc.store.Chat().DecidePatch(context.Background(), p.ID, "rejected", reason, "council", now); err == nil {
					r.patches[i].Status, r.patches[i].Detail = "rejected", reason
					pd := chat.PatchDTO{ID: p.ID, Diff: p.Diff, Files: p.Files, Status: "rejected", Detail: reason, DecidedBy: "council"}
					r.live.emit(Event{Type: "patch", StepID: p.StepID, Patch: &pd})
				}
			}
		}
		var before strings.Builder
		for _, d := range j.a.DependsOn {
			if dj := r.jobs[d]; dj != nil {
				fmt.Fprintf(&before, "--- Việc %d (%s): %s\n%s\n", d, dj.agent.Name, dj.a.Task, truncate(dj.step.Output, 6000))
			}
		}
		list = append(list, redo{j: *j, issue: issue, before: before.String()})
	}
	r.mu.Unlock()
	slices.SortFunc(list, func(a, b redo) int { return a.j.n - b.j.n })

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, it := range list {
		wg.Add(1)
		go func(it redo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			prompt := repairPrompt(r.goal, assigner.Name, it.j.a, plan.Conventions, it.before, it.j.step.Output, it.issue)
			st, err := r.scopedStep("work", it.j.agent, fmt.Sprintf("Sửa lại việc %d (vòng %d): %s", it.j.n, round, it.j.a.Task), prompt, it.j.a.Files)
			if err != nil && st.ID == "" {
				return
			}
			r.mu.Lock()
			if j := r.jobs[it.j.n]; j != nil {
				j.step = st
			}
			r.mu.Unlock()
		}(it)
	}
	wg.Wait()
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
