package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/tasks"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// officeExecutor runs automation jobs as chats and tasks (ADR-040). Agents
// work within their own permissions: the mode sets no extra ceiling.
type officeExecutor struct {
	chat  *chat.Engine
	tasks *tasks.Service
}

func (x officeExecutor) RunChat(ctx context.Context, projectID, agentID, conversationID, prompt, editMode string) (string, string, error) {
	ctx = chat.WithModelTier(ctx, trigger.ModelTierOf(ctx)) // the automation's model choice
	ctx = chat.WithSkill(chat.WithInstructions(ctx, trigger.InstructionsOf(ctx)), trigger.SkillOf(ctx))
	untrusted := trigger.UntrustedOf(ctx)
	if untrusted { // outsiders drove it (a bot's escalation): no tools, read only
		ctx = chat.WithNoTools(ctx)
	}
	if conversationID == "" {
		purpose, mode := "", perm.Operate
		if untrusted {
			purpose, mode = "channel", perm.Read
		}
		conv, err := x.chat.StartConversationPurpose(ctx, projectID, agentID, purpose)
		if err != nil {
			return "", "", err
		}
		conversationID = conv.ID
		if err := x.chat.SetMode(ctx, conv.ID, mode); err != nil {
			return conversationID, "", err
		}
		if editMode != "" {
			if err := x.chat.SetEditMode(ctx, conv.ID, editMode); err != nil {
				return conversationID, "", err
			}
		}
	}
	turn, _, err := x.chat.Send(ctx, conversationID, prompt, nil)
	if errors.Is(err, chat.ErrBusy) {
		return conversationID, "", trigger.ErrBusy
	}
	if err != nil {
		return conversationID, "", err
	}
	reply, replyID, author := "", "", ""
	for seq := 0; ; {
		evs, done, wake := turn.Since(seq)
		seq += len(evs)
		for _, e := range evs {
			if e.Type == "error" {
				return conversationID, "", errors.New(e.Text)
			}
			if e.Type == "done" && e.Message != nil {
				reply, replyID, author = e.Message.Content, e.Message.ID, e.Message.Author
			}
		}
		if done {
			if fn := trigger.FollowUpOf(ctx); fn != nil && replyID != "" {
				go x.followUps(conversationID, replyID, author, fn)
			}
			return conversationID, reply, nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			turn.Cancel()
			return conversationID, "", ctx.Err()
		}
	}
}

func (x officeExecutor) RunTask(ctx context.Context, projectID, agentID, goal, editMode string) (string, error) {
	ctx = chat.WithModelTier(ctx, trigger.ModelTierOf(ctx))
	return x.start(ctx, projectID, agentID, goal, 0, nil, perm.Operate, editMode)
}

func (x officeExecutor) RunQueuedTask(ctx context.Context, projectID, payload string) (string, error) {
	var q tasks.QueuedTask
	if err := json.Unmarshal([]byte(payload), &q); err != nil {
		return "", err
	}
	return x.start(ctx, projectID, q.AgentID, q.Goal, q.BudgetUSD, q.Attachments, q.Mode, q.EditMode)
}

func (x officeExecutor) start(ctx context.Context, projectID, agentID, goal string, budget float64, files []string, mode, editMode string) (string, error) {
	t, err := x.tasks.StartFor(ctx, projectID, agentID, goal, budget, files, mode, editMode)
	if errors.Is(err, tasks.ErrBusy) {
		return "", trigger.ErrBusy
	}
	if err != nil {
		return "", err
	}
	if live, ok := x.tasks.Live(t.ID); ok {
		for {
			_, done, wake := live.Since(0)
			if done {
				break
			}
			select {
			case <-wake:
			case <-ctx.Done():
				live.Cancel()
				return t.ID, ctx.Err()
			}
		}
	}
	d, err := x.tasks.Get(ctx, t.ID)
	if err != nil {
		return t.ID, err
	}
	if d.Task.Status == "failed" || d.Task.Status == "rejected" {
		return t.ID, errors.New(d.Task.Detail)
	}
	return t.ID, nil
}

// assistantRunner starts what the office assistant proposed and a person
// approved (ADR-046).
type assistantRunner struct {
	store   storage.Store
	tasks   *tasks.Service
	trigger *trigger.Runner
}

func (r assistantRunner) StartTask(ctx context.Context, projectID, agentID, goal string) (string, error) {
	t, err := r.tasks.StartFor(ctx, projectID, agentID, goal, 0, nil, perm.Operate, perm.EditWorktree)
	if errors.Is(err, tasks.ErrBusy) {
		j, qerr := r.tasks.Queue(ctx, projectID, agentID, goal, 0, nil, perm.Operate, perm.EditWorktree)
		return j.ID, qerr
	}
	return t.ID, err
}

func (r assistantRunner) RunAutomation(ctx context.Context, automationID string) (string, error) {
	a, err := r.store.Automations().Get(ctx, automationID)
	if err != nil {
		return "", err
	}
	if trigger.IsChannel(a.Source) {
		return "", errors.New("tự động hóa này chạy khi có tin nhắn tới bot, không chạy tay được")
	}
	j, _, err := r.trigger.Enqueue(ctx, a, "manual", "", "", "")
	return j.ID, err
}

// followUps sends on what the answering agent says after its answer, once the
// agents it gave work to are done (its reports), until the chat is quiet.
func (x officeExecutor) followUps(convID, afterID, author string, send func(string)) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	seen := map[string]bool{}
	past := false // messages up to the answer were sent already
	quiet := 0
	for quiet < 2 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		if len(x.chat.Running(convID)) == 0 {
			quiet++
		} else {
			quiet = 0
		}
		msgs, err := x.chat.History(ctx, convID)
		if err != nil {
			return
		}
		past = false
		for _, m := range msgs {
			if m.ID == afterID {
				past = true
				continue
			}
			if !past || seen[m.ID] {
				continue
			}
			if (m.Role == "assistant" && m.Author == author) || m.Role == "error" {
				seen[m.ID] = true
				if strings.TrimSpace(m.Content) != "" {
					send(m.Content)
				}
			}
			if m.Role == "user" { // the person wrote again: that turn answers on its own
				return
			}
		}
	}
}
