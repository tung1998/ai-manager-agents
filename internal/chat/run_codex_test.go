package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
