package usage

import (
	"math"
	"testing"
)

// Haiku 5.5 is priced by prompt length: up to 100K tokens the base price,
// over it five times that.
func TestEstimateLongPrompt(t *testing.T) {
	short, ok := Estimate("claude-haiku-5-5", 100_000, 1_000, nil)
	if !ok || math.Abs(short-(0.01+0.0005)) > 1e-9 {
		t.Fatalf("100K prompt: %v %v", short, ok)
	}
	long, _ := Estimate("claude-haiku-5-5", 200_000, 1_000, nil)
	if math.Abs(long-(0.10+0.0025)) > 1e-9 {
		t.Fatalf("200K prompt: %v", long)
	}
	if c, _ := Estimate("claude-opus-5-5", 200_000, 0, nil); math.Abs(c-0.8) > 1e-9 {
		t.Fatalf("one price for any length: %v", c)
	}
}
