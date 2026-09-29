package main

import (
	"bitbucket.org/senprints/agent-office/internal/storage"
	"context"
	"encoding/json"
	"errors"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/perm"
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
	if conversationID == "" {
		conv, err := x.chat.StartConversation(ctx, projectID, agentID)
		if err != nil {
			return "", "", err
		}
		conversationID = conv.ID
		if err := x.chat.SetMode(ctx, conv.ID, perm.Operate); err != nil {
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
	reply := ""
	for seq := 0; ; {
		evs, done, wake := turn.Since(seq)
		seq += len(evs)
		for _, e := range evs {
			if e.Type == "error" {
				return conversationID, "", errors.New(e.Text)
			}
			if e.Type == "done" && e.Message != nil {
				reply = e.Message.Content
			}
		}
		if done {
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
