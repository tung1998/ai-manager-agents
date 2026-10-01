package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// Decider decides a proposal (kind patch | action) in the name of by, the
// person in the chat ("discord:an"); what it returns says how it went.
type Decider interface {
	Decide(ctx context.Context, kind, id string, approve bool, by string) (string, error)
}

// SetDecider lets the people allowed to decide proposals from the chat (ADR-054).
func (m *Manager) SetDecider(d Decider) { m.decider = d }

// proposal is one thing an agent proposed in a chat's conversations, with the
// number the chat knows it by (numbers never shift until all are decided).
type proposal struct {
	N      int    `json:"n"`
	Kind   string `json:"kind"` // patch | action
	ID     string `json:"id"`
	Label  string `json:"label"`
	Action string `json:"action,omitempty"` // the action's kind (git_push, run_command…)
	Target string `json:"target,omitempty"`
}

// approvalMode is how a chat takes proposals: ask (commands) or direct
// (approved in the name of By, who turned it on).
type approvalMode struct {
	Mode string `json:"mode"`
	By   string `json:"by"`
}

func pendingKey(channelID, chatID string) string {
	return "channel_pending/" + channelID + "/" + chatID
}
func modeKey(channelID, chatID string) string { return "channel_approval/" + channelID + "/" + chatID }

func (m *Manager) listing(ctx context.Context, channelID, chatID string) []proposal {
	var list []proposal
	_, _ = m.store.Settings().Get(ctx, pendingKey(channelID, chatID), &list)
	return list
}

func (m *Manager) mode(ctx context.Context, ch storage.Channel, chatID string) approvalMode {
	var am approvalMode
	if ok, _ := m.store.Settings().Get(ctx, modeKey(ch.ID, chatID), &am); ok && am.Mode != "" {
		return am
	}
	if ch.Approval == "direct" || ch.Approval == "admin" {
		return approvalMode{Mode: ch.Approval, By: "bot:" + ch.Name} // the bot was set up that way
	}
	return approvalMode{Mode: "ask"}
}

// modeFor: the chat's mode for a message of userID — administrator only for
// who may approve there; anyone else gets the direct rules.
func (m *Manager) modeFor(ctx context.Context, ch storage.Channel, chatID, userID string) approvalMode {
	am := m.mode(ctx, ch, chatID)
	if am.Mode == "admin" && !mayAdmin(ch, userID) {
		am.Mode = "direct"
	}
	return am
}

// proposals are what waits for a person in a conversation.
func (m *Manager) proposals(ctx context.Context, conversationID string) []proposal {
	var out []proposal
	if ps, err := m.store.Chat().ListPatches(ctx, conversationID); err == nil {
		for _, p := range ps {
			if p.Status == "pending" {
				out = append(out, proposal{Kind: "patch", ID: p.ID, Label: "Sửa " + truncate(strings.Join(p.Files, ", "), 160)})
			}
		}
	}
	if as, err := m.store.Actions().List(ctx, conversationID, "", ""); err == nil {
		for _, a := range as {
			if a.Status == "pending" {
				out = append(out, proposal{Kind: "action", ID: a.ID, Label: firstNonEmpty(actions.Kinds[a.Kind], a.Kind) + ": " + truncate(a.Target, 160), Action: a.Kind, Target: a.Target})
			}
		}
	}
	return out
}

// still reports whether a proposal waits for a decision yet.
func (m *Manager) still(ctx context.Context, p proposal) bool {
	if p.Kind == "patch" {
		x, err := m.store.Chat().GetPatch(ctx, p.ID)
		return err == nil && x.Status == "pending"
	}
	x, err := m.store.Actions().Get(ctx, p.ID)
	return err == nil && x.Status == "pending"
}

var risky = regexp.MustCompile(`(?i)\brm\s|\bgit\s+(push|reset\s+--hard|clean|branch\s+-D)|\bdrop\s|\bdelete\b|\btruncate\b`)

// mustAsk: what is always asked, even when the chat approves directly —
// a push, stopping something, a setting, a command that deletes.
func mustAsk(p proposal) bool {
	switch p.Action {
	case "git_push", "stop_process", "stop_container", "config_change", "update_automation":
		return true
	case "run_command", "run_process":
		return risky.MatchString(p.Target)
	}
	return false
}

