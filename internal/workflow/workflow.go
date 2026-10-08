// Package workflow reads the processes agents work together by (spec
// 2026-10-07-workflows-design): a Markdown file whose YAML header is what the
// office enforces (roles, their permission, limits, the brief, gates, votes)
// and whose body tells the coordinating agent how to run it.
//
// A skill is how one agent works; a workflow is how several work together.
package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Coordinator is the role name of the agent running the workflow (usable in
// differ_from).
const Coordinator = "dieu-phoi"

// Access of a role: the most its turns may do.
const (
	AccessAnalyze = "analyze" // read and answer, no file changes
	AccessPropose = "propose" // proposals and diffs a person decides
	AccessEdit    = "edit"    // edits in its own worktree
)

// Gate kinds.
const (
	GateApprove = "approve" // a person approves on a card
	GateCheck   = "check"   // a check command must pass
)

// Def is one workflow.
type Def struct {
	Key         string     `yaml:"key" json:"key"`
	Name        string     `yaml:"name" json:"name"`
	Description string     `yaml:"description" json:"description"`
	Input       string     `yaml:"input,omitempty" json:"input,omitempty"`
	Roles       []Role     `yaml:"roles" json:"roles"`
	Parallel    [][]string `yaml:"parallel,omitempty" json:"parallel,omitempty"`
	Limits      Limits     `yaml:"limits,omitempty" json:"limits"`
	Brief       []string   `yaml:"brief,omitempty" json:"brief,omitempty"`
	Gates       []Gate     `yaml:"gates,omitempty" json:"gates,omitempty"`
	Vote        *Vote      `yaml:"vote,omitempty" json:"vote,omitempty"`
	// Supervise: a role that checks the run now and then against what was
	// asked; drift is told to the coordinator when it is called back
	Supervise *Supervise `yaml:"supervise,omitempty" json:"supervise,omitempty"`
	// Strict: a differ_from that cannot be met refuses to run (default: a warning).
	Strict bool `yaml:"strict,omitempty" json:"strict,omitempty"`
	// Inputs a caller gives it and outputs it gives back (ADR-103): a
	// sub-workflow's role is given its inputs by name; workflow_done must
	// give every required output.
	Inputs  []Field `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs []Field `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	// Callable: who may run it: "" = a chat (/key) and other workflows,
	// "chat" = only a chat, "sub" = only other workflows (hidden from /).
	Callable string `yaml:"callable,omitempty" json:"callable,omitempty"`
	// Steps: the graph the office runs in order (ADR-108); none = a coordinator decides
	Steps []Step `yaml:"steps,omitempty" json:"steps,omitempty"`
	// Start: the first steps, run at once ("" = the first of Steps)
	Start Targets `yaml:"start,omitempty" json:"start,omitempty"`
	// Body is what the coordinator follows (the file below the header).
	Body string `yaml:"-" json:"body"`
}

// Role is a seat of the workflow, filled by an agent of the project.
type Role struct {
	Key        string   `yaml:"key" json:"key"`
	Name       string   `yaml:"name" json:"name"`
	Hint       string   `yaml:"hint,omitempty" json:"hint,omitempty"`
	Access     string   `yaml:"access,omitempty" json:"access"`
	DifferFrom []string `yaml:"differ_from,omitempty" json:"differ_from,omitempty"`
	// Workflow: the seat is filled by another workflow of the project (a
	// sub-workflow, this one's own key too: recursion, bounded by
	// limits.depth); the agent bound to it coordinates that run.
	Workflow string `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	// Prefer: the kind of agent the seat wants (installing suggests by it).
	Prefer *Prefer `yaml:"prefer,omitempty" json:"prefer,omitempty"`
}

// Field is an input or an output of a workflow.
type Field struct {
	Key         string `yaml:"key" json:"key"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required"`
	// Type of an output: string (default) | number | boolean | list (a JSON
	// array) | json (any JSON); workflow_done is refused when a value is not one
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// FieldTypes are the types an output may have.
var FieldTypes = []string{"string", "number", "boolean", "list", "json"}

// Prefer is what a role wants of its agent: a model tier, a vendor family
// (anthropic, openai, google, or a connection's host).
type Prefer struct {
	Tier   string `yaml:"tier,omitempty" json:"tier,omitempty"`
	Family string `yaml:"family,omitempty" json:"family,omitempty"`
}

// Callable values.
const (
	CallableChat = "chat"
	CallableSub  = "sub"
)

// Limits bound one run.
type Limits struct {
	Rounds    int     `yaml:"rounds,omitempty" json:"rounds"`         // follow-ups (workflow_send) per role
	Turns     int     `yaml:"turns,omitempty" json:"turns"`           // turns of all roles together
	Timeout   string  `yaml:"timeout,omitempty" json:"timeout"`       // the whole run
	BudgetUSD float64 `yaml:"budget_usd,omitempty" json:"budget_usd"` // 0 = none
	Depth     int     `yaml:"depth,omitempty" json:"depth"`           // how deep sub-workflows may go below a run of this one
	// Concurrency: roles answering at once in a run and every run below it
	Concurrency int `yaml:"concurrency,omitempty" json:"concurrency"`
	// Idle: a role working this long without being done is noted ("" = never)
	Idle string `yaml:"idle,omitempty" json:"idle,omitempty"`
}

// Gate is a point the run must pass.
type Gate struct {
	Key      string `yaml:"key" json:"key"`
	Name     string `yaml:"name" json:"name"`
	Kind     string `yaml:"kind" json:"kind"`
	Command  string `yaml:"command,omitempty" json:"command,omitempty"` // check: "" = one of the project's allowed commands
	Required bool   `yaml:"required,omitempty" json:"required"`
}

// Vote is how roles decide together.
type Vote struct {
	Roles  []string `yaml:"roles" json:"roles"`
	Quorum int      `yaml:"quorum" json:"quorum"`
	Veto   []string `yaml:"veto,omitempty" json:"veto,omitempty"`
}

// Supervise is a role watching a run: every so often it reads the run's chat
// and says whether the work still goes where the request asked.
type Supervise struct {
	Role  string `yaml:"role" json:"role"`
	Every string `yaml:"every,omitempty" json:"every,omitempty"` // "" = DefaultSuperviseEvery
}

// DefaultSuperviseEvery is how often a supervisor checks when not said.
const DefaultSuperviseEvery = 10 * time.Minute

// SuperviseEvery is how often the supervisor checks (0 = no supervisor).
func (d Def) SuperviseEvery() time.Duration {
	if d.Supervise == nil || d.Supervise.Role == "" {
		return 0
	}
	t, err := time.ParseDuration(d.Supervise.Every)
	if err != nil || t <= 0 {
		return DefaultSuperviseEvery
	}
	return t
}

// Defaults of the limits.
const (
	DefaultRounds  = 3
	DefaultTurns   = 12
	DefaultTimeout = time.Hour
	MaxTimeout     = 24 * time.Hour
	DefaultDepth   = 2
	MaxDepth       = 5
	// DefaultConcurrency bounds the roles answering at once in a tree of runs.
	DefaultConcurrency = 6
	MaxConcurrency     = 20
)

// BriefFields are the parts a brief may have, in the order they are shown.
var BriefFields = []struct{ Key, Label string }{
	{"outcome", "Kết quả cần đạt"},
	{"question", "Câu hỏi"},
	{"context", "Bối cảnh"},
	{"constraints", "Ràng buộc đã kiểm chứng"},
	{"current_option", "Phương án đang thử (được phép phản biện)"},
	{"tried", "Đã thử và vì sao bỏ"},
	{"files", "File liên quan (tự mở để đọc)"},
	{"done_when", "Tiêu chí xong"},
	{"must_not", "Điều cấm"},
}

func briefLabel(k string) string {
	for _, f := range BriefFields {
		if f.Key == k {
			return f.Label
		}
	}
	return ""
}

var (
	keyRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	frontmatterRe = regexp.MustCompile(`(?s)^\x{FEFF}?---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(\r?\n|$)`)
)

// ValidKey reports whether k can name a workflow or a role.
func ValidKey(k string) bool { return keyRe.MatchString(k) }

// ErrInvalid: a workflow file that does not parse or check (errors.Is).
var ErrInvalid = errors.New("quy trình không hợp lệ")

// InvalidError says what is wrong with a workflow file.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string        { return e.Msg }
func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(format string, a ...any) error { return &InvalidError{Msg: fmt.Sprintf(format, a...)} }

// ErrNoHeader: the file has no YAML header.
var ErrNoHeader = &InvalidError{Msg: "thiếu phần đầu YAML (--- … ---)"}

// Parse reads a workflow file and checks it (Validate).
func Parse(md string) (Def, error) {
	m := frontmatterRe.FindStringSubmatch(md)
	if m == nil {
		return Def{}, ErrNoHeader
	}
	var d Def
	dec := yaml.NewDecoder(bytes.NewReader([]byte(m[1])))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return Def{}, invalid("phần đầu YAML: %v", err)
	}
	d.Body = strings.TrimSpace(md[len(m[0]):])
	d.normalize()
	return d, d.Validate()
}

func (d *Def) normalize() {
	d.Key, d.Name = strings.TrimSpace(d.Key), strings.TrimSpace(d.Name)
	for i := range d.Roles {
		r := &d.Roles[i]
		r.Key, r.Name = strings.TrimSpace(r.Key), strings.TrimSpace(r.Name)
		switch {
		case r.Access == "" && r.Workflow != "":
			r.Access = AccessEdit // a sub-workflow: its own roles' access, no lower cap unless asked
		case r.Access == "":
			r.Access = AccessAnalyze
		}
		if r.Name == "" {
			r.Name = r.Key
		}
	}
	if d.Limits.Rounds == 0 {
		d.Limits.Rounds = DefaultRounds
	}
	if d.Limits.Turns == 0 {
		d.Limits.Turns = DefaultTurns
	}
	if d.Limits.Timeout == "" {
		d.Limits.Timeout = DefaultTimeout.String()
	}
	if d.Limits.Depth == 0 {
		d.Limits.Depth = DefaultDepth
	}
	if d.Limits.Concurrency == 0 {
		d.Limits.Concurrency = DefaultConcurrency
	}
	d.Callable = strings.TrimSpace(d.Callable)
	for i := range d.Roles {
		if p := d.Roles[i].Prefer; p != nil {
			p.Tier, p.Family = strings.ToLower(strings.TrimSpace(p.Tier)), strings.ToLower(strings.TrimSpace(p.Family))
		}
	}
	for i := range d.Gates {
		if d.Gates[i].Name == "" {
			d.Gates[i].Name = d.Gates[i].Key
		}
	}
}

// Timeout is how long a run may take.
func (d Def) Timeout() time.Duration {
	t, err := time.ParseDuration(d.Limits.Timeout)
	if err != nil || t <= 0 {
		return DefaultTimeout
	}
	return min(t, MaxTimeout)
}

// Role finds a role by key.
func (d Def) Role(key string) (Role, bool) {
	i := slices.IndexFunc(d.Roles, func(r Role) bool { return r.Key == key })
	if i < 0 {
		return Role{}, false
	}
	return d.Roles[i], true
}

// Gate finds a gate by key.
func (d Def) Gate(key string) (Gate, bool) {
	i := slices.IndexFunc(d.Gates, func(g Gate) bool { return g.Key == key })
	if i < 0 {
		return Gate{}, false
	}
	return d.Gates[i], true
}

// SameBatch: roles a and b are allowed to run at once.
func (d Def) SameBatch(a, b string) bool {
	for _, g := range d.Parallel {
		if slices.Contains(g, a) && slices.Contains(g, b) {
			return true
		}
	}
	return false
}

// Validate checks a workflow; the error lists every problem.
func (d Def) Validate() error {
	var bad []string
	add := func(f string, a ...any) { bad = append(bad, fmt.Sprintf(f, a...)) }
	if !ValidKey(d.Key) {
		add("key %q: chữ thường, số, gạch ngang, tối đa 40 ký tự", d.Key)
	}
	if d.Name == "" {
		add("thiếu name")
	}
	if d.StepMode() {
		d.validateSteps(add)
	} else {
		if strings.TrimSpace(d.Body) == "" {
			add("thiếu phần hướng dẫn cho agent điều phối (dưới phần đầu YAML)")
		}
		if len(d.Roles) == 0 {
			add("cần ít nhất một vai (roles), hoặc các bước (steps)")
		}
	}
	roles := map[string]bool{}
	for _, r := range d.Roles {
		switch {
		case !ValidKey(r.Key):
			add("vai %q: key không hợp lệ", r.Key)
		case r.Key == Coordinator:
			add("vai %q là tên dành cho agent điều phối", r.Key)
		case roles[r.Key]:
			add("vai %q bị trùng", r.Key)
		}
		roles[r.Key] = true
		if r.Access != AccessAnalyze && r.Access != AccessPropose && r.Access != AccessEdit {
			add("vai %q: access phải là analyze | propose | edit", r.Key)
		}
		if r.Workflow != "" && !ValidKey(r.Workflow) {
			add("vai %q: workflow %q không phải key quy trình", r.Key, r.Workflow)
		}
		if p := r.Prefer; p != nil && p.Tier != "" && p.Tier != "strong" && p.Tier != "balanced" && p.Tier != "fast" {
			add("vai %q: prefer.tier phải là strong | balanced | fast", r.Key)
		}
	}
	for _, r := range d.Roles {
		for _, o := range r.DifferFrom {
			if o == r.Key {
				add("vai %q: differ_from không được là chính nó", r.Key)
			} else if o != Coordinator && !roles[o] {
				add("vai %q: differ_from %q không có", r.Key, o)
			}
		}
	}
	for _, g := range d.Parallel {
		seen := map[string]bool{}
		for _, k := range g {
			if !roles[k] {
				add("parallel: vai %q không có", k)
			}
			if seen[k] {
				add("parallel: vai %q lặp lại trong một nhóm", k)
			}
			seen[k] = true
		}
	}
	for _, k := range d.Brief {
		if briefLabel(k) == "" {
			add("brief: mục %q không có (có: %s)", k, strings.Join(briefKeys(), ", "))
		}
	}
	if d.Limits.Rounds < 0 || d.Limits.Rounds > 20 {
		add("limits.rounds: 0–20")
	}
	if d.Limits.Turns < 1 || d.Limits.Turns > 60 {
		add("limits.turns: 1–60")
	}
	if t, err := time.ParseDuration(d.Limits.Timeout); err != nil || t <= 0 || t > MaxTimeout {
		add("limits.timeout %q: thời lượng như 30m, 2h (tối đa 24h)", d.Limits.Timeout)
	}
	if d.Limits.Depth < 0 || d.Limits.Depth > MaxDepth {
		add("limits.depth: 0–%d", MaxDepth)
	}
	if d.Limits.Concurrency < 1 || d.Limits.Concurrency > MaxConcurrency {
		add("limits.concurrency: 1–%d", MaxConcurrency)
	}
	if d.Limits.Idle != "" {
		if t, err := time.ParseDuration(d.Limits.Idle); err != nil || t < time.Minute {
			add("limits.idle %q: thời lượng như 10m (ít nhất 1m)", d.Limits.Idle)
		}
	}
	if d.Callable != "" && d.Callable != CallableChat && d.Callable != CallableSub {
		add("callable: chat | sub (bỏ trống = cả hai)")
	}
	for _, fs := range []struct {
		name string
		list []Field
	}{{"inputs", d.Inputs}, {"outputs", d.Outputs}} {
		seen := map[string]bool{}
		for _, f := range fs.list {
			if !ValidKey(strings.ReplaceAll(f.Key, "_", "-")) || seen[f.Key] {
				add("%s: key %q không hợp lệ hoặc bị trùng", fs.name, f.Key)
			}
			if f.Type != "" && !slices.Contains(FieldTypes, f.Type) {
				add("%s: %q có type %q (%s)", fs.name, f.Key, f.Type, strings.Join(FieldTypes, " | "))
			}
			seen[f.Key] = true
		}
	}
	if d.Limits.BudgetUSD < 0 {
		add("limits.budget_usd không được âm")
	}
	gates := map[string]bool{}
	for _, g := range d.Gates {
		if !ValidKey(g.Key) || gates[g.Key] {
			add("cổng %q: key không hợp lệ hoặc bị trùng", g.Key)
		}
		gates[g.Key] = true
		if g.Kind != GateApprove && g.Kind != GateCheck {
			add("cổng %q: kind phải là approve | check", g.Key)
		}
	}
	if v := d.Vote; v != nil {
		if len(v.Roles) < 2 {
			add("vote: cần ít nhất 2 vai")
		}
		for _, k := range v.Roles {
			if !roles[k] {
				add("vote: vai %q không có", k)
			}
		}
		if v.Quorum < 1 || v.Quorum > len(v.Roles) {
			add("vote.quorum: 1–%d", len(v.Roles))
		}
		for _, k := range v.Veto {
			if !slices.Contains(v.Roles, k) {
				add("vote.veto: %q không nằm trong vote.roles", k)
			}
		}
	}
	if sv := d.Supervise; sv != nil {
		r, ok := d.Role(sv.Role)
		switch {
		case !ok:
			add("supervise.role: vai %q không có", sv.Role)
		case r.Access != AccessAnalyze || r.Workflow != "":
			add("supervise.role: vai %q phải là vai analyze (chỉ đọc), không phải quy trình con", sv.Role)
		}
		if sv.Every != "" {
			if t, err := time.ParseDuration(sv.Every); err != nil || t < 2*time.Minute {
				add("supervise.every %q: thời lượng như 10m (ít nhất 2m)", sv.Every)
			}
		}
		if d.StepMode() && !slices.ContainsFunc(d.Steps, func(s Step) bool { return s.Type == StepCoordinate }) {
			add("supervise: chỉ giám sát được bước coordinate (quy trình không có bước nào như vậy)")
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return &InvalidError{Msg: strings.Join(bad, "; ")}
}

func briefKeys() []string {
	out := make([]string, 0, len(BriefFields))
	for _, f := range BriefFields {
		out = append(out, f.Key)
	}
	return out
}

// RenderBrief renders what a role gets from the coordinator; a part the
// workflow requires and that is empty is an error naming every missing one.
func (d Def) RenderBrief(role Role, parts map[string]string) (string, error) {
	var missing []string
	for _, k := range d.Brief {
		if strings.TrimSpace(parts[k]) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("bản giao việc thiếu: %s (quy trình %s bắt buộc: %s)", strings.Join(missing, ", "), d.Key, strings.Join(d.Brief, ", "))
	}
	for k := range parts {
		if briefLabel(k) == "" {
			return "", fmt.Errorf("bản giao việc có mục %q không có (có: %s)", k, strings.Join(briefKeys(), ", "))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Bạn làm vai **%s** trong quy trình **%s**.\n", role.Name, d.Name)
	for _, f := range BriefFields {
		if v := strings.TrimSpace(parts[f.Key]); v != "" {
			fmt.Fprintf(&b, "\n## %s\n%s\n", f.Label, v)
		}
	}
	b.WriteString("\n" + AccessNote(role.Access))
	return b.String(), nil
}

// AccessNote is the line a role's prompt ends with.
func AccessNote(access string) string {
	switch access {
	case AccessEdit:
		return "Bạn được sửa file trong worktree riêng của mình. Làm xong thì trả lời ngắn: đã làm gì, kiểm chứng ra sao, còn gì dở."
	case AccessPropose:
		return "Bạn không sửa file trực tiếp: đề xuất diff hoặc thao tác để người dùng duyệt. Làm xong thì trả lời ngắn kết quả."
	}
	return "Chỉ phân tích, không sửa file, không viết code. Trả lời kết luận kèm lý do."
}

// IdleAfter is how long a role may work before it is noted (0 = never).
func (d Def) IdleAfter() time.Duration {
	t, err := time.ParseDuration(d.Limits.Idle)
	if err != nil {
		return 0
	}
	return t
}

// RenderInputs is a sub-workflow's input from the values its caller gave,
// by the inputs it declares; a required one missing is an error.
func (d Def) RenderInputs(values map[string]string) (string, error) {
	var b strings.Builder
	var missing []string
	for _, f := range d.Inputs {
		v := strings.TrimSpace(values[f.Key])
		if v == "" {
			if f.Required {
				missing = append(missing, f.Key)
			}
			continue
		}
		fmt.Fprintf(&b, "### %s\n%s\n\n", cmpOr(f.Description, f.Key), v)
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("quy trình /%s cần đầu vào: %s (ghi trong brief theo đúng key)", d.Key, strings.Join(missing, ", "))
	}
	for k, v := range values { // what it does not declare, kept as context
		if !slices.ContainsFunc(d.Inputs, func(f Field) bool { return f.Key == k }) && strings.TrimSpace(v) != "" {
			fmt.Fprintf(&b, "### %s\n%s\n\n", k, strings.TrimSpace(v))
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// CheckOutputs: every required output is there; the error names those missing.
func (d Def) CheckOutputs(values map[string]string) error {
	var bad []string
	for _, f := range d.Outputs {
		v := strings.TrimSpace(values[f.Key])
		if v == "" {
			if f.Required {
				bad = append(bad, f.Key+" (thiếu)")
			}
			continue
		}
		if !typeOK(f.Type, v) {
			bad = append(bad, fmt.Sprintf("%s (phải là %s)", f.Key, f.Type))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("đầu ra không hợp lệ trong outputs: %s", strings.Join(bad, ", "))
	}
	return nil
}

// typeOK: v is a value of an output of type t.
func typeOK(t, v string) bool {
	switch t {
	case "number":
		_, err := strconv.ParseFloat(v, 64)
		return err == nil
	case "boolean":
		_, err := strconv.ParseBool(v)
		return err == nil
	case "list":
		var a []any
		return json.Unmarshal([]byte(v), &a) == nil
	case "json":
		return json.Valid([]byte(v))
	}
	return true
}

// Format is a workflow's file from its definition (the editor's canvas
// changes the definition; the file stays the source).
func Format(d Def) (string, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return "", err
	}
	_ = enc.Close()
	body := strings.TrimSpace(d.Body)
	if body == "" && d.StepMode() {
		body = "Quy trình chạy theo các bước ở phần đầu (steps)."
	}
	return "---\n" + b.String() + "---\n" + body + "\n", nil
}
