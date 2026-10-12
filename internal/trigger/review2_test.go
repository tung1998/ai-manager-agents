package trigger_test

import (
	"context"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

func TestScriptDoesNotSeeAIKeys(t *testing.T) { // M3
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-secret")
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	t.Setenv("KEEP_ME", "ok")
	out, _, _, err := trigger.RunScript(context.Background(), t.TempDir(),
		storage.AutomationScript{Lang: "bash", Body: `echo "[$ANTHROPIC_API_KEY][$OPENAI_API_KEY][$KEEP_ME]"`}, nil, "")
	if err != nil || out != "[][][ok]\n" {
		t.Fatalf("out = %q %v", out, err)
	}
}
