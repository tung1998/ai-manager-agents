package chat

import "testing"

// A delegator always hears back from a hand-off, even when the message
// already had its replies; other turns stay held to the cap.
func TestReportBackNotHeldToAnswers(t *testing.T) {
	if _, over := overAnswers("auto:Lead", maxAnswers, false); !over {
		t.Fatal("an unattended message went past maxAnswers")
	}
	if _, over := overAnswers("auto:Lead", maxAnswers, true); over {
		t.Fatal("the report of a done hand-off was dropped")
	}
	if _, over := overAnswers("human:a@b.c", maxAnswers, false); over {
		t.Fatal("a person's message got the unattended cap")
	}
}
