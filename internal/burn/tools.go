package burn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Tool is one of the agent's Burn tools, called from the Burn's conversation
// (burn_add, burn_pick, burn_skip, burn_list…), or from a piece's work chat
// (ADR-116) for that piece only: a scan records pieces and ends with
// burn_scan_done (ADR-130), a worker reports burn_done or burn_fail.
func (s *Service) Tool(ctx context.Context, sc actions.Scope, name string, in ToolInput) (string, error) {
	b, err := s.store.Burn().SessionByConversation(ctx, sc.ConversationID)
	if err != nil || sc.ConversationID == "" {
		return "", errors.New("các công cụ burn_* chỉ dùng trong hội thoại Burn")
	}
	if name == "burn_add" && b.ConversationID == sc.ConversationID {
		return s.record(ctx, b, "", in)
	}
	if name == "burn_list" { // its own chat or a piece's (a reviewer's only reads)
		if b.ConversationID != sc.ConversationID {
			if _, err := s.workItem(ctx, b.ID, sc.ConversationID); err != nil {
				return "", errors.New("các công cụ burn_* chỉ dùng trong hội thoại Burn")
			}
		}
		return s.list(ctx, b, in.What)
	}
	it, err := s.store.Burn().Item(ctx, strings.TrimSpace(in.Item))
	if b.ConversationID != sc.ConversationID { // a piece's work chat (a reviewer's only reads)
		own, oerr := s.workItem(ctx, b.ID, sc.ConversationID)
		switch {
		case oerr != nil:
			return "", errors.New("các công cụ burn_* chỉ dùng trong hội thoại Burn")
		case name == "burn_add" && hunting(own):
			return s.record(ctx, b, own.ID, in)
		case name == "burn_add":
			return s.record(ctx, b, "", in)
		case hunting(own) && name != "burn_scan_done":
			return "", fmt.Errorf("lượt quét %s chỉ ghi việc (burn_add) và kết thúc bằng burn_scan_done, không làm việc", own.ID)
		case name != "burn_done" && name != "burn_fail" && name != "burn_scan_done":
			return "", fmt.Errorf("chat của việc %s chỉ dùng burn_done/burn_fail cho chính nó", own.ID)
		case err != nil || it.ID != own.ID:
			return "", fmt.Errorf("chat này chỉ báo cho việc %s", own.ID)
		case name == "burn_scan_done":
			return s.scanDone(ctx, b, own, in)
		}
	}
	if err != nil || it.SessionID != b.ID {
		return "", fmt.Errorf("không có việc %q trong Burn này", in.Item)
	}
	fromStatus := it.Status
	switch name {
	case "burn_pick":
		if it.Status != "found" {
			return "", fmt.Errorf("việc %s đang %s, không chọn lại được", it.ID, it.Status)
		}
		it.Status = "queued"
	case "burn_skip":
		it.Status, it.Summary = "skipped", strings.TrimSpace(in.Reason)
	case "burn_done":
		if strings.TrimSpace(in.Summary) == "" {
			return "", errors.New("hãy ghi tóm tắt đã làm gì")
		}
		it.Status, it.Summary = "done", strings.TrimSpace(in.Summary)
		if s.reviews(ctx, b, "result") { // the reviewer has the last word (ADR-112)
			it.Status = "review"
		}
	case "burn_fail":
		it.Status, it.Summary = "failed", strings.TrimSpace(in.Reason)
	default:
		return "", fmt.Errorf("không có công cụ %s", name)
	}
	// compare-and-swap theo status đã đọc: 2 lệnh đổi trạng thái cùng item
	// chạy gần như đồng thời (vd 2 burn_pick) không được phép cùng thành công
	if err := s.store.Burn().UpdateItemFrom(ctx, it, fromStatus); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return "", fmt.Errorf("việc %s vừa đổi trạng thái ở nơi khác, hãy xem lại danh sách", it.ID)
		}
		return "", err
	}
	if name == "burn_pick" { // a slot free takes it at once (ADR-126: no waiting)
		s.wake(b.ProjectID)
	}
	return fmt.Sprintf("Đã ghi: %s → %s.", it.Title, it.Status), nil
}

// workItem is the piece of session whose work chat conversationID is.
func (s *Service) workItem(ctx context.Context, sessionID, conversationID string) (storage.BurnItem, error) {
	items, err := s.store.Burn().Items(ctx, sessionID)
	if err != nil {
		return storage.BurnItem{}, err
	}
	for _, it := range items {
		if it.WorkConversationID == conversationID {
			return it, nil
		}
	}
	return storage.BurnItem{}, storage.ErrNotFound
}

// ToolInput is what the Burn tools take.
type ToolInput struct {
	Title, Kind, Detail, Item, Summary, Reason string
	What                                       string // burn_list: open | closed | scanned
	Scanned                                    string // burn_scan_done: the areas looked at
	Map                                        string // burn_scan_done: the code map, updated (ADR-130)
	Lessons                                    string // burn_scan_done: the lessons, updated (ADR-131)
	Priority                                   string // burn_add: high | normal | low
}

// list is what burn_list gives (ADR-121): the Burn's data kept out of the
// coordination prompt, read when needed.
func (s *Service) list(ctx context.Context, b storage.BurnSession, what string) (string, error) {
	if what == "scanned" {
		if b.Scanned == "" {
			return "No scan has recorded its areas yet.", nil
		}
		return "Areas earlier scans looked at (oldest first):\n" + b.Scanned, nil
	}
	items, err := s.store.Burn().Items(ctx, b.ID)
	if err != nil {
		return "", err
	}
	closed := map[string]bool{"done": true, "skipped": true, "failed": true}
	var sb strings.Builder
	n := 0
	for _, it := range items {
		if closed[it.Status] != (what == "closed") {
			continue
		}
		n++
		fmt.Fprintf(&sb, "- %s [%s, %s] %s", it.ID, it.Kind, it.Status, it.Title)
		switch {
		case what == "closed" && it.Summary != "":
			fmt.Fprintf(&sb, " — %s", oneLine(it.Summary, 160))
		case what != "closed" && it.Detail != "":
			fmt.Fprintf(&sb, "\n  %s", oneLine(it.Detail, 600))
		}
		sb.WriteString("\n")
	}
	if n == 0 {
		return "None.", nil
	}
	return sb.String(), nil
}
