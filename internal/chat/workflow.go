package chat

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/automation"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// Workflows (spec 2026-10-07-workflows-design): "/key …" in a chat starts
// one in a chat of its own (purpose "workflow_run", not listed); the chat it
// was called from shows only the input and, when it ends, the output. In the
// run's chat the agent coordinates it with the workflow_* tools and the roles
// answer in the background like hand-offs; no person writes there. What the workflow's
// header says (roles, their permission, limits, the brief, gates, votes) is
// enforced here; the body only guides the coordinator.

// wfRun is a workflow running in a chat.
type wfRun struct {
	rec      storage.WorkflowRun
	def      workflow.Def
	bindings map[string]string // role → agent id the project chose
	coord    storage.Agent
	tier     string          // the model tier of the message that started it
	ceiling  string          // the most that message may have run (a bot's user…)
	deadline time.Time       // the run's timeout
	timer    *time.Timer     // fires at the deadline
	batch    map[string]bool // roles answering now; when none is left the coordinator is called back
	voting   *wfVoting       // a vote in progress
	notes    []string        // what the coordinator is told when called back (a role failed…)
	tries    int             // call-backs that found the chat busy
	owed     bool            // a role answered while another was asked and waited for: the coordinator is called back for it
	done     chan struct{}   // closed when the run ends
	// sub-workflows (ADR-102): how many levels may still go below this run,
	// and the deadline of the run that called it (zero: called from a chat)
	depthLeft int
	until     time.Time
	// the run whose role called it and that role (nil: called from a chat):
	// its cost goes up the tree as it is spent, the tree's budgets and
	// concurrency bound it (ADR-103)
	parent     *wfRun
	parentRole string
}

// rootOf is the top of a run's tree (with the lock held).
func rootOf(r *wfRun) *wfRun {
	for r.parent != nil {
		r = r.parent
	}
	return r
}

// RunPurpose is the purpose of a workflow run's own chat.
const RunPurpose = "workflow_run"

// prepared is a run the chat that called it starts in the run's own chat.
type prepared struct {
	conv   string // the run's chat
	run    *wfRun
	prompt string
}

type preparedKey struct{}

func withPrepared(ctx context.Context, p *prepared) context.Context {
	return context.WithValue(ctx, preparedKey{}, p)
}

func preparedOf(ctx context.Context, conversationID string) *prepared {
	if p, _ := ctx.Value(preparedKey{}).(*prepared); p != nil && p.conv == conversationID {
		return p
	}
	return nil
}

// calledFrom is the run a chat called that is still running (nil = none).
func (e *Engine) calledFrom(conversationID string) *wfRun {
	e.wf.mu.Lock()
	defer e.wf.mu.Unlock()
	for _, r := range e.wf.byConv {
		if r.rec.CallerConversationID == conversationID {
			return r
		}
	}
	return nil
}

// wfVoting is a vote waiting for its roles' answers.
type wfVoting struct {
	question string
	ballots  map[string]string // role → yes | no | unclear
}

// wfAsk is a turn the coordinator asked for, started once its answer is done.
type wfAsk struct {
	role   string
	agent  storage.Agent
	prompt string
	vote   bool
	flow   bool // the role is a sub-workflow: the turn is a run of it
}

// wfState is the engine's running workflows.
type wfState struct {
	mu      sync.Mutex
	byConv  map[string]*wfRun      // conversation id → its running workflow
	pending map[string][]wfAsk     // coordinator turn id → turns to start after it
	waiting map[string]chan string // run id/role → a coordinator waiting for that role's answer (workflow_ask)
}

func (s *wfState) init() {
	if s.byConv == nil {
		s.byConv, s.pending, s.waiting = map[string]*wfRun{}, map[string][]wfAsk{}, map[string]chan string{}
	}
}

// accessLevel is the permission a role's access caps its turns at.
func accessLevel(access string) string {
	switch access {
	case workflow.AccessEdit:
		return perm.Edit
	case workflow.AccessPropose:
		return perm.Propose
	}
	return perm.Read
}

func accessLabel(access string) string {
	switch access {
	case workflow.AccessEdit:
		return "sửa trong worktree riêng"
	case workflow.AccessPropose:
		return "đề xuất, người duyệt"
	}
	return "chỉ phân tích"
}

// family is the vendor family of a connection (workflow.Family).
func family(p storage.Provider) string { return workflow.Family(p) }