// announce tells the chat what its answer left waiting: approved at once in
// direct mode, else numbered for /approve and /reject.
func (m *Manager) announce(ctx context.Context, ch storage.Channel, ad Adapter, chatID, userID, conversationID string) {
	if m.decider == nil || conversationID == "" {
		return
	}
	list := m.listing(ctx, ch.ID, chatID)
	known := map[string]bool{}
	next := 1
	for _, p := range list {
		known[p.ID] = true
		next = max(next, p.N+1)
	}
	am := m.modeFor(ctx, ch, chatID, userID)
	var done []string
	asked := false
	for _, p := range m.proposals(ctx, conversationID) {
		if known[p.ID] {
			continue
		}
		if am.Mode == "admin" || am.Mode == "direct" && !mustAsk(p) { // admin: everything
			detail, err := m.decider.Decide(ctx, p.Kind, p.ID, true, am.By)
			done = append(done, outcome(p, true, detail, err))
			continue
		}
		p.N, next = next, next+1
		list = append(list, p)
		asked = true
	}
	_ = m.store.Settings().Set(ctx, pendingKey(ch.ID, chatID), list)
	if len(done) > 0 {
		how := "chế độ làm thẳng"
		if am.Mode == "admin" {
			how = "chế độ administrator"
		}
		_, _ = ad.Send(ctx, chatID, "Tự duyệt ("+how+"):\n"+strings.Join(done, "\n"))
	}
	if asked {
		m.sendPending(ctx, ch, ad, chatID, list)
	}
}

// sendPending lists what waits, with Approve / Reject buttons where the chat
// has them (and someone may decide there); the commands still work.
func (m *Manager) sendPending(ctx context.Context, ch storage.Channel, ad Adapter, chatID string, list []proposal) {
	text := m.pendingText(ctx, ch, list)
	bs, ok := ad.(ButtonSender)
	if !ok || len(ch.Approvers) == 0 {
		_, _ = ad.Send(ctx, chatID, text)
		return
	}
	var rows [][]Button
	waiting := 0
	for _, p := range list {
		if !m.still(ctx, p) {
			continue
		}
		waiting++
		if len(rows) < 4 {
			n := strconv.Itoa(p.N)
			// by id: a number is given again once all is decided, an old button must not pick the new one
			rows = append(rows, []Button{{Label: "✅ Duyệt " + n, Data: "/approve " + p.ID}, {Label: "❌ Từ chối " + n, Data: "/reject " + p.ID, Danger: true}})
		}
	}
	if waiting == 0 {
		_, _ = ad.Send(ctx, chatID, text)
		return
	}
	if waiting > 1 {
		rows = append(rows, []Button{{Label: "✅ Duyệt tất cả", Data: "/approve all"}})
	}
	if _, err := bs.SendButtons(ctx, chatID, text, rows); err != nil {
		_, _ = ad.Send(ctx, chatID, text) // the commands, then
	}
}

// pendingText lists what still waits, by number.
func (m *Manager) pendingText(ctx context.Context, ch storage.Channel, list []proposal) string {
	var lines []string
	for _, p := range list {
		if m.still(ctx, p) {
			lines = append(lines, fmt.Sprintf("%d. %s", p.N, p.Label))
		}
	}
	if len(lines) == 0 {
		return "Không có gì chờ duyệt."
	}
	how := "Bấm nút bên dưới, hoặc gõ /approve 1 (/approve all) để duyệt, /reject 1 để từ chối."
	if len(ch.Approvers) == 0 {
		how = "Bot này chưa có ai được duyệt qua chat: duyệt trên dashboard (Tổng quan → Cần xử lý)."
	}
	return "Chờ duyệt:\n" + strings.Join(lines, "\n") + "\n" + how
}

func outcome(p proposal, approve bool, detail string, err error) string {
	switch {
	case err != nil:
		return fmt.Sprintf("⚠️ %d. %s: %s", p.N, p.Label, truncate(err.Error(), 300))
	case approve:
		return strings.TrimSpace(fmt.Sprintf("✅ Đã duyệt %s — %s", numbered(p), truncate(detail, 400)))
	}
	return "❌ Đã từ chối " + numbered(p)
}

