package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Steps (ADR-108): a workflow may be a graph of steps the office runs in
// order, as n8n does, instead of a coordinator deciding: an agent's turn, a
// sub-workflow, code, an HTTP request, a condition (and loops by going back),
// a person's approval, a check command, the end. Data goes from step to
// step by templates: {{input.key}}, {{steps.<id>.output}},
// {{steps.<id>.json.a.b}}, {{steps.<id>.status}}.

// Step types.
const (
	StepAgent     = "agent"
	StepWorkflow  = "workflow"
	StepCode      = "code"
	StepHTTP      = "http"
	StepCondition = "condition"
	StepApprove   = "approve"
	StepCheck     = "check"
	StepEnd       = "end"
)

// StepTypes are the step types, in the order the editor offers them.
var StepTypes = []string{StepAgent, StepWorkflow, StepCode, StepHTTP, StepCondition, StepApprove, StepCheck, StepEnd}

// Step is one node of a workflow's graph.
type Step struct {
	ID   string `yaml:"id" json:"id"`
	Type string `yaml:"type" json:"type"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Next is the step after this one (approve: once approved; condition: Then/Else)
	Next string `yaml:"next,omitempty" json:"next,omitempty"`
	// OnError: "stop" (default) ends the run when the step fails; "continue" goes on to Next
	OnError string `yaml:"on_error,omitempty" json:"on_error,omitempty"`

	// agent: a role of the workflow (the project binds it to an agent) and what it is asked
	Role   string `yaml:"role,omitempty" json:"role,omitempty"`
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	// workflow: another workflow of the project, given inputs by key
	Workflow string            `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Inputs   map[string]string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	// code: a script (bash | node | python) run in the project's folder; it
	// gets the run's data as JSON on stdin and in OFFICE_* variables
	Lang     string `yaml:"lang,omitempty" json:"lang,omitempty"`
	Script   string `yaml:"script,omitempty" json:"script,omitempty"`
	TimeoutS int    `yaml:"timeout_s,omitempty" json:"timeout_s,omitempty"`
	// http: a request; the response body is its output (and json when it is JSON)
	Method  string            `yaml:"method,omitempty" json:"method,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty" json:"body,omitempty"`
	// condition: If (see Eval) chooses Then or Else. Going back to an
	// earlier step is a loop: any step runs at most MaxLoops+1 times (default 5)
	If       string `yaml:"if,omitempty" json:"if,omitempty"`
	Then     string `yaml:"then,omitempty" json:"then,omitempty"`
	Else     string `yaml:"else,omitempty" json:"else,omitempty"`
	MaxLoops int    `yaml:"max_loops,omitempty" json:"max_loops,omitempty"`
	// approve: what the person decides (rejected: Else, or the run stops)
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
	// check: a command that must pass (the project's command rules apply)
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
	// end: the run's summary and outputs (templates)
	Summary string            `yaml:"summary,omitempty" json:"summary,omitempty"`
	Outputs map[string]string `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	// Position on the editor's canvas (the office does not read it)
	Position *Position `yaml:"position,omitempty" json:"position,omitempty"`
}

// Position is a step's place on the canvas.
type Position struct {
	X float64 `yaml:"x" json:"x"`
	Y float64 `yaml:"y" json:"y"`
}

// DefaultMaxLoops bounds a condition's way back.
const DefaultMaxLoops = 5

// MaxStepsRun bounds the steps one run takes, loops included.
const MaxStepsRun = 200

// StepMode: the workflow is a graph of steps (no coordinator).
func (d Def) StepMode() bool { return len(d.Steps) > 0 }

// Step finds a step by id.
func (d Def) Step(id string) (Step, bool) {
	i := slices.IndexFunc(d.Steps, func(s Step) bool { return s.ID == id })
	if i < 0 {
		return Step{}, false
	}
	return d.Steps[i], true
}

var langs = []string{"bash", "node", "python"}
var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// validateSteps adds the problems of a graph of steps.
func (d Def) validateSteps(add func(string, ...any)) {
	ids := map[string]bool{}
	for _, s := range d.Steps {
		if !ValidKey(s.ID) || ids[s.ID] {
			add("bước %q: id không hợp lệ hoặc bị trùng", s.ID)
		}
		ids[s.ID] = true
	}
	ref := func(s Step, field, to string, need bool) {
		switch {
		case to == "" && need:
			add("bước %q: thiếu %s", s.ID, field)
		case to != "" && !ids[to]:
			add("bước %q: %s %q không có", s.ID, field, to)
		}
	}
	ends := 0
	for _, s := range d.Steps {
		if !slices.Contains(StepTypes, s.Type) {
			add("bước %q: type phải là %s", s.ID, strings.Join(StepTypes, " | "))
			continue
		}
		if s.MaxLoops < 0 || s.MaxLoops > 50 {
			add("bước %q: max_loops 0–50", s.ID)
		}
		if s.OnError != "" && s.OnError != "stop" && s.OnError != "continue" {
			add("bước %q: on_error là stop | continue", s.ID)
		}
		switch s.Type {
		case StepAgent:
			if _, ok := d.Role(s.Role); !ok {
				add("bước %q: role %q không có trong roles", s.ID, s.Role)
			}
			if strings.TrimSpace(s.Prompt) == "" {
				add("bước %q: thiếu prompt", s.ID)
			}
		case StepWorkflow:
			if !ValidKey(s.Workflow) {
				add("bước %q: thiếu workflow (key quy trình)", s.ID)
			}
		case StepCode:
			if !slices.Contains(langs, s.Lang) {
				add("bước %q: lang là bash | node | python", s.ID)
			}
			if strings.TrimSpace(s.Script) == "" {
				add("bước %q: thiếu script", s.ID)
			}
		case StepHTTP:
			if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "{{") {
				add("bước %q: url phải bắt đầu bằng http:// hoặc https://", s.ID)
			}
			if s.Method != "" && !slices.Contains(methods, strings.ToUpper(s.Method)) {
				add("bước %q: method là %s", s.ID, strings.Join(methods, " | "))
			}
		case StepCondition:
			if strings.TrimSpace(s.If) == "" {
				add("bước %q: thiếu if", s.ID)
			}
			ref(s, "then", s.Then, true)
			ref(s, "else", s.Else, true)
		case StepApprove:
			if strings.TrimSpace(s.Note) == "" {
				add("bước %q: thiếu note (điều người dùng duyệt)", s.ID)
			}
			ref(s, "else", s.Else, false)
		case StepCheck:
			if strings.TrimSpace(s.Command) == "" {
				add("bước %q: thiếu command", s.ID)
			}
			ref(s, "else", s.Else, false)
		case StepEnd:
			ends++
		}
		if s.Type != StepEnd && s.Type != StepCondition {
			ref(s, "next", s.Next, true)
		}
	}
	if ends == 0 {
		add("cần ít nhất một bước end")
	}
}

// StepResult is what a step left for the steps after it.
type StepResult struct {
	Output string `json:"output"`
	Status string `json:"status,omitempty"` // http: the status code; check/approve: passed | failed
	JSON   any    `json:"json,omitempty"`   // the output parsed, when it is JSON
}

// Vars are the data templates read.
type Vars struct {
	Input map[string]string     `json:"input"`
	Steps map[string]StepResult `json:"steps"`
}

// NewResult is out as a step's result (its JSON too, when it is JSON).
func NewResult(out, status string) StepResult {
	r := StepResult{Output: out, Status: status}
	var v any
	if t := strings.TrimSpace(out); (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Unmarshal([]byte(t), &v) == nil {
		r.JSON = v
	}
	return r
}

var tplRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.\-]+)\s*\}\}`)

