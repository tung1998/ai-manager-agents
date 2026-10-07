package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/attach"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// SetOnRunWait is told, when a turn of a workflow's own chat ends, the chat
// that called it: a bot's chat lists what the run waits on a person for.
func (e *Engine) SetOnRunWait(fn func(ctx context.Context, callerID string)) { e.onRunWait = fn }

// callWorkflow runs a workflow called from a chat as a function call: the
// chat keeps the person's message and answers, when the run ends, with its
// output; everything in between happens in the run's own chat, where no
// person writes. The chat's answer lasts as long as the run (a bot or an
// automation waits for it as for any answer; Dừng there stops the run).
func (e *Engine) callWorkflow(ctx context.Context, conv storage.Conversation, agent storage.Agent, run *wfRun, text, prompt, pageContext string, attachmentIDs []string, files []attach.File) (*Turn, storage.Message, error) {
	e.mu.Lock()
	if _, busy := e.active[conv.ID]; busy {
		e.mu.Unlock()
		return nil, storage.Message{}, ErrBusy
	}
	wctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	turn := &Turn{ID: fmt.Sprintf("%s-%d", conv.ID, time.Now().UnixNano()), ConversationID: conv.ID, wake: make(chan struct{}), cancel: cancel,
		actor: actor.From(ctx), agentID: agent.ID, agentName: agent.Name, total: new(atomic.Int32), tier: ModelTierFrom(ctx), ceiling: ceilingOf(ctx)}
	turn.total.Store(1)
	e.active[conv.ID], e.turns[turn.ID] = turn, turn
	e.mu.Unlock()
	e.running(conv.ID)
	fail := func(err error) (*Turn, storage.Message, error) {
		cancel()
		e.finish(turn)
		return nil, storage.Message{}, err
	}
	msg, err := e.store.Chat().AddMessage(ctx, storage.Message{ConversationID: conv.ID, Role: "user", Content: text, Attachments: attach.Refs(files), Author: actor.From(ctx), Context: pageContext})
	if err != nil {
		return fail(err)
	}
	e.titleFrom(ctx, &conv, text, files)
	job, err := e.beginJob(ctx, conv, agent.ID, truncate("⚙️ "+run.def.Name+": "+oneLine(firstNonEmpty(run.rec.Input, run.def.Name)), 80))
	if err != nil {
		return fail(err)
	}
	turn.JobID = job.ID
	// the run's chat: the same agent and rights as here, never listed
	own := storage.Conversation{ProjectID: conv.ProjectID, AgentID: agent.ID, AgentName: agent.Name, CreatedBy: actor.From(ctx), Purpose: RunPurpose,
		Title: truncate(run.def.Name+": "+oneLine(firstNonEmpty(run.rec.Input, run.def.Name)), 80), Mode: conv.Mode, EditMode: conv.EditMode, Effort: conv.Effort}
	if own, err = e.store.Chat().CreateConversation(ctx, own); err != nil {
		e.endJob(job.ID, "", err, nil)
		return fail(err)
	}
	run.rec.CallerConversationID = conv.ID
	// the run's first answer: its own job (the caller's is this chat's)
	rctx := withPrepared(usage.WithJob(ctx, ""), &prepared{conv: own.ID, run: run, prompt: prompt})
	if _, _, err := e.SendWithContext(rctx, own.ID, text, pageContext, attachmentIDs); err != nil {
		e.endJob(job.ID, "", err, nil)
		_ = e.store.Chat().DeleteConversation(context.WithoutCancel(ctx), own.ID)
		return fail(err)
	}
	go e.awaitRun(wctx, turn, conv, own.ID, run)
	return turn, msg, nil
}

// awaitRun gives the chat that called a run its output once the run ends.
func (e *Engine) awaitRun(ctx context.Context, turn *Turn, conv storage.Conversation, ownID string, run *wfRun) {
	defer turn.cancel()
	defer e.finish(turn)
	select {
	case <-run.done:
	case <-ctx.Done(): // Dừng in the chat that called it
		e.StopAll(ownID)
		select {
		case <-run.done:
		case <-time.After(30 * time.Second):
		}
	}
	bg := context.Background()
	rec, err := e.store.WorkflowRuns().Get(bg, run.rec.ID)
	if err != nil {
		rec = run.rec
	}
	role, content := "assistant", strings.TrimSpace(rec.Result)
	var runErr, stopped error
	switch rec.Status {
	case storage.RunDone:
		if content == "" {
			content = "Quy trình " + rec.WorkflowName + " đã xong."
		}
	case storage.RunStopped:
		role, content, stopped = "error", "⏹️ Đã dừng quy trình "+rec.WorkflowName+".", context.Canceled
	default:
		role, content = "error", "⚠️ Quy trình "+rec.WorkflowName+" không xong: "+firstNonEmpty(rec.Error, "lỗi không rõ")
		runErr = errors.New(content)
	}
	author := ""
	if role == "assistant" {
		author = rec.CoordinatorName
	}
	m, err := e.store.Chat().AddMessage(bg, storage.Message{ConversationID: conv.ID, Role: role, Content: content, Author: author})
	if err != nil {
		e.endJob(turn.JobID, "", err, nil)
		turn.emit(Event{Type: "error", Text: err.Error()})
		return
	}
	e.endJob(turn.JobID, m.ID, runErr, stopped)
	dto := MessageDTO{ID: m.ID, Role: m.Role, Content: m.Content, Author: m.Author, CreatedAt: m.CreatedAt,
		Tools: []storage.ToolCall{}, Attachments: []storage.Attachment{}, Patches: []PatchDTO{}, Actions: []ActionDTO{}}
	if role == "error" {
		turn.emit(Event{Type: "error", Text: content, Message: &dto})
		return
	}
	turn.emit(Event{Type: "done", Message: &dto})
}
