package api_test

import (
	"strings"
	"testing"
)

// putPolicy must refuse a custom pack command that covers a risky command
// (delete, push, run-another-command), the same class AllowAlways blocks.
func TestPutPolicyRejectsRiskyCommands(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)

	for _, pattern := range []string{"git push *", "rm *", "sudo *"} {
		policy := map[string]any{"packs": []map[string]any{{"label": "Custom", "commands": []string{pattern}}}}
		resp, b := do(t, admin, "PUT", e.srv.URL+"/api/projects/"+pid+"/policy", policy, nil)
		if resp.StatusCode != 400 {
			t.Fatalf("pattern %q must be rejected: %d %v", pattern, resp.StatusCode, b)
		}
		if msg, _ := b["error"].(string); !strings.Contains(msg, pattern) {
			t.Fatalf("error must name the rejected pattern %q: %v", pattern, b)
		}
	}

	// a non-risky pattern still saves fine
	policy := map[string]any{"packs": []map[string]any{{"label": "Custom", "commands": []string{"go test *"}}}}
	resp, b := do(t, admin, "PUT", e.srv.URL+"/api/projects/"+pid+"/policy", policy, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("a safe pattern must save: %d %v", resp.StatusCode, b)
	}
}
