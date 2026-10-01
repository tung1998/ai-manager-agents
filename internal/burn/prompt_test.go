package burn

import (
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func TestIdleBacksOff(t *testing.T) {
	s := &Service{idle: 5 * time.Minute}
	for n, want := range map[int]time.Duration{1: 5 * time.Minute, 2: 10 * time.Minute, 3: 20 * time.Minute, 4: 40 * time.Minute, 5: time.Hour, 9: time.Hour} {
		if got := s.idleAfter(n); got != want {
			t.Errorf("idleAfter(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestPlanPromptDigsDeeper(t *testing.T) {
	b := storage.BurnSession{MaxSubagents: 2}
	p := planPrompt(b, nil, 0)
	for _, want := range []string{"subagent", "vùng đã xem"} {
		if !strings.Contains(p, want) {
			t.Errorf("first scan prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "liên tiếp") {
		t.Error("first scan should not mention empty scans")
	}
	if p2 := planPrompt(b, nil, 2); !strings.Contains(p2, "2 lần quét liên tiếp") {
		t.Errorf("repeat scan should say how many came back empty:\n%s", p2)
	}
}
