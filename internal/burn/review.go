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
	"bitbucket.org/senprints/agent-office/internal/prompts"
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
		ok, note, err := s.review(ctx, b, &it, stage)
		if err != nil {
			r, _ := s.reviewer(ctx, b, stage)
			return false, s.reviewErrAttempt(ctx, it, stage, r.AgentID, err)
		}
		it.ReviewErrAttempts = 0
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
	ok, note, err := s.review(ctx, b, &it, "result")
	if err != nil {
		r, _ := s.reviewer(ctx, b, "result")
		return s.reviewErrAttempt(ctx, it, "result", r.AgentID, err) // stays "review": asked again, unless past the limit
	}
	it.ReviewErrAttempts = 0
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

// maxReviewErrAttempts: how many times in a row review() may fail with a
// system error (an agent gone, a bad review profile, the network) before the
// piece is failed instead of asked again forever (ADR-120). The AI
// connection's limit is not a system error and is not counted here: it is
// checked separately (limitHit), and keeps the piece retrying as before.
const maxReviewErrAttempts = 3

// reviewErrAttempt counts a system error from review(): past the threshold,
// the piece is marked failed instead of looping at this stage forever.
// agentID is the stage's reviewer (not necessarily the Burn's own agent):
// its connection hitting its limit is not counted, and is not failed, since
// the loop already waits a minute and asks again (step, waitLimit). retry
// reports whether it is still worth asking again (false once failed, or
// once the context was cancelled: no point saving then). it is saved only
// if its status is still what this call started from, so a change made
// elsewhere while review() ran (paused, skipped, a person acting on it) is
// not overwritten.
func (s *Service) reviewErrAttempt(ctx context.Context, it storage.BurnItem, stage, agentID string, err error) (retry bool) {
	if ctx.Err() != nil {
		return false
	}
	if _, hit := s.limitHit(ctx, agentID); hit {
		return true
	}
	ctx = context.WithoutCancel(ctx)
	from := it.Status
	it.ReviewErrAttempts++
	if it.ReviewErrAttempts < maxReviewErrAttempts {
		_ = s.store.Burn().UpdateItemFrom(ctx, it, from)
		return true
	}
	it.Status = "failed"
	it.Summary = fmt.Sprintf("%s: lỗi hệ thống liên tục (%d lần liền): %s", reviewLabel[stage], it.ReviewErrAttempts, err.Error())
	_ = s.store.Burn().UpdateItemFrom(ctx, it, from)
	return false
}

// review asks the stage's reviewer (an agent, or the workflow it runs) about
// a piece, in the stage's review chat, read-only: agreed or not, and why.
// Not reviewed (the profile changed meanwhile): agreed.
func (s *Service) review(ctx context.Context, b storage.BurnSession, it *storage.BurnItem, stage string) (bool, string, error) {
	r, ok := s.reviewer(ctx, b, stage)
	if !ok {
		return true, "", nil
	}
	conv, err := s.ensureReviewConversation(ctx, b, it, stage, r.AgentID)
	if err != nil {
		return false, "", err
	}
	text := reviewPrompt(b, *it, stage)
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

// ensureReviewConversation gives a piece's review at stage a hidden chat of
// its own with the reviewer (ADR-114), a new one once the reviewer was
// changed; it is kept on the piece, to read from the Burn.
func (s *Service) ensureReviewConversation(ctx context.Context, b storage.BurnSession, it *storage.BurnItem, stage, agentID string) (string, error) {
	if id := it.ReviewConversations[stage]; id != "" {
		c, err := s.store.Chat().GetConversation(ctx, id)
		if err == nil && (agentID == "" || c.AgentID == agentID) && c.Cleaned == "" {
			return id, nil
		}
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return "", err
		}
	}
	conv, err := s.chat.StartConversationPurpose(ctx, b.ProjectID, agentID, chat.BurnReviewPurpose)
	if err != nil {
		return "", err
	}
	conv.Title = oneLine(reviewLabel[stage]+": "+it.Title, 80)
	_ = s.store.Chat().UpdateConversation(ctx, conv)
	if it.ReviewConversations == nil {
		it.ReviewConversations = map[string]string{}
	}
	it.ReviewConversations[stage] = conv.ID
	// saved on the latest piece: the person may have moved it meanwhile
	cur, err := s.store.Burn().Item(ctx, it.ID)
	if err != nil {
		return "", err
	}
	cur.ReviewConversations = it.ReviewConversations
	if err := s.store.Burn().UpdateItem(ctx, cur); err != nil {
		return "", err
	}
	return conv.ID, nil
}

// reviewPrompt asks a stage's reviewer about a piece (burn/review.md).
func reviewPrompt(b storage.BurnSession, it storage.BurnItem, stage string) string {
	d := struct {
		Stage, ID, Kind, Title, Detail, Focus, ReviewNote string
		Summary, Worktree, Branch, Agree, Disagree        string
		FocusChecks                                       []string
	}{Stage: stage, ID: it.ID, Kind: it.Kind, Title: it.Title, Detail: it.Detail, Focus: b.Focus,
		// a workflow runs in chats of its own, not in the piece's worktree: where it is
		Worktree: it.Worktree, Branch: it.Branch, Agree: verdictAgree, Disagree: verdictDisagree}
	if stage == "result" {
		d.FocusChecks = focusChecks(b.Focus)
		if it.ReviewNote != "" {
			d.ReviewNote = oneLine(it.ReviewNote, 600)
		}
		d.Summary = oneLine(it.Summary, 800)
	}
	return prompts.Render("burn/review", d)
}

// The first line of a reviewer's answer (verdict reads it).
const (
	verdictAgree    = "VERDICT: AGREE"
	verdictDisagree = "VERDICT: DISAGREE"
)

// verdict reads the reviewer's conclusion from the first lines of its answer
// (unclear counts as no, as a workflow's vote).
func verdict(text string) string {
	for _, line := range strings.SplitN(strings.TrimSpace(text), "\n", 4) {
		l := strings.ToUpper(strings.Trim(strings.TrimSpace(line), "*`_# "))
		switch {
		case strings.Contains(l, "DISAGREE"), strings.Contains(l, "KHÔNG ĐỒNG Ý"), strings.Contains(l, "KHONG DONG Y"), strings.Contains(l, "PHẢN ĐỐI"):
			return "no"
		case strings.Contains(l, "AGREE"), strings.Contains(l, "ĐỒNG Ý"), strings.Contains(l, "DONG Y"), strings.Contains(l, "TÁN THÀNH"):
			return "yes"
		}
	}
	return "unclear"
}
