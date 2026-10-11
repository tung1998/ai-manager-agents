package chat

import "testing"

// DeleteConversation/CleanConversation must refuse to drop a chat's worktree
// while a turn or a member hand-off is still writing to it.
func TestBusyBlocksActiveTurn(t *testing.T) {
	e := NewEngine(nil, nil, nil)
	e.active["c1"] = &Turn{}
	if err := e.busy("c1"); err != ErrBusy {
		t.Fatalf("busy = %v, want ErrBusy", err)
	}
}

func TestBusyBlocksBackgroundHandoff(t *testing.T) {
	e := NewEngine(nil, nil, nil)
	e.bg["c1/agent-x"] = &Turn{}
	if err := e.busy("c1"); err != ErrAgentBusy {
		t.Fatalf("busy = %v, want ErrAgentBusy", err)
	}
}

func TestBusyIdleIsNil(t *testing.T) {
	e := NewEngine(nil, nil, nil)
	e.active["other"] = &Turn{}
	e.bg["other/agent-x"] = &Turn{}
	if err := e.busy("c1"); err != nil {
		t.Fatalf("busy = %v, want nil", err)
	}
}
