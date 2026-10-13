package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A JSON line over the scanner's 16MB cap must fail the run instead of
// silently returning an empty "success" (the thread never finishes reading).
func TestCodexFailsOnOversizedLine(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	script := `#!/bin/sh
cat >/dev/null
echo '{"type":"thread.started","thread_id":"s1"}'
python3 -c 'print("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"" + "x"*(17*1024*1024) + "\"}}")'
`
	os.WriteFile(bin, []byte(script), 0o755)
	res, err := codexRunner{}.Run(context.Background(), RunRequest{Bin: bin, WorkDir: dir, Prompt: "hi"}, func(Event) {})
	if err == nil {
		t.Fatalf("expected an error for an oversized line, got res = %+v", res)
	}
	if res.Text != "" {
		t.Fatalf("result must not be read past the oversized line: res = %+v", res)
	}
}

func TestCodexLimitsFromRollout(t *testing.T) {
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "10", "12")
	os.MkdirAll(day, 0o755)
	rollout := `{"type":"session_meta","payload":{"id":"t1"}}
{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":10.0,"window_minutes":300,"resets_at":1791887888},"secondary":{"used_percent":40.0,"window_minutes":10080,"resets_at":1792474688},"rate_limit_reached_type":null}}}
{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":25.0,"window_minutes":300,"resets_at":1791887888},"secondary":{"used_percent":41.0,"window_minutes":10080,"resets_at":1792474688},"rate_limit_reached_type":null}}}
`
	os.WriteFile(filepath.Join(day, "rollout-2026-10-12T22-38-05-t1.jsonl"), []byte(rollout), 0o644)
	l := codexLimits(home, "t1", time.Now())
	if l == nil || l.Status != "allowed" {
		t.Fatalf("limits = %+v", l)
	}
	if w := l.Windows["five_hour"]; w.Utilization != 0.25 || w.ResetsAt.Unix() != 1791887888 {
		t.Fatalf("five_hour = %+v", w)
	}
	if w := l.Windows["seven_day"]; w.Utilization != 0.41 {
		t.Fatalf("seven_day = %+v", w)
	}
	if l := CodexLatestLimits(home); l == nil || l.Windows["five_hour"].Utilization != 0.25 {
		t.Fatalf("latest = %+v", l)
	}
	if codexLimits(home, "missing", time.Now()) != nil {
		t.Fatal("no rollout: want nil")
	}
}
