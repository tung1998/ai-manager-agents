package channels

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/storage"
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
	if ch.Approval == "direct" {
		return approvalMode{Mode: "direct", By: "bot:" + ch.Name} // the bot was set up that way
	}
	return approvalMode{Mode: "ask"}
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
func (m *Manager) announce(ctx context.Context, ch storage.Channel, ad Adapter, chatID, conversationID string) {
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
	am := m.mode(ctx, ch, chatID)
	var done []string
	asked := false
	for _, p := range m.proposals(ctx, conversationID) {
		if known[p.ID] {
			continue
		}
		if am.Mode == "direct" && !mustAsk(p) {
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
		_, _ = ad.Send(ctx, chatID, "Tự duyệt (chế độ làm thẳng):\n"+strings.Join(done, "\n"))
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
			rows = append(rows, []Button{{Label: "✅ Duyệt " + n, Data: "/approve " + n}, {Label: "❌ Từ chối " + n, Data: "/reject " + n, Danger: true}})
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
		}
		if m.mode(ctx, ch, in.ChatID).Mode == "direct" {
			return "Đang làm thẳng. Gõ /mode ask để hỏi trước mỗi đề xuất."
		}
		return "Đang hỏi trước từng đề xuất. Gõ /mode direct để làm thẳng."
	}
	approve := cmd == "approve"
	var picked []proposal
	all := arg == "" && len(list) == 1 || strings.EqualFold(strings.TrimSpace(arg), "all") || strings.EqualFold(strings.TrimSpace(arg), "tat-ca")
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		for _, p := range list {
			if p.N == n {
				picked = append(picked, p)
			}
		}
	}
	if all {
		picked = list
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
