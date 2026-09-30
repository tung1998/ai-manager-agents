package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Saves check what they were edited from (ADR-072): an edit page reads a
// version with the data and sends it back when it saves; someone else's
// change in between answers 409 and nothing is overwritten. The version is a
// hash of what a person edits (a run's last-run time or failures are not an
// edit). A save without a version (a tool, an agent) is taken as before.

func versionOf(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// conflicted answers 409 when want is given and is not now.
func conflicted(w http.ResponseWriter, want, now string) bool {
	if want == "" || want == now {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]any{"code": "conflict",
		"error": "Đã có thay đổi mới ở nơi khác kể từ lúc bạn mở (người khác hoặc agent). Tải lại để xem bản mới rồi sửa lại."})
	return true
}

func automationVersion(a storage.Automation) string {
	c := a.Config
	c.ConversationID, c.SecretHash = "", "" // kept by office, not edited
	return versionOf(struct {
		Name, Source, Action, AgentID, Prompt, EditMode, ModelTier string
		Enabled, KeepContext                                       bool
		Config                                                     storage.AutomationConfig
		Limits                                                     storage.AutomationLimits
		Script                                                     storage.AutomationScript
	}{a.Name, a.Source, a.Action, a.AgentID, a.Prompt, a.EditMode, a.ModelTier, a.Enabled, a.KeepContext, c, a.Limits, a.Script})
}

func agentVersion(d agentDTO) string {
	d.Version, d.Sort = "", 0
	return versionOf(d)
}

func policyVersion(p perm.Policy) string {
	return versionOf(struct {
		Packs         []perm.Pack
		DenyPaths     []string
		WorktreeLinks []string
	}{p.Packs, p.DenyPaths, p.WorktreeLinks})
}
