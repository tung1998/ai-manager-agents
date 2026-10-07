package burn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// Review stages (ADR-112), in the order a piece meets them: the problem
// (is it real, worth doing), the plan (do it, and how), the result (keep it).
// Which are reviewed, and by whom, is the Burn's review profile (ADR-113);
// none: a Burn runs as before.
var ReviewStages = []string{"issue", "plan", "result"}

// reviewer is who reviews stage under the Burn's profile, as it is now (it
// may be edited while the Burn runs); ok is false when stage is not reviewed.
func (s *Service) reviewer(ctx context.Context, b storage.BurnSession, stage string) (storage.BurnReviewStage, bool) {
	if b.ReviewProfileID == "" {
		return storage.BurnReviewStage{}, false
	}
	p, err := s.store.Burn().ReviewProfile(ctx, b.ReviewProfileID)
	if err != nil {
		return storage.BurnReviewStage{}, false
	}
	r, ok := p.Stages[stage]
	if r.AgentID == "" {
		r.AgentID = b.AgentID
	}
	return r, ok
}

// CheckReviewers refuses a profile whose reviewer is paused: its review
// would only get the "agent is off" notice, read as a no.
func (s *Service) CheckReviewers(ctx context.Context, profileID string) error {
	if profileID == "" {
		return nil
	}
	p, err := s.store.Burn().ReviewProfile(ctx, profileID)
	if err != nil {
		return errors.New("không tìm thấy hồ sơ review của Burn: hãy chọn hồ sơ khác")
	}
	for _, stage := range ReviewStages {
		if r, ok := p.Stages[stage]; ok {
			if err := s.CheckAgent(ctx, r.AgentID); err != nil {
				return fmt.Errorf("%s (hồ sơ %s): %w", reviewLabel[stage], p.Name, err)
			}
		}
	}
	return nil
}

func (s *Service) reviews(ctx context.Context, b storage.BurnSession, stage string) bool {
	_, ok := s.reviewer(ctx, b, stage)
	return ok
}

// gate runs the reviews a chosen piece meets before it is done (problem,
// plan): go is false when it was turned down (skipped) or the review could
// not run (failed: the loop waits, then asks again).
func (s *Service) gate(ctx context.Context, b storage.BurnSession, it storage.BurnItem) (proceed, failed bool) {
	if it.Status != "queued" { // started before: it goes on
		return true, false
	}
	for _, stage := range []string{"issue", "plan"} {
		if !s.reviews(ctx, b, stage) || slices.Contains(it.Reviewed, stage) {
			continue
		}
		ok, note, err := s.review(ctx, b, it, stage)
		if err != nil {
			return false, ctx.Err() == nil
		}
		from := it.Status
		it.ReviewNote = note
		if ok {
			it.Reviewed = append(it.Reviewed, stage)
		} else {
			it.Status, it.Summary = "skipped", reviewLabel[stage]+": không làm. "+oneLine(note, 400)
		}
		// the person may have moved it meanwhile: theirs wins
		if err := s.store.Burn().UpdateItemFrom(context.WithoutCancel(ctx), it, from); err != nil || !ok {
			return false, false
		}
	}
	return true, false
}

// finish reviews a piece reported done (status "review"): agreed, it is done
// (committed to its branch, or its diff put up); not, it goes again with what
// the reviewer said, then fails. failed: the review could not run.
func (s *Service) finish(ctx context.Context, b storage.BurnSession, it storage.BurnItem) (failed bool) {
	ok, note, err := s.review(ctx, b, it, "result")
	if err != nil {
		return ctx.Err() == nil // stays "review": asked again
	}
	ctx = context.WithoutCancel(ctx)
	it.ReviewNote = note
	if ok {
		it.Status, it.Reviewed = "done", append(it.Reviewed, "result")
		s.deliver(ctx, b, &it)
	} else {
		it.Status, it.Summary = s.failedOrAgain(it, reviewLabel["result"]+": chưa đạt. "+oneLine(note, 400))
	}
	_ = s.store.Burn().UpdateItem(ctx, it)
	return false
}

// deliver hands a finished piece over: committed to its branch, or (patch
// mode, held back for its review) its diff put up to approve.
func (s *Service) deliver(ctx context.Context, b storage.BurnSession, it *storage.BurnItem) {
	var err error
	switch {
	case b.ResultMode != "patch":
		err = commit(ctx, it.Worktree, it.Branch, it.Title, it.Summary)
	case s.reviews(ctx, b, "result"):
		err = s.chat.ProposeTree(ctx, b.ConversationID, it.Worktree, "burn-"+it.ID)
	}
	if err != nil {
		it.Summary = strings.TrimSpace(it.Summary + "\n(không giao được kết quả: " + err.Error() + ")")
	}
}

var reviewLabel = map[string]string{"issue": "Review vấn đề", "plan": "Review cách làm", "result": "Review kết quả"}

