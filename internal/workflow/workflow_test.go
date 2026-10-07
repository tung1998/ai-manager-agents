package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuiltinsParse(t *testing.T) {
	list, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 3 || list[0].Def.Key != "handoff" || list[1].Def.Key != "advisor" || list[2].Def.Key != "council" {
		t.Fatalf("builtins = %d, first %v", len(list), list[0].Def.Key)
	}
	for _, b := range list {
		if b.Def.Body == "" || len(b.Def.Roles) == 0 {
			t.Errorf("%s: empty body or roles", b.Def.Key)
		}
	}
}

const good = `---
key: thu
name: Thử
description: d
roles:
  - key: a
    access: analyze
  - key: b
    access: edit
    differ_from: [a, dieu-phoi]
parallel: [[a, b]]
brief: [outcome, done_when]
gates:
  - key: ok
    kind: check
vote: { roles: [a, b], quorum: 2, veto: [b] }
---
Làm đi.
`

func TestParseDefaults(t *testing.T) {
	d, err := Parse(good)
	if err != nil {
		t.Fatal(err)
	}
	if d.Limits.Rounds != DefaultRounds || d.Limits.Turns != DefaultTurns || d.Timeout() != time.Hour {
		t.Fatalf("limits = %+v", d.Limits)
	}
	if r, _ := d.Role("a"); r.Name != "a" {
		t.Fatalf("role name default = %q", r.Name)
	}
	if !d.SameBatch("a", "b") || d.SameBatch("a", "x") {
		t.Fatal("SameBatch")
	}
	if d.Body != "Làm đi." {
		t.Fatalf("body = %q", d.Body)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no header":    "chỉ có chữ",
		"unknown":      strings.Replace(good, "name: Thử", "name: Thử\nfoo: 1", 1),
		"bad access":   strings.Replace(good, "access: analyze", "access: root", 1),
		"self differ":  strings.Replace(good, "differ_from: [a, dieu-phoi]", "differ_from: [b]", 1),
		"bad parallel": strings.Replace(good, "[[a, b]]", "[[a, z]]", 1),
		"bad brief":    strings.Replace(good, "[outcome, done_when]", "[outcome, foo]", 1),
		"check kind":   strings.Replace(good, "kind: check", "kind: maybe", 1),
		"quorum":       strings.Replace(good, "quorum: 2", "quorum: 3", 1),
		"no body":      strings.TrimSuffix(good, "Làm đi.\n"),
		"coordinator":  strings.Replace(good, "- key: a\n", "- key: dieu-phoi\n", 1),
	}
	for name, src := range cases {
		if _, err := Parse(src); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRenderBrief(t *testing.T) {
	d, _ := Parse(good)
	r, _ := d.Role("a")
	if _, err := d.RenderBrief(r, map[string]string{"outcome": "x"}); err == nil || !strings.Contains(err.Error(), "done_when") {
		t.Fatalf("missing part err = %v", err)
	}
	if _, err := d.RenderBrief(r, map[string]string{"outcome": "x", "done_when": "y", "bogus": "z"}); err == nil {
		t.Fatal("unknown part accepted")
	}
	out, err := d.RenderBrief(r, map[string]string{"outcome": "Đăng nhập được", "done_when": "test xanh"})
	if err != nil || !strings.Contains(out, "Kết quả cần đạt") || !strings.Contains(out, "Chỉ phân tích") {
		t.Fatalf("brief = %q, %v", out, err)
	}
}

func TestLibrary(t *testing.T) {
	l := Library{Dir: filepath.Join(t.TempDir(), "workflows")}
	n, err := l.Seed()
	if err != nil || n < 3 {
		t.Fatalf("Seed = %d, %v", n, err)
	}
	if err := l.Delete("advisor"); err != nil {
		t.Fatal(err)
	}
	if n, _ := l.Seed(); n != 0 {
		t.Fatalf("a deleted builtin came back: %d", n)
	}
	if _, err := l.Get("advisor"); err != ErrNotFound {
		t.Fatalf("Get deleted = %v", err)
	}
	if _, err := l.Reset("advisor"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Save("khac", good); err == nil {
		t.Fatal("key mismatch accepted")
	}
	if _, err := l.Save("thu", good); err != nil {
		t.Fatal(err)
	}
	it, err := l.Get("handoff")
	if err != nil || !it.Builtin || it.Modified {
		t.Fatalf("handoff = %+v, %v", it, err)
	}
	_ = os.WriteFile(filepath.Join(l.Dir, "hong.md"), []byte("---\nkey: hong\n---\n"), 0o644)
	list, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	var broken bool
	for _, x := range list {
		if x.Def.Key == "hong" && x.Error != "" {
			broken = true
		}
	}
	if !broken {
		t.Fatal("a broken file is not listed with its error")
	}
}

// a library seeded under the old Vietnamese keys moves to the English ones,
// keeping what was changed
func TestSeedRenamesOldKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(BuiltinSource("advisor"), "key: advisor", "key: co-van", 1) + "\nghi chú riêng\n"
	if err := os.WriteFile(filepath.Join(dir, "co-van.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".seeded"), []byte("co-van\ngiao-lai\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := Library{Dir: dir}
	if _, err := l.Seed(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "co-van.md")); !os.IsNotExist(err) {
		t.Fatal("old file kept")
	}
	it, err := l.Get("advisor")
	if err != nil || it.Error != "" || !strings.Contains(it.Source, "ghi chú riêng") || !it.Modified {
		t.Fatalf("advisor = %+v, %v", it, err)
	}
	// giao-lai was seeded once and deleted on purpose: handoff stays deleted
	if _, err := l.Get("handoff"); err != ErrNotFound {
		t.Fatalf("handoff came back: %v", err)
	}
}

func TestParseSubWorkflowRole(t *testing.T) {
	d, err := Parse("---\nkey: cha\nname: Cha\nroles:\n  - key: con\n    workflow: advisor\n  - key: ban\n---\nx\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.Roles[0].Access != AccessEdit || d.Roles[1].Access != AccessAnalyze || d.Limits.Depth != DefaultDepth {
		t.Fatalf("defaults = %+v %+v", d.Roles, d.Limits)
	}
	if _, err := Parse("---\nkey: cha\nname: Cha\nroles:\n  - key: con\n    workflow: Bad Key\nlimits:\n  depth: 9\n---\nx\n"); err == nil || !strings.Contains(err.Error(), "workflow") || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("bad sub-workflow: %v", err)
	}
}

func TestParseContract(t *testing.T) {
	d, err := Parse("---\nkey: a\nname: A\ncallable: sub\ninputs:\n  - key: q\n    required: true\noutputs:\n  - key: r\nroles:\n  - key: x\n    prefer: { tier: Strong, family: OpenAI }\nlimits: { idle: 10m }\n---\nx\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.Roles[0].Prefer.Tier != "strong" || d.Roles[0].Prefer.Family != "openai" || d.Limits.Concurrency != DefaultConcurrency || d.IdleAfter() != 10*time.Minute {
		t.Fatalf("def = %+v", d)
	}
	if _, err := d.RenderInputs(map[string]string{}); err == nil {
		t.Fatal("a required input missing")
	}
	if in, err := d.RenderInputs(map[string]string{"q": "Q?", "extra": "E"}); err != nil || !strings.Contains(in, "Q?") || !strings.Contains(in, "E") {
		t.Fatalf("inputs = %q, %v", in, err)
	}
	if _, err := Parse("---\nkey: a\nname: A\ncallable: nope\nroles:\n  - key: x\n    prefer: { tier: huge }\nlimits: { idle: 5s, concurrency: 99 }\n---\nx\n"); err == nil ||
		!strings.Contains(err.Error(), "callable") || !strings.Contains(err.Error(), "prefer.tier") || !strings.Contains(err.Error(), "idle") || !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("bad contract: %v", err)
	}
}

func TestOutputTypes(t *testing.T) {
	d, err := Parse("---\nkey: a\nname: A\noutputs:\n  - { key: n, type: number, required: true }\n  - { key: ok, type: boolean }\n  - { key: files, type: list }\n  - { key: raw, type: json }\nroles:\n  - key: x\n---\nx\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CheckOutputs(map[string]string{"n": "3.5", "ok": "true", "files": `["a.go"]`, "raw": `{"a":1}`}); err != nil {
		t.Fatal(err)
	}
	err = d.CheckOutputs(map[string]string{"n": "ba", "ok": "có", "files": "a.go", "raw": "{"})
	for _, k := range []string{"n (phải là number)", "ok (phải là boolean)", "files (phải là list)", "raw (phải là json)"} {
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Fatalf("missing %q in %v", k, err)
		}
	}
	if _, err := Parse("---\nkey: a\nname: A\noutputs:\n  - { key: n, type: date }\nroles:\n  - key: x\n---\nx\n"); err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("bad type: %v", err)
	}
}