func numbered(p proposal) string {
	if p.N == 0 {
		return p.Label
	}
	return fmt.Sprintf("%d. %s", p.N, p.Label)
}

// MayDecide: the user is among the bot's approvers ("*" = anyone who may
// message it).
func MayDecide(ch storage.Channel, userID string) bool {
	return slices.Contains(ch.Approvers, "*") || slices.Contains(ch.Approvers, userID)
}

// mayAdmin: administrator mode is for the approvers named one by one —
// "*" (anyone who may message the bot) never gets the machine.
func mayAdmin(ch storage.Channel, userID string) bool {
	return userID != "" && slices.Contains(ch.Approvers, userID)
}

// approvals answers /pending, /approve, /reject and /mode from a chat.
func (m *Manager) approvals(ctx context.Context, ch storage.Channel, in Incoming, cmd, arg, who string) string {
	if m.decider == nil {
		return "Office này chưa bật duyệt qua chat."
	}
	list := m.listing(ctx, ch.ID, in.ChatID)
	if cmd == "pending" {
		return m.pendingText(ctx, ch, list)
	}
	if !MayDecide(ch, in.UserID) {
		return "Bạn không được duyệt qua chat ở bot này (xem ô \"Ai được duyệt\" khi cài bot)."
	}
	by := ch.Kind + ":" + who
	if cmd == "mode" {
		switch CommandName(arg) {
		case "thang", "lam-thang", "direct":
			_ = m.store.Settings().Set(ctx, modeKey(ch.ID, in.ChatID), approvalMode{Mode: "direct", By: by})
			return "Đã chuyển sang làm thẳng: những gì agent đề xuất ở đây được duyệt ngay, đứng tên " + who + ". Push, dừng dịch vụ, đổi cài đặt và lệnh xóa vẫn hỏi. Gõ /mode ask để quay lại."
		case "duyet", "ask":
			_ = m.store.Settings().Set(ctx, modeKey(ch.ID, in.ChatID), approvalMode{Mode: "ask"})
			return "Đã chuyển sang hỏi trước: mỗi đề xuất chờ /approve."
		case "admin", "administrator", "quan-tri":
			if slices.Contains(ch.Approvers, "*") { // anyone would get the machine
				return "Không bật administrator khi ai cũng được duyệt (\"*\"): hãy ghi rõ người được duyệt khi cài bot."
			}
			_ = m.store.Settings().Set(ctx, modeKey(ch.ID, in.ChatID), approvalMode{Mode: "admin", By: by})
			return "Đã chuyển sang administrator: mọi đề xuất ở đây được duyệt ngay (cả push, dừng dịch vụ, đổi cài đặt), đứng tên " + who +
				". Tin của người được duyệt chạy với toàn quyền trên máy cài office (Bash, sửa file ở bất kỳ đâu). Gõ /mode ask để quay lại."
		}
		switch m.mode(ctx, ch, in.ChatID).Mode {
		case "direct":
			return "Đang làm thẳng. Gõ /mode ask để hỏi trước mỗi đề xuất, /mode admin để không hỏi gì."
		case "admin":
			return "Đang administrator: không hỏi duyệt, toàn quyền trên máy. Gõ /mode ask để hỏi trước."
		}
		return "Đang hỏi trước từng đề xuất. Gõ /mode direct để làm thẳng, /mode admin để không hỏi gì."
	}
	approve := cmd == "approve"
	var picked []proposal
	all := arg == "" && len(list) == 1 || strings.EqualFold(strings.TrimSpace(arg), "all") || strings.EqualFold(strings.TrimSpace(arg), "tat-ca")
	stale := 0 // a button of a proposal no longer listed: decided before
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		i := slices.IndexFunc(list, func(p proposal) bool { return err == nil && p.N == n || p.ID == f })
		switch {
		case i >= 0:
			picked = append(picked, list[i])
		case err != nil && strings.Contains(f, "_"):
			stale++
		}
	}
	if all {
		picked = list
	}
	if len(picked) == 0 && stale > 0 {
		return "Đề xuất này đã được quyết trước đó."
	}
	if len(picked) == 0 {
		return "Hãy ghi số đề xuất, ví dụ /approve 1 hoặc /approve all.\n" + m.pendingText(ctx, ch, list)
	}
	var lines []string
	for _, p := range picked {
		if !m.still(ctx, p) {
			if !all {
				lines = append(lines, fmt.Sprintf("%d. %s: đã được quyết trước đó.", p.N, p.Label))
			}
			continue
		}
		detail, err := m.decider.Decide(ctx, p.Kind, p.ID, approve, by)
		lines = append(lines, outcome(p, approve, detail, err))
	}
	// all decided: numbers start again
	if !slices.ContainsFunc(list, func(p proposal) bool { return m.still(ctx, p) }) {
		_ = m.store.Settings().Set(ctx, pendingKey(ch.ID, in.ChatID), []proposal{})
	}
	if len(lines) == 0 {
		return "Không có gì chờ duyệt."
	}
	return strings.Join(lines, "\n")
}

