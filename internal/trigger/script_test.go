package trigger_test

import (
	"context"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

func TestRunScriptOutputAndExit(t *testing.T) {
	out, code, timedOut, err := trigger.RunScript(context.Background(), t.TempDir(),
		storage.AutomationScript{Lang: "bash", Body: "read x; echo \"got $x $OFFICE_PAYLOAD\"; echo oops >&2; exit 3"},
		[]string{"OFFICE_PAYLOAD=pl"}, "in\n")
	if err != nil || code != 3 || timedOut || !strings.Contains(out, "got in pl") || !strings.Contains(out, "oops") {
		t.Fatalf("out=%q code=%d timeout=%v err=%v", out, code, timedOut, err)
	}
}

func TestSignals(t *testing.T) {
	got := trigger.Signals("a\n@@agent: lỗi DB tăng\nb\n  @@agent:  xem lại cron \n")
	if len(got) != 2 || got[0] != "lỗi DB tăng" || got[1] != "xem lại cron" {
		t.Fatalf("signals = %q", got)
	}
	if _, _, _, err := trigger.RunScript(context.Background(), t.TempDir(), storage.AutomationScript{Lang: "ruby", Body: "x"}, nil, ""); err == nil {
		t.Fatal("unknown language ran")
	}
}
