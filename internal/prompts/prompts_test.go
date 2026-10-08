package prompts

import (
	"strings"
	"testing"
)

// Every prompt file parses as a template (Render's panics are found here).
func TestEveryPromptParses(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("no prompt embedded")
	}
	for _, n := range names {
		if Text(n) == "" {
			t.Errorf("%s is empty", n)
		}
		parsed(n)
	}
}

func TestRender(t *testing.T) {
	got := Render("handoff/handed", map[string]string{"Agent": "Ann"})
	if !strings.HasPrefix(got, "Handed to Ann; Ann starts") || strings.Contains(got, "[[") {
		t.Errorf("Render = %q", got)
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "no misc/none.md") {
			t.Errorf("a missing prompt: %v", r)
		}
	}()
	Text("misc/none")
}

func TestHandoff(t *testing.T) {
	for _, c := range []struct {
		h    Handoff
		want string
	}{
		{Handoff{From: "Ann", Task: "fix it"}, "Ann handed you a task:\nfix it\n\nDo this part, then report the result briefly."},
		{Handoff{Kind: "tagged", From: "The person"}, "The person tagged you in the chat. Answer the part meant for you in the latest message."},
		{Handoff{Kind: "report", From: "Bob"}, "Bob finished the task you handed over (see the latest message). Report the result to the person briefly and carry on if needed."},
		{Handoff{From: "Ann (coordinator of the workflow W)", Role: "dev", Task: "q", ReportBack: "Answer briefly."}, "Ann (coordinator of the workflow W) asks you, role dev:\nq\n\nAnswer briefly."},
	} {
		if got := c.h.String(); got != c.want {
			t.Errorf("%+v:\n%q\nwant\n%q", c.h, got, c.want)
		}
	}
}