// a library file still as an earlier version shipped it takes the new one;
// one changed here is kept
func TestSeedUpdatesUntouchedBuiltins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workflows")
	l := Library{Dir: dir}
	if _, err := l.Seed(); err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(BuiltinSource("advisor"), "Cố vấn", "Cố vấn cũ", 1)
	mine := strings.Replace(BuiltinSource("council"), "Hội đồng", "Hội đồng của tôi", 1)
	os.WriteFile(filepath.Join(dir, "advisor.md"), []byte(old), 0o644)
	os.WriteFile(filepath.Join(dir, "council.md"), []byte(mine), 0o644)
	defer func(m map[string][]string) { shippedBefore = m }(shippedBefore)
	shippedBefore = map[string][]string{"advisor": {Hash(old)}}
	if n, err := l.Seed(); err != nil || n != 1 {
		t.Fatalf("seed = %d, %v", n, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "advisor.md")); string(raw) != BuiltinSource("advisor") {
		t.Fatal("the untouched old version was not updated")
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "council.md")); string(raw) != mine {
		t.Fatal("a changed one was overwritten")
	}
}

func TestStepsParseRenderEval(t *testing.T) {
	src := "---\nkey: s\nname: S\nroles:\n  - key: a\nsteps:\n  - { id: x, type: agent, role: a, prompt: 'hi {{input.ten}}', next: c }\n  - { id: c, type: condition, if: '{{steps.x.json.n}} > 2', then: e, else: x }\n  - { id: e, type: end }\n---\n"
	d, err := Parse(src)
	if err != nil || !d.StepMode() {
		t.Fatalf("parse: %v", err)
	}
	v := Vars{Input: map[string]string{"ten": "An"}, Steps: map[string]StepResult{"x": NewResult(`{"n": 3, "l": ["p", "q"]}`, "")}}
	if Render(d.Steps[0].Prompt, v) != "hi An" || Render("{{steps.x.json.l.1}}|{{steps.nope.output}}", v) != "q|" {
		t.Fatal("render")
	}
	for expr, want := range map[string]bool{"{{steps.x.json.n}} > 2": true, "{{steps.x.json.n}} == 3.0": true, "{{input.ten}} contains A": true, "{{input.ten}} != An": false, "{{steps.nope.output}}": false, "{{input.ten}}": true} {
		if Eval(expr, v) != want {
			t.Fatalf("eval %q", expr)
		}
	}
	// the canvas writes the file back from the definition: it parses the same
	out, err := Format(d)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := Parse(out); err != nil || len(again.Steps) != 3 || !slices.Equal(again.Steps[1].Else, Targets{"x"}) {
		t.Fatalf("format round trip: %v\n%s", err, out)
	}
	bad := "---\nkey: s\nname: S\nsteps:\n  - { id: x, type: agent, role: nope }\n  - { id: h, type: http, url: ftp://x, next: zz }\n  - { id: k, type: code, lang: ruby }\n---\n"
	_, err = Parse(bad)
	for _, want := range []string{`role "nope"`, "thiếu prompt", "url", `next "zz"`, "lang", "thiếu script", "bước end"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q in %v", want, err)
		}
	}
}

