package chat

import (
	"strings"
	"testing"
)

// While the task runs, the lead relays guidance to the team instead of editing.
func TestTaskIntroWhileRunning(t *testing.T) {
	run := taskIntro("running")
	if !strings.Contains(run, "đang chạy") || !strings.Contains(run, "KHÔNG sửa code") {
		t.Fatalf("running intro = %q", run)
	}
	if done := taskIntro("done"); strings.Contains(done, "đang chạy") || !strings.Contains(done, "commit") {
		t.Fatalf("done intro = %q", done)
	}
}
