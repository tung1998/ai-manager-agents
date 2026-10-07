// Package workflow reads the processes agents work together by (spec
// 2026-10-07-workflows-design): a Markdown file whose YAML header is what the
// office enforces (roles, their permission, limits, the brief, gates, votes)
// and whose body tells the coordinating agent how to run it.
//
// A skill is how one agent works; a workflow is how several work together.
package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
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
	// Strict: a differ_from that cannot be met refuses to run (default: a warning).
	Strict bool `yaml:"strict,omitempty" json:"strict,omitempty"`
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
}

// Limits bound one run.
type Limits struct {
	Rounds    int     `yaml:"rounds,omitempty" json:"rounds"`         // follow-ups (workflow_send) per role
	Turns     int     `yaml:"turns,omitempty" json:"turns"`           // turns of all roles together
	Timeout   string  `yaml:"timeout,omitempty" json:"timeout"`       // the whole run
	BudgetUSD float64 `yaml:"budget_usd,omitempty" json:"budget_usd"` // 0 = none
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

// Defaults of the limits.
const (
	DefaultRounds  = 3
	DefaultTurns   = 12
	DefaultTimeout = time.Hour
	MaxTimeout     = 24 * time.Hour
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
		if r.Access == "" {
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
	if strings.TrimSpace(d.Body) == "" {
		add("thiếu phần hướng dẫn cho agent điều phối (dưới phần đầu YAML)")
	}
	if len(d.Roles) == 0 {
		add("cần ít nhất một vai (roles)")
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