func TestParseCall(t *testing.T) {
	for _, c := range []struct {
		in, key, rest string
		ok            bool
	}{
		{"#review fix the bug", "review", "fix the bug", true},
		{"#review", "review", "", true},
		{"# heading", "", "# heading", false},
		{"/review x", "", "/review x", false},
		{"see #review", "", "see #review", false},
	} {
		k, r, ok := ParseCall(c.in)
		if k != c.key || r != c.rest || ok != c.ok {
			t.Errorf("ParseCall(%q) = %q %q %v", c.in, k, r, ok)
		}
	}
	if Call("review", " x ") != "#review x" || Call("review", "") != "#review" {
		t.Error("Call")
	}
}

// next/then/else take one step or a list; a switch picks its case; a join
// is found by Reaches; an id with "_" says why it is refused
func TestStepTargetsAndSwitch(t *testing.T) {
	src := `---
key: nhanh
name: Nhánh
start: [a, b]
steps:
  - { id: a, type: code, lang: bash, script: "echo a", next: [c, d] }
  - { id: b, type: code, lang: bash, script: "echo b", next: c }
  - id: c
    type: switch
    value: "{{steps.a.output}}"
    cases:
      - { when: "ok", next: [d, e] }
    else: e
  - { id: d, type: end }
  - { id: e, type: end }
---
`
	d, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Starts(), []string{"a", "b"}) || !slices.Equal(d.Steps[0].Next, Targets{"c", "d"}) || !slices.Equal(d.Steps[1].Next, Targets{"c"}) {
		t.Fatalf("targets = %v %v %v", d.Start, d.Steps[0].Next, d.Steps[1].Next)
	}
	if !d.Reaches("a", "e") || d.Reaches("d", "a") || d.Reaches("c", "b") {
		t.Fatal("reaches")
	}
	v := Vars{Steps: map[string]StepResult{"a": {Output: " OK "}}}
	if got, next := d.Steps[2].Pick(v); got != "OK" || !slices.Equal(next, Targets{"d", "e"}) {
		t.Fatalf("pick = %q %v", got, next)
	}
	v.Steps["a"] = StepResult{Output: "khác"}
	if _, next := d.Steps[2].Pick(v); !slices.Equal(next, Targets{"e"}) {
		t.Fatalf("pick else = %v", next)
	}
	out, err := Format(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "next: c\n") || !strings.Contains(out, "- c\n") {
		t.Fatalf("one step stays a string, several a list:\n%s", out)
	}
	if again, err := Parse(out); err != nil || !slices.Equal(again.Steps[0].Next, Targets{"c", "d"}) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
	var s Step
	if err := json.Unmarshal([]byte(`{"id":"x","next":"y","then":["a","b"]}`), &s); err != nil || !slices.Equal(s.Next, Targets{"y"}) || len(s.Then) != 2 {
		t.Fatalf("json = %+v %v", s, err)
	}
	bad := strings.Replace(src, "id: e, type: end", "id: E-x, type: end", 1)
	bad = strings.Replace(bad, "value: \"{{steps.a.output}}\"", "value: \"\"", 1)
	_, err = Parse(bad)
	for _, want := range []string{"gạch dưới", "thiếu value", `else "e" không có`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q not in %v", want, err)
		}
	}
}