// review asks the stage's reviewer (an agent, or the workflow it runs) about
// a piece, in the stage's review chat, read-only: agreed or not, and why.
// Not reviewed (the profile changed meanwhile): agreed.
func (s *Service) review(ctx context.Context, b storage.BurnSession, it storage.BurnItem, stage string) (bool, string, error) {
	r, ok := s.reviewer(ctx, b, stage)
	if !ok {
		return true, "", nil
	}
	conv, err := s.ensureReviewConversation(ctx, b, stage, r.AgentID)
	if err != nil {
		return false, "", err
	}
	text := reviewPrompt(b, it, stage)
	if r.Workflow != "" {
		text = workflow.Call(r.Workflow, text)
	}
	rctx := chat.WithCeiling(chat.WithTurnTimeout(actor.With(ctx, "burn:"+b.StartedBy), 0), perm.Read)
	if stage == "result" { // it reads the piece's own worktree
		rctx = chat.WithTree(rctx, "burn-"+it.ID, true)
	}
	res, err := s.run(rctx, conv, text)
	if err == nil && res.failed != "" {
		err = errors.New(res.failed)
	}
	if err != nil {
		return false, "", err
	}
	return verdict(res.text) == "yes", strings.TrimSpace(res.text), nil
}

// ensureReviewConversation gives a stage's reviews a chat of their own with
// its reviewer, a new one once the reviewer was changed.
func (s *Service) ensureReviewConversation(ctx context.Context, b storage.BurnSession, stage, agentID string) (string, error) {
	if id := b.ReviewConversations[stage]; id != "" {
		c, err := s.store.Chat().GetConversation(ctx, id)
		if err == nil && (agentID == "" || c.AgentID == agentID) && c.Cleaned == "" {
			return id, nil
		}
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return "", err
		}
	}
	conv, err := s.chat.StartConversationPurpose(ctx, b.ProjectID, agentID, "burn")
	if err != nil {
		return "", err
	}
	conv.Title = "Burn · " + reviewLabel[stage]
	_ = s.store.Chat().UpdateConversation(ctx, conv)
	// saved on the latest session: the loop's copy may be older than the settings
	cur, err := s.store.Burn().SessionByID(ctx, b.ID)
	if err != nil {
		return "", err
	}
	if cur.ReviewConversations == nil {
		cur.ReviewConversations = map[string]string{}
	}
	cur.ReviewConversations[stage] = conv.ID
	if _, err := s.store.Burn().SaveSession(ctx, cur); err != nil {
		return "", err
	}
	return conv.ID, nil
}

func reviewPrompt(b storage.BurnSession, it storage.BurnItem, stage string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Burn · %s] Việc %s (%s): %s\n", reviewLabel[stage], it.ID, it.Kind, it.Title)
	if it.Detail != "" {
		fmt.Fprintf(&sb, "Chi tiết agent ghi: %s\n", it.Detail)
	}
	if b.Focus != "" {
		fmt.Fprintf(&sb, "Trọng tâm người dùng dặn: %s\n", b.Focus)
	}
	if it.ReviewNote != "" && stage == "result" {
		fmt.Fprintf(&sb, "Ý kiến review trước đó: %s\n", oneLine(it.ReviewNote, 600))
	}
	sb.WriteString("\nBurn là agent tự tìm và làm việc trong project này. Bạn là người review độc lập, CHỈ ĐỌC (không sửa file, không đề xuất thao tác). Đọc code thật trước khi kết luận.\n")
	switch stage {
	case "issue":
		sb.WriteString("Hãy phân tích VẤN ĐỀ: có thật không (đối chiếu code, dẫn file:dòng), có đáng làm không (lợi ích so với rủi ro), có trùng việc đã làm hay đã có quyết định khác không.\n")
	case "plan":
		sb.WriteString("Hãy phân tích CÁCH LÀM: có nên làm ngay không, phạm vi hợp lý chưa, nên sửa ở đâu và cần kiểm chứng gì. Nếu đồng ý, ghi hướng dẫn ngắn cho agent làm việc (sẽ được chuyển cho nó).\n")
	case "result":
		fmt.Fprintf(&sb, "Agent báo đã xong: %s\n", oneLine(it.Summary, 800))
		sb.WriteString("Hãy review KẾT QUẢ trong worktree này (git status, git diff, git log so với nhánh chính): đúng vấn đề chưa, có lỗi, thiếu test hay sửa ngoài phạm vi không. Chạy build/test để kiểm chứng nếu quyền cho phép. Không đồng ý thì ghi rõ cần sửa gì (sẽ được chuyển cho agent làm lại).\n")
	}
	sb.WriteString("\nDòng ĐẦU TIÊN của câu trả lời phải đúng một trong hai: `KẾT LUẬN: ĐỒNG Ý` hoặc `KẾT LUẬN: KHÔNG ĐỒNG Ý`; sau đó là lý do ngắn, dựa trên bằng chứng.")
	return sb.String()
}

// verdict reads the reviewer's conclusion from the first lines of its answer
// (unclear counts as no, as a workflow's vote).
func verdict(text string) string {
	for _, line := range strings.SplitN(strings.TrimSpace(text), "\n", 4) {
		l := strings.ToUpper(strings.Trim(strings.TrimSpace(line), "*`_# "))
		switch {
		case strings.Contains(l, "KHÔNG ĐỒNG Ý"), strings.Contains(l, "KHONG DONG Y"), strings.Contains(l, "PHẢN ĐỐI"):
			return "no"
		case strings.Contains(l, "ĐỒNG Ý"), strings.Contains(l, "DONG Y"), strings.Contains(l, "TÁN THÀNH"):
			return "yes"
		}
	}
	return "unclear"
}