// agentFamily is the family of the connection an agent's turns run on first.
func (e *Engine) agentFamily(ctx context.Context, a storage.Agent) (string, string) {
	if e.providers == nil {
		return "", ""
	}
	c, err := e.choices(ctx, a)
	if err != nil || len(c) == 0 {
		return "", ""
	}
	return family(c[0].Provider), c[0].Provider.Name
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// ---- starting ----

// ErrWorkflowRunning: the chat already runs a workflow.
var ErrWorkflowRunning = errors.New("cuộc chat đang chạy một quy trình; đợi xong hoặc bấm Dừng")

// prepWorkflow reads "/key rest" as a workflow of the project. It is not one
// (handled false) when no enabled workflow has that key; the skill of that
// name, if any, then runs as before.
func (e *Engine) prepWorkflow(ctx context.Context, conv storage.Conversation, coord storage.Agent, text string) (*wfRun, string, bool, error) {
	name, rest, isCall := automation.ParseSkillCall(text)
	if !isCall || !teamChat(conv) {
		return nil, "", false, nil
	}
	if conv.Purpose == "channel" && name != skillOf(ctx) {
		return nil, "", false, nil // a bot's chat runs only what its command names
	}
	w, err := e.store.Workflows().GetByKey(ctx, conv.ProjectID, name)
	if key, ok := workflow.Renamed[name]; ok && errors.Is(err, storage.ErrNotFound) {
		w, err = e.store.Workflows().GetByKey(ctx, conv.ProjectID, key) // the old Vietnamese command still works
	}
	if err != nil || !w.Enabled {
		return nil, "", false, nil
	}
	def, err := workflow.Parse(w.Source)
	if err != nil {
		return nil, "", true, fmt.Errorf("quy trình /%s không hợp lệ: %w", name, err)
	}
	if def.Callable == workflow.CallableSub {
		return nil, "", true, fmt.Errorf("quy trình /%s chỉ để quy trình khác gọi (callable: sub)", def.Key)
	}
	if e.calledFrom(conv.ID) != nil {
		return nil, "", true, ErrWorkflowRunning
	}
	run, prompt, err := e.newRun(ctx, conv.ProjectID, w, def, coord, rest)
	return run, prompt, true, err
}

// newRun prepares a run of workflow w coordinated by coord, asked input.
func (e *Engine) newRun(ctx context.Context, projectID string, w storage.Workflow, def workflow.Def, coord storage.Agent, rest string) (*wfRun, string, error) {
	agents, err := e.Agents(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	byID := map[string]storage.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	run := &wfRun{def: def, bindings: map[string]string{}, coord: coord, batch: map[string]bool{}, done: make(chan struct{}), depthLeft: def.Limits.Depth}
	run.rec = storage.WorkflowRun{ProjectID: projectID, WorkflowID: w.ID, WorkflowKey: def.Key, WorkflowName: def.Name,
		BodyHash: hashOf(w.Source), CoordinatorID: coord.ID, CoordinatorName: coord.Name, Input: rest, Status: storage.RunRunning}
	for _, r := range def.Roles {
		rr := storage.RunRole{Role: r.Key, Name: r.Name, Access: r.Access, Status: "idle", Workflow: r.Workflow}
		if id := w.Bindings[r.Key]; id != "" { // a sub-workflow's: the agent coordinating it (the coordinator too)
			if a, ok := byID[id]; ok && !a.Disabled && (a.ID != coord.ID || r.Workflow != "") {
				run.bindings[r.Key] = a.ID
				rr.AgentID, rr.AgentName = a.ID, a.Name
			}
		}
		run.rec.Roles = append(run.rec.Roles, rr)
	}
	for _, g := range def.Gates {
		run.rec.Gates = append(run.rec.Gates, storage.RunGate{Key: g.Key, Name: g.Name, Kind: g.Kind, Required: g.Required, Status: "open"})
	}
	if warn := e.differWarnings(ctx, run, byID); len(warn) > 0 {
		if def.Strict {
			return nil, "", fmt.Errorf("quy trình /%s cần các vai dùng kết nối AI khác hãng: %s", def.Key, strings.Join(warn, "; "))
		}
		run.notes = append(run.notes, "Cảnh báo: "+strings.Join(warn, "; "))
	}
	input := strings.TrimSpace(rest)
	if input == "" {
		input = "(không ghi thêm)"
	}
	prompt := fmt.Sprintf("Người dùng chạy quy trình **%s** (/%s) với yêu cầu:\n%s\n\nBắt đầu theo hướng dẫn của quy trình ở phần hệ thống.", def.Name, def.Key, input) // i18n-ignore
	if len(def.Inputs) > 0 {
		prompt += "\nĐầu vào quy trình cần (lấy từ yêu cầu trên; thiếu thì tự quyết và ghi giả định): " + fieldList(def.Inputs) // i18n-ignore
	}
	return run, prompt, nil
}

// differWarnings: roles already filled whose agents share a vendor family
// they must not share.
func (e *Engine) differWarnings(ctx context.Context, run *wfRun, byID map[string]storage.Agent) []string {
	fam := func(role string) (string, string) {
		if role == workflow.Coordinator {
			return e.agentFamily(ctx, run.coord)
		}
		if a, ok := byID[run.bindings[role]]; ok {
			return e.agentFamily(ctx, a)
		}
		return "", ""
	}
	var out []string
	for _, r := range run.def.Roles {
		f, p := fam(r.Key)
		if f == "" || r.Workflow != "" {
			continue
		}
		for _, o := range r.DifferFrom {
			if g, q := fam(o); g != "" && g == f {
				out = append(out, fmt.Sprintf("vai %s (%s) và %s (%s) cùng hãng", r.Key, p, o, q))
			}
		}
	}
	return out
}

// startRun keeps a prepared run going once its first turn has started.
func (e *Engine) startRun(conv storage.Conversation, run *wfRun, turn *Turn) {
	ctx := context.Background()
	run.tier, run.ceiling = turn.tier, turn.ceiling
	run.deadline = time.Now().Add(run.def.Timeout())
	if !run.until.IsZero() && run.until.Before(run.deadline) { // no later than the run that called it
		run.deadline = run.until
	}
	run.rec.ConversationID = conv.ID // its own chat (the caller's is CallerConversationID)
	run.rec.Actor, run.rec.StartedAt = turn.actor, time.Now().UTC()
	run.rec.Log = append(run.rec.Log, storage.RunLog{At: run.rec.StartedAt, Text: "Bắt đầu, " + run.coord.Name + " điều phối"})
	for _, n := range run.notes {
		run.rec.Log = append(run.rec.Log, storage.RunLog{At: run.rec.StartedAt, Text: n})
	}
	rec, err := e.store.WorkflowRuns().Create(ctx, run.rec)
	if err != nil {
		slog.Warn("workflow: save run", "err", err)
	}
	run.rec = rec
	e.wf.mu.Lock()
	e.wf.init()
	e.wf.byConv[conv.ID] = run
	run.timer = time.AfterFunc(time.Until(run.deadline), func() { e.wfExpire(conv.ID, rec.ID) })
	e.wf.mu.Unlock()
	e.mu.Lock()
	turn.wfRun = rec.ID // the chat shows the run's card where it started (the dashboard puts it by time)
	e.mu.Unlock()
}

// runOf is the chat's running workflow (nil = none).
func (e *Engine) runOf(conversationID string) *wfRun {
	e.wf.mu.Lock()
	defer e.wf.mu.Unlock()
	return e.wf.byConv[conversationID]
}

// RunningWorkflow is the id of the workflow a chat runs ("" = none).
func (e *Engine) RunningWorkflow(conversationID string) string {
	if r := e.runOf(conversationID); r != nil {
		return r.rec.ID
	}
	return ""
}

// save writes the run as it is now (call without the lock, with a copy).
func (e *Engine) saveRun(rec storage.WorkflowRun) {
	if err := e.store.WorkflowRuns().Update(context.Background(), rec); err != nil {
		slog.Warn("workflow: save run", "run", rec.ID, "err", err)
	}
}

// logRun adds a line to the run's log (with the lock held).
func (r *wfRun) logf(format string, a ...any) {
	r.rec.Log = append(r.rec.Log, storage.RunLog{At: time.Now().UTC(), Text: fmt.Sprintf(format, a...)})
	if len(r.rec.Log) > 200 {
		r.rec.Log = r.rec.Log[len(r.rec.Log)-200:]
	}
}

func (r *wfRun) role(key string) *storage.RunRole {
	for i := range r.rec.Roles {
		if r.rec.Roles[i].Role == key {
			return &r.rec.Roles[i]
		}
	}
	return nil
}

func (r *wfRun) gate(key string) *storage.RunGate {
	for i := range r.rec.Gates {
		if r.rec.Gates[i].Key == key {
			return &r.rec.Gates[i]
		}
	}
	return nil
}

// finishRun ends a run (status done, failed or stopped).
func (e *Engine) finishRun(conversationID, runID, status, result, errText string) {
	e.wf.mu.Lock()
	run := e.wf.byConv[conversationID]
	if run == nil || run.rec.ID != runID {
		e.wf.mu.Unlock()
		return
	}
	delete(e.wf.byConv, conversationID)
	if run.timer != nil {
		run.timer.Stop()
	}
	defer close(run.done)
	now := time.Now().UTC()
	run.rec.Status, run.rec.FinishedAt = status, &now
	run.rec.Result, run.rec.Error = result, errText
	switch status {
	case storage.RunDone:
		run.logf("Xong")
	case storage.RunStopped:
		run.logf("Đã dừng")
	default:
		run.logf("Thất bại: %s", errText)
	}
	rec := run.rec
	var children []string // its sub-workflows still running end with it
	for conv, r := range e.wf.byConv {
		if r.rec.ParentRunID == runID {
			children = append(children, conv)
		}
	}
	e.wf.mu.Unlock()
	e.saveRun(rec)
	for _, c := range children {
		e.StopAll(c)
	}
}

// wfExpire ends a run that ran out of time, stopping its roles' turns.
func (e *Engine) wfExpire(conversationID, runID string) {
	e.cancelRunTurns(runID)
	e.finishRun(conversationID, runID, storage.RunFailed, "", "hết thời gian của quy trình")
	if conv, err := e.store.Chat().GetConversation(context.Background(), conversationID); err == nil {
		e.note(conv, "⏱️ Quy trình đã hết thời gian và dừng lại.")
	}
}

// cancelRunTurns stops the turns of a run still answering.
func (e *Engine) cancelRunTurns(runID string) {
	e.mu.Lock()
	var stop []*Turn
	for _, t := range e.turns {
		if t.wfRun == runID {
			stop = append(stop, t)
		}
	}
	e.mu.Unlock()
	for _, t := range stop {
		t.Cancel()
	}
}

// stopRun: the person stopped the chat.
func (e *Engine) stopRun(conversationID string) {
	if r := e.runOf(conversationID); r != nil {
		e.finishRun(conversationID, r.rec.ID, storage.RunStopped, "", "")
	}
}

// ---- the coordinator's turn ----

// coordinatorOf: turn is the coordinator answering in its chat's running
// workflow (the person's messages and the call-backs while it runs).
func (e *Engine) coordinatorOf(conversationID string, turn *Turn, agentID string) *wfRun {
	run := e.runOf(conversationID)
	if run == nil || turn.background || run.coord.ID != agentID {
		return nil
	}
	e.mu.Lock()
	turn.wfRun = run.rec.ID
	e.mu.Unlock()
	return run
}

// wfBrief is what the coordinator is told about its workflow.
func (e *Engine) wfBrief(ctx context.Context, run *wfRun, projectID string) string {
	e.refreshGates(ctx, run)
	e.wf.mu.Lock()
	rec, def := run.rec, run.def
	left := def.Limits.Turns - rec.Turns
	deadline := run.deadline
	e.wf.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n## Quy trình đang chạy: %s (/%s)\n%s\nBạn là agent ĐIỀU PHỐI. Người dùng yêu cầu: %s\n", def.Name, def.Key, def.Description, cmp.Or(rec.Input, "(không ghi thêm)")) // i18n-ignore
	b.WriteString("\n### Các vai\n")
	for _, r := range rec.Roles {
		who := r.AgentName
		if who == "" {
			who = "chưa gán: chọn agent bằng tham số agent khi giao" // i18n-ignore
		}
		d, _ := def.Role(r.Role)
		if d.Workflow != "" {
			if r.AgentName == "" {
				who = "bạn" // i18n-ignore
			}
			fmt.Fprintf(&b, "- `%s` %s: QUY TRÌNH CON /%s (điều phối: %s, trần quyền %s); %s, đã chạy lại %d lần\n", r.Role, r.Name, d.Workflow, who, accessLabel(r.Access), roleStatus(r.Status), r.Rounds) // i18n-ignore
			continue
		}
		extra := ""
		if len(d.DifferFrom) > 0 {
			extra = ", phải khác hãng model với " + strings.Join(d.DifferFrom, ", ")
		}
		fmt.Fprintf(&b, "- `%s` %s (%s%s): %s; %s, đã gửi tiếp %d lần\n", r.Role, r.Name, accessLabel(r.Access), extra, who, roleStatus(r.Status), r.Rounds) // i18n-ignore
		if d.Hint != "" {
			fmt.Fprintf(&b, "  sở trường: %s\n", d.Hint)
		}
		if p := d.Prefer; p != nil && (p.Tier != "" || p.Family != "") {
			fmt.Fprintf(&b, "  nên dùng agent: %s\n", strings.Trim(p.Tier+" "+p.Family, " ")) // i18n-ignore
		}
	}
	if slices.ContainsFunc(def.Roles, func(r workflow.Role) bool { return r.Workflow != "" }) {
		fmt.Fprintf(&b, "Vai là quy trình con: giao bằng workflow_delegate như vai thường (bản giao là đầu vào của nó). Nó chạy trong chat riêng; đầu ra của nó thành một tin trong cuộc chat này khi xong. workflow_send chạy lại nó với nội dung mới. Còn lồng được %d cấp.\n", run.depthLeft) // i18n-ignore
	}
	if len(def.Outputs) > 0 {
		fmt.Fprintf(&b, "Kết thúc bằng workflow_done kèm outputs (object theo key): %s.\n", fieldList(def.Outputs)) // i18n-ignore
	}
	if len(def.Parallel) > 0 {
		var g []string
		for _, p := range def.Parallel {
			g = append(g, strings.Join(p, " + "))
		}
		fmt.Fprintf(&b, "Được giao cùng lúc: %s. Các vai khác giao lần lượt.\n", strings.Join(g, "; ")) // i18n-ignore
	}
	if len(rec.Gates) > 0 {
		b.WriteString("\n### Cổng (mở bằng workflow_gate)\n")
		for _, g := range rec.Gates {
			req := ""
			if g.Required {
				req = ", bắt buộc trước workflow_done"
			}
			kind := "người dùng duyệt"
			if g.Kind == workflow.GateCheck {
				kind = "lệnh kiểm tra phải đạt"
				if gd, _ := def.Gate(g.Key); gd.Command != "" {
					kind += ": " + gd.Command
				}
			}
			fmt.Fprintf(&b, "- `%s` %s (%s%s): %s\n", g.Key, g.Name, kind, req, gateStatus(g.Status)) // i18n-ignore
		}
	}
	if v := def.Vote; v != nil {
		fmt.Fprintf(&b, "\n### Biểu quyết (workflow_vote)\nCác vai %s bỏ phiếu, cần %d phiếu đồng ý", strings.Join(v.Roles, ", "), v.Quorum) // i18n-ignore
		if len(v.Veto) > 0 {
			fmt.Fprintf(&b, "; %s phản đối là bác (phủ quyết)", strings.Join(v.Veto, ", ")) // i18n-ignore
		}
		b.WriteString(".\n")
	}
	fmt.Fprintf(&b, "\n### Giới hạn\nCòn %d lượt cho các vai; mỗi vai gửi tiếp tối đa %d lần; hết hạn lúc %s.", left, def.Limits.Rounds, deadline.Format("15:04")) // i18n-ignore
	if def.Limits.BudgetUSD > 0 {
		fmt.Fprintf(&b, " Ngân sách $%.2f, đã dùng $%.2f.", def.Limits.BudgetUSD, rec.CostUSD) // i18n-ignore
	}
	b.WriteString("\n\n### Hướng dẫn của quy trình\n" + def.Body + "\n")
	b.WriteString(`
### Cách điều phối
- Giao việc CHỈ bằng workflow_delegate (lần đầu), workflow_send (gửi tiếp cho vai đã giao, vai giữ mạch), workflow_vote (biểu quyết). Câu hỏi ngắn cho vai chỉ phân tích thì dùng workflow_ask: chờ câu trả lời ngay trong lượt, không phải đợi gọi lại. Không tự làm phần việc của các vai; không dùng delegate.
- Cuộc chat này là của riêng lần chạy: không ai nhắn vào, người gọi chỉ thấy yêu cầu ban đầu và summary của workflow_done. Đừng hỏi lại người dùng; thiếu thông tin thì tự quyết theo hướng an toàn và ghi rõ giả định trong kết quả.
- Gọi xong thì ghi ngắn (đã giao gì cho ai) rồi DỪNG lượt. Các vai làm ở nền; khi tất cả vai vừa giao đã xong, office gọi lại bạn, kết quả của họ là các tin ngay trên trong cuộc chat.
- Bản giao việc nêu kết quả cần đạt, ràng buộc và phương án đang thử; không viết sẵn cách sửa từng file hay từng hàm, để vai đọc code rồi tự quyết.
- Xong hết (và qua các cổng bắt buộc) thì gọi workflow_done; summary là kết quả gửi người gọi (đầy đủ, không dẫn chiếu "ở trên").
`) // i18n-ignore
	if agents, err := e.Agents(ctx, projectID); err == nil {
		b.WriteString("\nAgent của project có thể gán vào vai: ") // i18n-ignore
		var names []string
		for _, a := range storage.OnAgents(agents) {
			if a.ID == run.coord.ID {
				continue
			}
			_, prov := e.agentFamily(ctx, a)
			names = append(names, fmt.Sprintf("%s (%s, %s, quyền %s)", a.Name, cmp.Or(a.Role, "—"), cmp.Or(prov, "?"), perm.Label(perm.Agent(a))))
		}
		b.WriteString(strings.Join(names, "; ") + "\n")
	}
	return b.String()
}

func roleStatus(s string) string {
	switch s {
	case "working":
		return "đang làm"
	case "done":
		return "đã xong"
	case "failed":
		return "lỗi"
	}
	return "chưa giao"
}

func gateStatus(s string) string {
	switch s {
	case "waiting":
		return "đang chờ"
	case "passed":
		return "đã qua"
	case "failed":
		return "không đạt"
	}
	return "chưa mở"
}

// ---- the tools ----

// WorkflowScope tells the toolbox what a run of sc may use: coordinator =
// the workflow_* tools; inRun = a turn of a running workflow (no delegate).
func (e *Engine) WorkflowScope(sc officetools.Scope) (coordinator, inRun bool) {
	e.mu.Lock()
	t := e.turns[sc.RunRef]
	e.mu.Unlock()
	if t == nil || t.wfRun == "" {
		return false, false
	}
	return t.wfRole == "", true
}

// WorkflowCall runs a workflow_* tool for the coordinator in sc.
func (e *Engine) WorkflowCall(ctx context.Context, sc officetools.Scope, name string, raw json.RawMessage) (string, error) {
	e.mu.Lock()
	t := e.turns[sc.RunRef]
	e.mu.Unlock()
	if t == nil || t.wfRun == "" || t.wfRole != "" {
		return "", errors.New("chỉ agent điều phối của một quy trình đang chạy mới dùng được công cụ này")
	}
	run := e.runOf(sc.ConversationID)
	if run == nil || run.rec.ID != t.wfRun {
		return "", errors.New("quy trình đã kết thúc")
	}
	var in struct {
		Role     string            `json:"role"`
		Agent    string            `json:"agent"`
		Brief    map[string]string `json:"brief"`
		Message  string            `json:"message"`
		Question string            `json:"question"`
		Agents   map[string]string `json:"agents"`
		Gate     string            `json:"gate"`
		Note     string            `json:"note"`
		Command  string            `json:"command"`
		Summary  string            `json:"summary"`
		Outputs  map[string]string `json:"outputs"`
		Wait     int               `json:"wait_seconds"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", fmt.Errorf("tham số không hợp lệ: %w", err)
		}
	}
	switch name {
	case "workflow_delegate":
		return e.wfDelegate(ctx, sc, run, in.Role, in.Agent, in.Brief)
	case "workflow_ask":
		return e.wfAskNow(ctx, sc, run, in.Role, in.Question, in.Wait)
	case "workflow_send":
		return e.wfSend(ctx, sc, run, in.Role, in.Message)
	case "workflow_vote":
		return e.wfVote(ctx, sc, run, in.Question, in.Agents)
	case "workflow_gate":
		return e.wfGate(ctx, sc, run, in.Gate, in.Note, in.Command, in.Role)
	case "workflow_done":
		return e.wfDone(ctx, sc, run, in.Summary, in.Outputs)
	}
	return "", fmt.Errorf("không có công cụ %s", name)
}

// queued are the asks of this coordinator turn not yet started.
func (e *Engine) queuedAsks(turnID string) []wfAsk {
	return e.wf.pending[turnID]
}

// pickAgent fills a role: the name given, else the project's binding.
func (e *Engine) pickAgent(ctx context.Context, run *wfRun, role, name string) (storage.Agent, error) {
	agents, err := e.Agents(ctx, run.rec.ProjectID)
	if err != nil {
		return storage.Agent{}, err
	}
	var a storage.Agent
	if name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "@"))); name != "" {
		i := slices.IndexFunc(agents, func(x storage.Agent) bool { return strings.ToLower(x.Name) == name || strings.ToLower(x.Key) == name })
		if i < 0 {
			return a, fmt.Errorf("không có agent %q trong project", name)
		}
		a = agents[i]
	} else if r := run.role(role); r != nil && r.AgentID != "" {
		i := slices.IndexFunc(agents, func(x storage.Agent) bool { return x.ID == r.AgentID })
		if i < 0 {
			return a, fmt.Errorf("agent của vai %s không còn trong project", role)
		}
		a = agents[i]
	} else {
		var names []string
		for _, x := range storage.OnAgents(agents) {
			if x.ID != run.coord.ID {
				names = append(names, x.Name)
			}
		}
		return a, fmt.Errorf("vai %s chưa gán agent: gọi lại với tham số agent (một trong: %s)", role, strings.Join(names, ", "))
	}
	if a.Disabled {
		return a, errors.New(storage.OffNotice(a.Name))
	}
	if a.ID == run.coord.ID {
		return a, errors.New("agent điều phối không tự nhận vai; chọn agent khác")
	}
	return a, nil
}

// checkDiffer: agent a in role must not share a vendor family with the
// roles it must differ from (or, for those, with role).
func (e *Engine) checkDiffer(ctx context.Context, run *wfRun, role string, a storage.Agent) error {
	fam, prov := e.agentFamily(ctx, a)
	if fam == "" {
		return nil
	}
	against := func(other string) (storage.Agent, bool) {
		if other == workflow.Coordinator {
			return run.coord, true
		}
		if r := run.role(other); r != nil && r.AgentID != "" {
			ag, err := e.store.Agents().Get(ctx, r.AgentID)
			return ag, err == nil
		}
		return storage.Agent{}, false
	}
	d, _ := run.def.Role(role)
	if d.Workflow != "" {
		return nil
	}
	others := slices.Clone(d.DifferFrom)
	for _, r := range run.def.Roles { // roles that must differ from this one
		if slices.Contains(r.DifferFrom, role) {
			others = append(others, r.Key)
		}
	}
	for _, o := range others {
		if b, ok := against(o); ok && b.ID != "" {
			if f, q := e.agentFamily(ctx, b); f == fam {
				msg := fmt.Sprintf("vai %s (%s) phải khác hãng model với %s (%s, %s)", role, prov, o, b.Name, q)
				if run.def.Strict {
					return errors.New(msg + "; chọn agent khác")
				}
				run.notes = append(run.notes, "Cảnh báo: "+msg)
			}
		}
	}
	return nil
}

func (e *Engine) wfDelegate(ctx context.Context, sc officetools.Scope, run *wfRun, role, agentName string, brief map[string]string) (string, error) {
	d, ok := run.def.Role(role)
	if !ok {
		return "", fmt.Errorf("quy trình không có vai %q (có: %s)", role, roleKeys(run.def))
	}
	if d.Workflow != "" {
		return e.wfDelegateFlow(ctx, sc, run, d, agentName, brief)
	}
	a, err := e.pickAgent(ctx, run, role, agentName)
	if err != nil {
		return "", err
	}
	prompt, err := run.def.RenderBrief(d, brief)
	if err != nil {
		return "", err
	}
	e.wf.mu.Lock()
	r := run.role(role)
	if r.Status == "working" {
		e.wf.mu.Unlock()
		return "", fmt.Errorf("vai %s đang làm; đợi xong rồi gửi tiếp bằng workflow_send", role)
	}
	if err := e.canAsk(run, sc.RunRef, role, a); err != nil {
		e.wf.mu.Unlock()
		return "", err
	}
	e.wf.mu.Unlock()
	if err := e.checkDiffer(ctx, run, role, a); err != nil {
		return "", err
	}
	e.wf.mu.Lock()
	r = run.role(role)
	r.AgentID, r.AgentName = a.ID, a.Name
	e.wf.pending[sc.RunRef] = append(e.wf.pending[sc.RunRef], wfAsk{role: role, agent: a, prompt: prompt})
	run.logf("Giao vai %s cho %s", d.Name, a.Name)
	rec := run.rec
	e.wf.mu.Unlock()
	e.saveRun(rec)
	return fmt.Sprintf("Đã giao vai %s cho %s; bắt đầu khi bạn trả lời xong lượt này và làm ở nền (quyền: %s). Trả lời người dùng ngắn gọn rồi dừng lượt; khi các vai vừa giao xong, bạn được gọi lại.",
		d.Name, a.Name, accessLabel(d.Access)), nil
}

// canAsk checks a new turn for role (with the lock held): the turn limit,
// the budget, and what may answer at once.
func (e *Engine) canAsk(run *wfRun, turnID, role string, a storage.Agent) error {
	queued := e.queuedAsks(turnID)
	if run.rec.Turns+len(run.batch)+len(queued) >= run.def.Limits.Turns {
		return fmt.Errorf("đã hết %d lượt của các vai trong quy trình; tổng kết kết quả hiện có rồi gọi workflow_done", run.def.Limits.Turns)
	}
	for r := run; r != nil; r = r.parent { // its own budget and those of the runs above it
		if b := r.def.Limits.BudgetUSD; b > 0 && r.rec.CostUSD >= b {
			if r == run {
				return fmt.Errorf("quy trình đã dùng hết ngân sách $%.2f; tổng kết rồi gọi workflow_done", b)
			}
			return fmt.Errorf("quy trình cấp trên /%s đã dùng hết ngân sách $%.2f; tổng kết rồi gọi workflow_done", r.def.Key, b)
		}
	}
	root := rootOf(run) // roles answering at once in the whole tree
	busyN := len(queued)
	for _, x := range e.wf.byConv {
		if rootOf(x) == root {
			busyN += len(x.batch)
		}
	}
	if limit := root.def.Limits.Concurrency; limit > 0 && busyN >= limit {
		return fmt.Errorf("đã có %d vai đang làm trong cả cây quy trình (limits.concurrency %d); đợi bớt rồi giao tiếp", busyN, limit)
	}
	busy := map[string]bool{}
	for k := range run.batch {
		busy[k] = true
	}
	for _, q := range queued {
		if q.role == role && !q.vote {
			return fmt.Errorf("vai %s đã được giao trong lượt này", role)
		}
		busy[q.role] = true
	}
	for k := range busy {
		if k == role {
			continue
		}
		if !run.def.SameBatch(role, k) {
			return fmt.Errorf("vai %s đang/sắp làm và không được làm cùng lúc với %s (parallel của quy trình); đợi xong rồi giao tiếp", k, role)
		}
		if r := run.role(k); r != nil && r.AgentID == a.ID {
			return fmt.Errorf("%s đang làm vai %s; hai vai làm cùng lúc phải là hai agent khác nhau", a.Name, k)
		}
	}
	return nil
}

func roleKeys(d workflow.Def) string {
	keys := make([]string, 0, len(d.Roles))
	for _, r := range d.Roles {
		keys = append(keys, r.Key)
	}
	return strings.Join(keys, ", ")
}

func (e *Engine) wfSend(ctx context.Context, sc officetools.Scope, run *wfRun, role, message string) (string, error) {
	d, ok := run.def.Role(role)
	if !ok {
		return "", fmt.Errorf("quy trình không có vai %q (có: %s)", role, roleKeys(run.def))
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("hãy ghi nội dung gửi")
	}
	e.wf.mu.Lock()
	r := run.role(role)
	if r.AgentID == "" || r.Status == "idle" {
		e.wf.mu.Unlock()
		return "", fmt.Errorf("vai %s chưa được giao; dùng workflow_delegate trước", role)
	}
	if r.Status == "working" {
		e.wf.mu.Unlock()
		return "", fmt.Errorf("vai %s đang làm; đợi xong", role)
	}
	if r.Rounds >= run.def.Limits.Rounds {
		e.wf.mu.Unlock()
		return "", fmt.Errorf("đã gửi tiếp cho vai %s %d lần (giới hạn); tổng kết với kết quả hiện có", role, r.Rounds)
	}
	agentID := r.AgentID
	e.wf.mu.Unlock()
	if d.Workflow != "" { // a sub-workflow: it runs again with this as its input
		return e.wfAskFlow(ctx, sc, run, d, agentID, message, true)
	}
	a, err := e.store.Agents().Get(ctx, agentID)
	if err != nil {
		return "", err
	}
	if a.Disabled {
		return "", errors.New(storage.OffNotice(a.Name))
	}
	e.wf.mu.Lock()
	defer e.wf.mu.Unlock()
	if err := e.canAsk(run, sc.RunRef, role, a); err != nil {
		return "", err
	}
	run.role(role).Rounds++
	prompt := fmt.Sprintf("%s (điều phối quy trình %s) gửi tiếp cho bạn, vai %s:\n%s\n\n%s", run.coord.Name, run.def.Name, d.Name, message, workflow.AccessNote(d.Access))
	e.wf.pending[sc.RunRef] = append(e.wf.pending[sc.RunRef], wfAsk{role: role, agent: a, prompt: prompt})
	run.logf("Gửi tiếp cho %s (%s)", d.Name, a.Name)
	rec := run.rec
	go e.saveRun(rec)
	return fmt.Sprintf("Đã gửi cho %s (vai %s); bắt đầu khi bạn trả lời xong lượt này. Trả lời người dùng ngắn gọn rồi dừng lượt.", a.Name, d.Name), nil
}

const ballotLine = "PHIẾU: ĐỒNG Ý"

func (e *Engine) wfVote(ctx context.Context, sc officetools.Scope, run *wfRun, question string, names map[string]string) (string, error) {
	v := run.def.Vote
	if v == nil {
		return "", errors.New("quy trình này không có biểu quyết")
	}
	if strings.TrimSpace(question) == "" {
		return "", errors.New("hãy ghi điều cần biểu quyết (kèm đủ bối cảnh: các vai không đọc được suy nghĩ của bạn)")
	}
	var asks []wfAsk
	seen := map[string]string{}
	for _, role := range v.Roles {
		a, err := e.pickAgent(ctx, run, role, names[role])
		if err != nil {
			return "", fmt.Errorf("%w (truyền agents: {\"%s\": \"tên agent\"})", err, role)
		}
		if other, dup := seen[a.ID]; dup {
			return "", fmt.Errorf("%s đang bỏ phiếu ở vai %s; mỗi vai bỏ phiếu phải là một agent khác", a.Name, other)
		}
		seen[a.ID] = role
		d, _ := run.def.Role(role)
		prompt := fmt.Sprintf("%s (điều phối quy trình %s) cần bạn bỏ phiếu với tư cách vai **%s**:\n\n%s\n\n"+
			"Dòng ĐẦU TIÊN của câu trả lời phải đúng một trong hai: `PHIẾU: ĐỒNG Ý` hoặc `PHIẾU: KHÔNG ĐỒNG Ý`; sau đó là lý do ngắn, dựa trên bằng chứng. Chỉ phân tích, không sửa file.",
			run.coord.Name, run.def.Name, d.Name, question) // i18n-ignore
		asks = append(asks, wfAsk{role: role, agent: a, prompt: prompt, vote: true})
	}
	e.wf.mu.Lock()
	defer e.wf.mu.Unlock()
	if run.voting != nil || len(run.batch) > 0 || len(e.queuedAsks(sc.RunRef)) > 0 {
		return "", errors.New("đang có vai làm việc hoặc cuộc biểu quyết khác; đợi xong rồi biểu quyết")
	}
	if run.rec.Turns+len(asks) > run.def.Limits.Turns {
		return "", fmt.Errorf("không đủ lượt cho %d phiếu (còn %d)", len(asks), run.def.Limits.Turns-run.rec.Turns)
	}
	for _, a := range asks {
		if r := run.role(a.role); r != nil && r.AgentID == "" {
			r.AgentID, r.AgentName = a.agent.ID, a.agent.Name
		}
	}
	run.voting = &wfVoting{question: question, ballots: map[string]string{}}
	e.wf.pending[sc.RunRef] = append(e.wf.pending[sc.RunRef], asks...)
	run.logf("Biểu quyết: %s", truncate(oneLine(question), 120))
	rec := run.rec
	go e.saveRun(rec)
	return fmt.Sprintf("Đã gửi biểu quyết cho %d vai; bắt đầu khi bạn trả lời xong lượt này. Office đếm phiếu (cần %d đồng ý) rồi gọi lại bạn kèm kết quả.", len(asks), v.Quorum), nil
}

// ballot reads a vote from the first lines of an answer.
func ballot(text string) string {
	for _, line := range strings.SplitN(strings.TrimSpace(text), "\n", 4) {
		l := strings.ToUpper(strings.Trim(strings.TrimSpace(line), "*`_# "))
		switch {
		case strings.Contains(l, "KHÔNG ĐỒNG Ý"), strings.Contains(l, "KHONG DONG Y"), strings.Contains(l, "PHẢN ĐỐI"):
			return "no"
		case strings.Contains(l, "ĐỒNG Ý"), strings.Contains(l, "DONG Y"), strings.Contains(l, "TÁN THÀNH"):
			return "yes"
		}
	}
	return "unclear"
}

// tally is the outcome of a vote, as the coordinator is told.
func tally(v *workflow.Vote, ballots map[string]string) (bool, string) {
	yes, vetoed := 0, []string{}
	var lines []string
	for _, role := range v.Roles {
		b := ballots[role]
		label := map[string]string{"yes": "đồng ý", "no": "không đồng ý"}[b]
		if label == "" {
			label = "không rõ (tính là không đồng ý)"
		}
		lines = append(lines, role+": "+label)
		if b == "yes" {
			yes++
		} else if slices.Contains(v.Veto, role) {
			vetoed = append(vetoed, role)
		}
	}
	passed := yes >= v.Quorum && len(vetoed) == 0
	out := fmt.Sprintf("Kết quả biểu quyết: %d/%d đồng ý (cần %d) — %s.", yes, len(v.Roles), v.Quorum, strings.Join(lines, ", "))
	if len(vetoed) > 0 {
		out += " Bị phủ quyết bởi " + strings.Join(vetoed, ", ") + "."
	}
	if passed {
		return true, out + " THÔNG QUA."
	}
	return false, out + " KHÔNG THÔNG QUA."
}

func (e *Engine) wfGate(ctx context.Context, sc officetools.Scope, run *wfRun, key, note, command, role string) (string, error) {
	g, ok := run.def.Gate(key)
	if !ok {
		return "", fmt.Errorf("quy trình không có cổng %q", key)
	}
	if e.office == nil || e.acts == nil {
		return "", errors.New("office không mở cổng được ở đây")
	}
	switch g.Kind {
	case workflow.GateApprove:
		if strings.TrimSpace(note) == "" {
			return "", errors.New("hãy ghi điều người dùng cần duyệt (note), đủ để họ quyết mà không phải đọc lại cả cuộc chat")
		}
		a, err := e.acts.Propose(ctx, sc, "workflow_gate", run.def.Name+": "+g.Name, note)
		if err != nil {
			return "", err
		}
		e.wf.mu.Lock()
		if rg := run.gate(key); rg != nil {
			rg.Status, rg.ActionID, rg.Detail = "waiting", a.ID, ""
		}
		run.logf("Mở cổng %s, chờ người dùng duyệt", g.Name)
		rec := run.rec
		e.wf.mu.Unlock()
		e.saveRun(rec)
		return "Đã mở cổng " + g.Name + " (thẻ duyệt trong chat). Báo người dùng cần duyệt gì rồi DỪNG lượt; duyệt hay từ chối xong bạn sẽ được gọi lại.", nil
	}
	cmdline := cmp.Or(strings.TrimSpace(g.Command), strings.TrimSpace(command))
	if cmdline == "" {
		return "", fmt.Errorf("cổng %s cần lệnh kiểm tra (command), ví dụ một trong các lệnh được tự chạy: %s", g.Name, strings.Join(sc.Access.Commands, ", "))
	}
	csc := sc
	csc.Dir = ""
	if role != "" { // in the worktree of the role that changed the code
		r := run.role(role)
		if r == nil || r.AgentID == "" {
			return "", fmt.Errorf("vai %q chưa làm gì để kiểm tra", role)
		}
		if dir := e.roleDir(ctx, run, r.AgentID); dir != "" {
			csc.Dir = dir
		}
	}
	a, err := e.acts.Propose(ctx, csc, "run_command", cmdline, "Cổng "+g.Name+" của quy trình "+run.def.Name)
	if err != nil {
		return "", err
	}
	status, out := "waiting", fmt.Sprintf("Lệnh %q chờ người dùng duyệt (mã %s); báo họ rồi dừng lượt.", a.Target, a.ID)
	switch a.Status {
	case "done":
		status, out = "passed", "Cổng "+g.Name+" ĐẠT.\n$ "+a.Target+"\n"+truncate(a.Detail, 4000)
	case "failed":
		status, out = "failed", "Cổng "+g.Name+" KHÔNG ĐẠT.\n$ "+a.Target+"\n"+truncate(a.Detail, 4000)
	}
	e.wf.mu.Lock()
	if rg := run.gate(key); rg != nil {
		rg.Status, rg.ActionID, rg.Detail = status, a.ID, truncate(a.Detail, 500)
	}
	run.logf("Cổng %s: %s", g.Name, gateStatus(status))
	rec := run.rec
	e.wf.mu.Unlock()
	e.saveRun(rec)
	return out, nil
}

// roleDir is the worktree an agent of the chat edits in ("" = none).
func (e *Engine) roleDir(ctx context.Context, run *wfRun, agentID string) string {
	if e.trees == nil {
		return ""
	}
	conv, err := e.store.Chat().GetConversation(ctx, run.rec.ConversationID)
	if err != nil {
		return ""
	}
	a, err := e.store.Agents().Get(ctx, agentID)
	if err != nil {
		return ""
	}
	name := e.chatTree(ctx, conv, a)
	if !e.trees.Exists(run.rec.ProjectID, name) {
		return ""
	}
	return e.trees.Path(run.rec.ProjectID, name)
}

// refreshGates reads the decisions of the gates waiting on a person.
func (e *Engine) refreshGates(ctx context.Context, run *wfRun) {
	e.wf.mu.Lock()
	var waiting []storage.RunGate
	for _, g := range run.rec.Gates {
		if g.Status == "waiting" && g.ActionID != "" {
			waiting = append(waiting, g)
		}
	}
	e.wf.mu.Unlock()
	if len(waiting) == 0 {
		return
	}
	changed := false
	for _, g := range waiting {
		a, err := e.store.Actions().Get(ctx, g.ActionID)
		if err != nil {
			continue
		}
		status := ""
		switch a.Status {
		case "done":
			status = "passed"
		case "failed", "rejected":
			status = "failed"
		}
		if status == "" {
			continue
		}
		e.wf.mu.Lock()
		if rg := run.gate(g.Key); rg != nil && rg.Status == "waiting" {
			rg.Status, rg.Detail = status, truncate(a.Detail, 500)
			run.logf("Cổng %s: %s", rg.Name, gateStatus(status))
			changed = true
		}
		e.wf.mu.Unlock()
	}
	if changed {
		e.wf.mu.Lock()
		rec := run.rec
		e.wf.mu.Unlock()
		e.saveRun(rec)
	}
}

func (e *Engine) wfDone(ctx context.Context, sc officetools.Scope, run *wfRun, summary string, outputs map[string]string) (string, error) {
	e.refreshGates(ctx, run)
	e.wf.mu.Lock()
	var missing, working []string
	for _, g := range run.rec.Gates {
		if g.Required && g.Status != "passed" {
			missing = append(missing, g.Name+" ("+gateStatus(g.Status)+")")
		}
	}
	for _, r := range run.rec.Roles {
		if r.Status == "working" {
			working = append(working, r.Name)
		}
	}
	queued := len(e.queuedAsks(sc.RunRef))
	e.wf.mu.Unlock()
	if len(missing) > 0 {
		return "", errors.New("chưa qua cổng bắt buộc: " + strings.Join(missing, ", ") + ". Mở cổng bằng workflow_gate; không qua được thì báo người dùng và hỏi có dừng quy trình không")
	}
	if len(working) > 0 || queued > 0 {
		return "", errors.New("còn vai đang làm hoặc vừa giao: " + strings.Join(working, ", ") + "; đợi xong rồi mới kết thúc")
	}
	if strings.TrimSpace(summary) == "" {
		return "", errors.New("summary là kết quả gửi người gọi quy trình (họ chỉ thấy phần này): hãy viết đầy đủ")
	}
	if err := run.def.CheckOutputs(outputs); err != nil {
		return "", fmt.Errorf("%w (%s); gọi lại workflow_done kèm outputs", err, fieldList(run.def.Outputs))
	}
	e.wf.mu.Lock()
	run.rec.Outputs = outputs
	e.wf.mu.Unlock()
	e.finishRun(run.rec.ConversationID, run.rec.ID, storage.RunDone, strings.TrimSpace(summary), "")
	return "Đã kết thúc quy trình; summary đã gửi tới chat gọi nó. Kết thúc lượt bằng một dòng ngắn.", nil
}

// ---- after a turn ----

// wfAfter runs what follows a turn of a workflow: a coordinator's turn
// starts what it asked; a role's turn is counted, and when the last role it
// waited on is done, the coordinator is called back. role: whether prev was a
// role's turn (nothing else follows it).
func (e *Engine) wfAfter(prev *Turn, conv storage.Conversation, project storage.Repo, reply string) (role bool) {
	if prev.wfRun == "" {
		return false
	}
	if prev.wfRole != "" {
		e.wfRoleDone(prev, conv, project, reply)
		return true
	}
	e.wf.mu.Lock()
	asks := e.wf.pending[prev.ID]
	delete(e.wf.pending, prev.ID)
	run := e.wf.byConv[conv.ID]
	if run == nil || run.rec.ID != prev.wfRun {
		e.wf.mu.Unlock()
		return false
	}
	e.wf.mu.Unlock()
	var started int
	for _, a := range asks {
		if e.startRole(run, conv, project, a) {
			started++
		}
	}
	if len(asks) > 0 && started == 0 {
		e.wfCallBack(run.rec.ID, conv, project)
	}
	return false
}

// startRole starts one turn a coordinator asked for.
func (e *Engine) startRole(run *wfRun, conv storage.Conversation, project storage.Repo, a wfAsk) bool {
	if a.flow {
		return e.startChild(run, conv, project, a)
	}
	d, _ := run.def.Role(a.role)
	level := accessLevel(d.Access)
	if a.vote {
		level = perm.Read
	}
	if run.ceiling != "" {
		level = perm.Min(level, run.ceiling)
	}
	limit := time.Until(run.deadline)
	if limit <= time.Second {
		return false
	}
	e.wf.mu.Lock()
	r := run.role(a.role)
	r.Status = "working"
	run.batch[a.role] = true
	run.rec.Turns++
	r.Turns++
	e.wf.mu.Unlock()
	t, why := e.startTurn(conv, project, turnSpec{agent: a.agent, background: true, delegator: run.coord.ID, actor: run.rec.Actor,
		tier: run.tier, ceiling: level, limit: limit, title: run.coord.Name + " → " + a.agent.Name + " (" + d.Name + ")", prompt: a.prompt,
		wfRun: run.rec.ID, wfRole: a.role, wfVote: a.vote})
	if t == nil {
		e.wf.mu.Lock()
		r := run.role(a.role)
		r.Status = "failed"
		delete(run.batch, a.role)
		run.rec.Turns--
		r.Turns--
		run.notes = append(run.notes, fmt.Sprintf("Vai %s (%s) không bắt đầu được: %s", d.Name, a.agent.Name, cmp.Or(why, "cuộc chat đang bận")))
		if a.vote && run.voting != nil {
			run.voting.ballots[a.role] = "unclear"
		}
		rec := run.rec
		e.wf.mu.Unlock()
		e.saveRun(rec)
		e.note(conv, why)
		return false
	}
	e.wf.mu.Lock()
	rec := run.rec
	e.wf.mu.Unlock()
	e.saveRun(rec)
	e.watchIdle(run, conv, a.role)
	return true
}

// wfRoleDone records a role's answer; the last one of the batch calls the
// coordinator back.
func (e *Engine) wfRoleDone(prev *Turn, conv storage.Conversation, project storage.Repo, reply string) {
	e.wf.mu.Lock()
	run := e.wf.byConv[conv.ID]
	if run == nil || run.rec.ID != prev.wfRun {
		e.wf.mu.Unlock()
		return // stopped, timed out or done meanwhile
	}
	r := run.role(prev.wfRole)
	if r == nil {
		e.wf.mu.Unlock()
		return
	}
	d, _ := run.def.Role(prev.wfRole)
	if prev.err != "" {
		r.Status = "failed"
		run.notes = append(run.notes, fmt.Sprintf("Vai %s (%s) lỗi: %s", d.Name, r.AgentName, truncate(prev.err, 300)))
		run.logf("%s (%s) lỗi", d.Name, r.AgentName)
	} else {
		r.Status = "done"
		r.Result = truncate(reply, 4000)
		run.logf("%s (%s) xong", d.Name, r.AgentName)
	}
	if prev.wfVote && run.voting != nil {
		b := "unclear"
		if prev.err == "" {
			b = ballot(reply)
		}
		run.voting.ballots[prev.wfRole] = b
	}
	delete(run.batch, prev.wfRole)
	last := len(run.batch) == 0
	key := run.rec.ID + "/" + prev.wfRole
	if ch, ok := e.wf.waiting[key]; ok { // its coordinator waits for it (workflow_ask): the answer goes there
		delete(e.wf.waiting, key)
		answer := reply
		if prev.err != "" {
			answer = "(lỗi) " + prev.err
		}
		ch <- answer
		last = last && run.owed // others answered meanwhile: they are told by a call-back
	} else if !last {
		run.owed = true
	}
	id, rec := run.rec.ID, run.rec
	e.wf.mu.Unlock()
	e.saveRun(rec)
	if last {
		e.wfCallBack(id, conv, project)
	}
}

// wfCallBack calls the coordinator back once its roles are done; a chat
// busy with another answer is tried again a little later.
func (e *Engine) wfCallBack(runID string, conv storage.Conversation, project storage.Repo) {
	e.wf.mu.Lock()
	run := e.wf.byConv[conv.ID]
	if run == nil || run.rec.ID != runID || len(run.batch) > 0 {
		e.wf.mu.Unlock()
		return
	}
	var lines []string
	for _, r := range run.rec.Roles {
		if r.Status == "done" || r.Status == "failed" {
			lines = append(lines, fmt.Sprintf("%s (%s): %s", r.Name, r.AgentName, roleStatus(r.Status)))
		}
	}
	notes := slices.Clone(run.notes)
	var vote string
	if run.voting != nil && run.def.Vote != nil {
		var passed bool
		passed, vote = tally(run.def.Vote, run.voting.ballots)
		if passed {
			run.logf("Biểu quyết thông qua")
		} else {
			run.logf("Biểu quyết không thông qua")
		}
	}
	coord, tier, actorOf := run.coord, run.tier, run.rec.Actor
	limit := time.Until(run.deadline)
	e.wf.mu.Unlock()
	if limit <= time.Second {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[office · quy trình %s] Các vai bạn giao đã xong: %s. Câu trả lời của họ là các tin ngay trên trong cuộc chat.\n", run.def.Name, strings.Join(lines, "; ")) // i18n-ignore
	if vote != "" {
		b.WriteString(vote + "\n")
	}
	for _, n := range notes {
		b.WriteString("- " + n + "\n")
	}
	b.WriteString("Làm bước tiếp theo của quy trình; xong hết thì gọi workflow_done.") // i18n-ignore
	t, why := e.startTurn(conv, project, turnSpec{agent: coord, actor: actorOf, tier: tier, ceiling: run.ceiling, limit: limit,
		title: "Quy trình " + run.def.Name + " → " + coord.Name, prompt: b.String(), wfRun: runID})
	if t == nil {
		if why == "" { // the chat is busy: again in a moment
			e.wf.mu.Lock()
			run.tries++
			tries := run.tries
			e.wf.mu.Unlock()
			if tries < 240 {
				time.AfterFunc(5*time.Second, func() { e.wfCallBack(runID, conv, project) })
			}
			return
		}
		e.note(conv, why)
		e.finishRun(conv.ID, runID, storage.RunFailed, "", why)
		return
	}
	e.wf.mu.Lock()
	run.tries, run.notes, run.voting, run.owed = 0, nil, nil, false
	e.wf.mu.Unlock()
}

// wfCost adds a turn's cost to its run.
func (e *Engine) wfCost(turn *Turn, conversationID string, cost *float64) {
	if turn.wfRun == "" || cost == nil {
		return
	}
	e.wf.mu.Lock()
	run := e.wf.byConv[conversationID]
	if run == nil || run.rec.ID != turn.wfRun {
		e.wf.mu.Unlock()
		return
	}
	run.rec.CostUSD += *cost
	if r := run.role(turn.wfRole); r != nil {
		r.CostUSD += *cost
	}
	for r := run; r.parent != nil; r = r.parent { // the runs above it pay for it too, as it spends
		r.parent.rec.CostUSD += *cost
		if pr := r.parent.role(r.parentRole); pr != nil {
			pr.CostUSD += *cost
		}
	}
	e.wf.mu.Unlock()
}

// wfSession is the session a role's turn resumes ("" = a new one).
func (e *Engine) wfSession(turn *Turn, conversationID string) (session, runtime string) {
	if turn.wfRole == "" || turn.wfVote {
		return "", ""
	}
	e.wf.mu.Lock()
	defer e.wf.mu.Unlock()
	if run := e.wf.byConv[conversationID]; run != nil && run.rec.ID == turn.wfRun {
		if r := run.role(turn.wfRole); r != nil {
			return r.SessionID, r.Runtime
		}
	}
	return "", ""
}

// wfKeepSession keeps a role's session for its follow-ups.
func (e *Engine) wfKeepSession(turn *Turn, conversationID, session, runtime string) {
	if turn.wfRole == "" || turn.wfVote || session == "" {
		return
	}
	e.wf.mu.Lock()
	defer e.wf.mu.Unlock()
	if run := e.wf.byConv[conversationID]; run != nil && run.rec.ID == turn.wfRun {
		if r := run.role(turn.wfRole); r != nil {
			r.SessionID, r.Runtime = session, runtime
		}
	}
}

// roleBrief is what a role's turn is told besides its brief.
func roleBrief(name, workflowName string) string {
	return fmt.Sprintf("\n\n## Quy trình\nBạn đang làm vai %s trong quy trình %s, do agent điều phối giao. Làm đúng phần của vai mình rồi trả lời kết quả; không giao việc cho agent khác.\n", name, workflowName) // i18n-ignore
}

// actionsProposer is what the gates propose through.
type actionsProposer interface {
	Propose(ctx context.Context, sc actions.Scope, kind, target, reason string, args ...storage.ActionArgs) (storage.Action, error)
}

// SetActions lets workflows open gates (approval cards, check commands).
func (e *Engine) SetActions(a actionsProposer) { e.acts = a }

// fieldList names declared inputs or outputs for a prompt.
func fieldList(fs []workflow.Field) string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		x := "`" + f.Key + "`"
		if f.Type != "" && f.Type != "string" {
			x += " [" + f.Type + "]"
		}
		if f.Description != "" {
			x += " (" + f.Description + ")"
		}
		if f.Required {
			x += " bắt buộc" // i18n-ignore
		}
		out = append(out, x)
	}
	return strings.Join(out, ", ")
}

// outputsText is a run's outputs as Markdown, after its summary.
func outputsText(rec storage.WorkflowRun) string {
	if len(rec.Outputs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(rec.Outputs))
	for k := range rec.Outputs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("\n\n**Đầu ra**\n") // i18n-ignore
	for _, k := range keys {
		fmt.Fprintf(&b, "- `%s`: %s\n", k, rec.Outputs[k])
	}
	return strings.TrimRight(b.String(), "\n")
}

// wfAskNow asks an analyze role a question and waits for its answer within
// the coordinator's turn (workflow_ask); past the wait it goes on in the
// background and the coordinator is called back as for workflow_delegate.
func (e *Engine) wfAskNow(ctx context.Context, sc officetools.Scope, run *wfRun, role, question string, waitSec int) (string, error) {
	d, ok := run.def.Role(role)
	if !ok {
		return "", fmt.Errorf("quy trình không có vai %q (có: %s)", role, roleKeys(run.def))
	}
	if d.Workflow != "" || d.Access != workflow.AccessAnalyze {
		return "", fmt.Errorf("workflow_ask chỉ dùng cho vai chỉ phân tích (access analyze); vai %s dùng workflow_delegate / workflow_send", d.Name)
	}
	if strings.TrimSpace(question) == "" {
		return "", errors.New("hãy ghi câu hỏi")
	}
	a, err := e.pickAgent(ctx, run, role, "")
	if err != nil {
		return "", err
	}
	conv, err := e.store.Chat().GetConversation(ctx, run.rec.ConversationID)
	if err != nil {
		return "", err
	}
	project, err := e.store.Repos().Get(ctx, run.rec.ProjectID)
	if err != nil {
		return "", err
	}
	e.wf.mu.Lock()
	r := run.role(role)
	if r.Status == "working" {
		e.wf.mu.Unlock()
		return "", fmt.Errorf("vai %s đang làm; đợi xong", d.Name)
	}
	again := r.Status != "idle"
	if again && r.Rounds >= run.def.Limits.Rounds {
		e.wf.mu.Unlock()
		return "", fmt.Errorf("đã hỏi vai %s %d lần (giới hạn); tổng kết với kết quả hiện có", d.Name, r.Rounds)
	}
	if err := e.canAsk(run, sc.RunRef, role, a); err != nil {
		e.wf.mu.Unlock()
		return "", err
	}
	e.wf.mu.Unlock()
	if !again {
		if err := e.checkDiffer(ctx, run, role, a); err != nil {
			return "", err
		}
	}
	e.wf.mu.Lock()
	r = run.role(role)
	if again {
		r.Rounds++
	}
	r.AgentID, r.AgentName = a.ID, a.Name
	key := run.rec.ID + "/" + role
	ch := make(chan string, 1)
	e.wf.waiting[key] = ch
	run.logf("Hỏi %s (%s) và chờ", d.Name, a.Name)
	e.wf.mu.Unlock()
	prompt := fmt.Sprintf("%s (điều phối quy trình %s) hỏi bạn, vai %s:\n%s\n\nTrả lời ngắn gọn, đủ ý. %s", run.coord.Name, run.def.Name, d.Name, strings.TrimSpace(question), workflow.AccessNote(d.Access)) // i18n-ignore
	if !e.startRole(run, conv, project, wfAsk{role: role, agent: a, prompt: prompt}) {
		e.wf.mu.Lock()
		delete(e.wf.waiting, key)
		e.wf.mu.Unlock()
		return "", fmt.Errorf("vai %s không bắt đầu được (chat bận hoặc hết giờ)", d.Name)
	}
	wait := time.Duration(min(max(cmp.Or(waitSec, 60), 10), 120)) * time.Second
	select {
	case answer := <-ch:
		return fmt.Sprintf("%s (vai %s) trả lời:\n%s", a.Name, d.Name, answer), nil
	case <-time.After(wait):
	case <-ctx.Done():
	}
	e.wf.mu.Lock()
	if _, still := e.wf.waiting[key]; still { // not answered yet: as a hand-off from here on
		delete(e.wf.waiting, key)
		e.wf.mu.Unlock()
		return fmt.Sprintf("%s chưa trả lời sau %s; vai làm tiếp ở nền, khi xong bạn được gọi lại (như workflow_delegate). Ghi ngắn rồi dừng lượt.", a.Name, wait), nil
	}
	e.wf.mu.Unlock()
	return fmt.Sprintf("%s (vai %s) trả lời:\n%s", a.Name, d.Name, <-ch), nil
}