// a coordinate step hands work to its roles: the run it starts has those
// roles (their groups and vote), its instructions, no output required
func TestCoordinateStep(t *testing.T) {
	src := `---
key: dp
name: DP
outputs:
  - { key: ket-luan, required: true }
roles:
  - { key: a, name: A }
  - { key: b, name: B }
  - { key: c, name: C }
  - { key: lead, name: Lead }
parallel: [[a, b, c]]
vote: { roles: [a, b], quorum: 2 }
steps:
  - { id: review, type: coordinate, role: lead, roles: [a, b], prompt: "Giao a, b", next: xong }
  - { id: xong, type: end }
---
`
	d, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c := d.CoordinateDef(d.Steps[0], "Giao a, b review X")
	if len(c.Roles) != 2 || c.Roles[0].Key != "a" || c.Roles[1].Key != "b" || c.StepMode() || c.Body != "Giao a, b review X" {
		t.Fatalf("def = %+v", c)
	}
	if len(c.Parallel) != 1 || len(c.Parallel[0]) != 2 || c.Vote == nil || c.Outputs[0].Required || !d.Outputs[0].Required {
		t.Fatalf("groups %v vote %v outputs %v", c.Parallel, c.Vote, c.Outputs)
	}
	if all := d.CoordinateDef(Step{Role: "a"}, "x"); len(all.Roles) != 3 || all.Vote != nil || slices.ContainsFunc(all.Roles, func(r Role) bool { return r.Key == "a" }) {
		t.Fatalf("every role but its coordinator: %+v", all.Roles)
	}
	bad := strings.Replace(src, `roles: [a, b], prompt: "Giao a, b"`, `roles: [a, lead, x], prompt: ""`, 1)
	_, err = Parse(bad)
	for _, want := range []string{`"lead" không có trong roles hoặc là vai điều phối`, `"x" không có`, "thiếu prompt"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q not in %v", want, err)
		}
	}
}
