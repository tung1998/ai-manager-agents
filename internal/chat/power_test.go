package chat

import (
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// The office assistant's own agent is read only; its rights are said once,
// by its mode: never "read only" beside "administrator" (ADR-059).
func TestAssistantPowerSaidOnce(t *testing.T) {
	project := storage.Repo{Name: "Office", Path: "/tmp/office"}
	agent := storage.Agent{Name: "Trợ lý office", Permissions: storage.Permissions{Level: perm.Read, ReadOnly: true}}
	acc := perm.Access{Level: perm.Read}
	pl := place{dir: "/tmp/office"}
	for power, want := range map[string]string{"admin": "administrator", "manage": "manage the office", "answer": "answer only"} {
		got := systemPrompt(project, agent, true, keywords(), acc, pl, Language{}, power)
		if !strings.Contains(got, "Your permission this turn: "+want) {
			t.Fatalf("%s: no permission line: %s", power, got)
		}
		if strings.Contains(got, "You only analyse and answer") || strings.Contains(got, perm.Label(perm.Read)) {
			t.Fatalf("%s: still says read only: %s", power, got)
		}
	}
	// any other agent: its level, as before
	if got := systemPrompt(project, agent, true, keywords(), acc, pl, Language{}, ""); !strings.Contains(got, "Your permission this turn: "+perm.Label(perm.Read)) ||
		!strings.Contains(got, "You only analyse and answer") {
		t.Fatalf("an agent: %s", got)
	}
}

func TestMachineAccess(t *testing.T) {
	if !machineAccess(storage.ProviderClaudeCLI) || machineAccess(storage.ProviderCodexCLI) || machineAccess(storage.ProviderOpenAI) {
		t.Fatal("only Claude Code, Gemini and Antigravity give the machine")
	}
}