// DirectApprover (for actions.SetAutoApprover): a proposal is approved at
// once, in the name of who turned it on, when either:
//   - a bot's chat is in direct/admin mode (ADR-054), or
//   - the run's own permission is already full access — the agent's own
//     (ADR-074: wherever it runs — chat, bot, automation), or an automation's
//     own override of it.
//
// Either way, what must always be asked stays asked (push, stop, settings,
// delete…), and so does creating/running an automation — an unattended run
// never grants itself more automations.
func (m *Manager) DirectApprover(ctx context.Context, a storage.Action) (string, bool) {
	if a.JobID == "" {
		return "", false
	}
	job, err := m.store.Jobs().Get(ctx, a.JobID)
	if err != nil {
		return "", false
	}
	// ADR-074 (security fix): the job already carries whether this run has
	// full (administrator) access and who enabled it, computed once when it
	// started (internal/trigger.Runner or internal/chat.Engine) — never
	// re-derived here from the agent or the automation. But a proposal can
	// stay pending a while (a job queued behind others, a human-in-the-loop
	// action waiting on an approval), so who enabled it is re-checked against
	// admin status right now, at decision time — an admin stripped of the
	// role after the job started must not have it auto-approve on their behalf.
	if job.FullAccess && job.FullAccessBy != "" && m.isAdminEmail(ctx, job.FullAccessBy) &&
		a.Kind != "create_automation" && a.Kind != "run_automation" && !mustAsk(proposal{Action: a.Kind, Target: a.Target}) {
		return job.FullAccessBy, true
	}
	if !trigger.IsChannel(job.Trigger) {
		return "", false
	}
	var p trigger.ChannelPayload
	if json.Unmarshal([]byte(job.Payload), &p) != nil || p.ChannelID == "" {
		return "", false
	}
	ch, err := m.store.Channels().Get(ctx, p.ChannelID)
	if err != nil {
		return "", false
	}
	am := m.modeFor(ctx, ch, p.ChatID, p.UserID)
	switch {
	case am.By == "":
	case am.Mode == "admin": // administrator: nothing asked
		return am.By, true
	case am.Mode == "direct" && a.Kind != "create_automation" && a.Kind != "run_automation" && !mustAsk(proposal{Action: a.Kind, Target: a.Target}):
		return am.By, true
	}
	return "", false
}

// isAdminEmail: that email is still an admin of the office (ADR-074: a
// FullAccess/override an admin turned on only holds while they still are
// one). Mirrors trigger.Runner.isAdminEmail (unexported there, so copied
// here rather than shared).
func (m *Manager) isAdminEmail(ctx context.Context, email string) bool {
	if email == "" {
		return false
	}
	u, err := m.store.Users().GetByEmail(ctx, email)
	return err == nil && u.Role == storage.RoleAdmin && !u.Disabled
}

// FullAccessFor: a message of userID in chatID runs with the machine (Bash,
// any file): the chat is in administrator mode and the user may approve there.
func (m *Manager) FullAccessFor(ctx context.Context, ch storage.Channel, chatID, userID string) bool {
	return m.mode(ctx, ch, chatID).Mode == "admin" && mayAdmin(ch, userID)
}