// Render fills a template's {{…}} from vars ("" for what is not there).
func Render(tpl string, v Vars) string {
	return tplRe.ReplaceAllStringFunc(tpl, func(m string) string {
		return lookup(tplRe.FindStringSubmatch(m)[1], v)
	})
}

func lookup(path string, v Vars) string {
	parts := strings.Split(path, ".")
	switch {
	case parts[0] == "input" && len(parts) == 2:
		return v.Input[parts[1]]
	case parts[0] == "steps" && len(parts) >= 3:
		r, ok := v.Steps[parts[1]]
		if !ok {
			return ""
		}
		switch parts[2] {
		case "output":
			return r.Output
		case "status":
			return r.Status
		case "json":
			var cur any = r.JSON
			for _, p := range parts[3:] {
				switch x := cur.(type) {
				case map[string]any:
					cur = x[p]
				case []any:
					i, err := strconv.Atoi(p)
					if err != nil || i < 0 || i >= len(x) {
						return ""
					}
					cur = x[i]
				default:
					return ""
				}
			}
			switch x := cur.(type) {
			case nil:
				return ""
			case string:
				return x
			default:
				b, _ := json.Marshal(x)
				return string(b)
			}
		}
	}
	return ""
}

var condOps = []string{" contains ", " == ", " != ", " >= ", " <= ", " > ", " < "}

// Eval reads a condition: "A op B" (op: == != > < >= <= contains; numbers
// compare as numbers) or one value, true unless empty, false, 0 or null.
// Both sides are templates.
func Eval(expr string, v Vars) bool {
	for _, op := range condOps {
		l, r, ok := strings.Cut(expr, op)
		if !ok {
			continue
		}
		a, b := unquote(Render(l, v)), unquote(Render(r, v))
		fa, ea := strconv.ParseFloat(a, 64)
		fb, eb := strconv.ParseFloat(b, 64)
		num := ea == nil && eb == nil
		switch strings.TrimSpace(op) {
		case "contains":
			return strings.Contains(a, b)
		case "==":
			return a == b || num && fa == fb
		case "!=":
			return !(a == b || num && fa == fb)
		case ">":
			return num && fa > fb || !num && a > b
		case "<":
			return num && fa < fb || !num && a < b
		case ">=":
			return num && fa >= fb || !num && a >= b
		case "<=":
			return num && fa <= fb || !num && a <= b
		}
	}
	s := strings.ToLower(unquote(Render(expr, v)))
	return s != "" && s != "false" && s != "0" && s != "null"
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// MaxHTTPBody is how much of a response an http step keeps.
const MaxHTTPBody = 256 << 10

// DoHTTP runs an http step (already rendered): the status code and the body.
func DoHTTP(ctx context.Context, s Step) (int, string, error) {
	method := strings.ToUpper(s.Method)
	if method == "" {
		method = http.MethodGet
	}
	timeout := time.Duration(s.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, min(timeout, 2*time.Minute))
	defer cancel()
	var body io.Reader
	if s.Body != "" {
		body = strings.NewReader(s.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.URL, body)
	if err != nil {
		return 0, "", err
	}
	for k, v := range s.Headers {
		req.Header.Set(k, v)
	}
	if s.Body != "" && req.Header.Get("Content-Type") == "" && json.Valid([]byte(s.Body)) {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxHTTPBody))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(raw), nil
}

// ErrStepFailed: a step did not do what it should (its output says why).
var ErrStepFailed = errors.New("bước không thành công")

// StepLabel names a step for the log and the run's chat.
func StepLabel(s Step) string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("%s (%s)", s.ID, s.Type)
}
