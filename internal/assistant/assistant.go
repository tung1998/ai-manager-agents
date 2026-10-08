// Package assistant sets up the office assistant (ADR-046): a hidden system
// project "Office" with one agent that works across projects through the
// office tools in office scope. Its chats reuse the chat engine as they are.
package assistant

import (
	"context"
	"errors"
	"os"

	"bitbucket.org/senprints/agent-office/internal/prompts"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

const settingKey = "office_assistant_project"

// Instructions of the assistant agent.
var Instructions = prompts.Text("assistant/instructions")

// ID is the assistant's project id ("" = not set up).
func ID(ctx context.Context, store storage.Store) string {
	var id string
	if ok, _ := store.Settings().Get(ctx, settingKey, &id); ok {
		return id
	}
	return ""
}

// Ensure sets the assistant up once (dir is its empty working folder) and
// returns its project id.
func Ensure(ctx context.Context, store storage.Store, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if id := ID(ctx, store); id != "" {
		if repo, err := store.Repos().Get(ctx, id); err == nil {
			refresh(ctx, store, repo)
			return id, nil
		}
	}
	repo, err := store.Repos().Create(ctx, storage.Repo{Name: "Office", Path: dir, Description: "Trợ lý toàn office (dự án hệ thống, không hiện trong danh sách)"})
	if err != nil {
		return "", err
	}
	a, err := store.Agents().Create(ctx, storage.Agent{
		ProjectID: repo.ID, Key: "office-assistant", Name: "Trợ lý office", Role: "Trợ lý toàn office: điều phối, thống kê, cài đặt",
		Instructions: Instructions, ModelTier: storage.TierBalanced,
		Permissions: storage.Permissions{Level: "read", ReadOnly: true},
		Avatar:      storage.Avatar{Color: "violet", Icon: "i-lucide-sparkles"},
	})
	if err != nil {
		return "", err
	}
	repo.DefaultAgentID = a.ID
	if err := store.Repos().Update(ctx, repo); err != nil {
		return "", err
	}
	return repo.ID, store.Settings().Set(ctx, settingKey, repo.ID)
}

// refresh gives an assistant made by an older office today's instructions
// (its project is hidden: nobody edits them by hand), as English prompts (ADR-121).
func refresh(ctx context.Context, store storage.Store, repo storage.Repo) {
	a, err := store.Agents().Get(ctx, repo.DefaultAgentID)
	if err != nil || a.Key != "office-assistant" || a.Instructions == Instructions {
		return
	}
	a.Instructions = Instructions
	_ = store.Agents().Update(ctx, a)
}

// The assistant's rights (ADR-059).
const (
	ModeAnswer = "answer" // reads the office and answers; proposes nothing
	ModeManage = "manage" // + proposes changes on approval cards, hands work to projects (the default)
	ModeAdmin  = "admin"  // + every command on the machine office runs on (for an admin of the office)
)

const modeKey = "assistant.mode"

// Mode is the assistant's rights as set (ModeManage when never set).
func Mode(ctx context.Context, store storage.Store) string {
	var m string
	if ok, _ := store.Settings().Get(ctx, modeKey, &m); ok && (m == ModeAnswer || m == ModeManage || m == ModeAdmin) {
		return m
	}
	return ModeManage
}

// SetMode sets the assistant's rights.
func SetMode(ctx context.Context, store storage.Store, mode string) error {
	if mode != ModeAnswer && mode != ModeManage && mode != ModeAdmin {
		return errors.New("quyền của trợ lý phải là answer, manage hoặc admin")
	}
	return store.Settings().Set(ctx, modeKey, mode)
}

// Powers is what the assistant may do for one person: administrator only for
// an admin of the office, anyone else gets it as manage.
func Powers(mode string, admin bool) string {
	if mode == ModeAdmin && !admin {
		return ModeManage
	}
	return mode
}
