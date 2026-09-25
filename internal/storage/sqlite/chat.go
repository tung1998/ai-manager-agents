package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ids"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

type chatRepo struct{ db dbtx }

const convCols = `id, project_id, agent_id, agent_name, title, session_id, runtime, created_by, created_at, updated_at`

func scanConv(row scanner) (storage.Conversation, error) {
	var (
		c                storage.Conversation
		agent            sql.NullString
		created, updated string
	)
	if err := row.Scan(&c.ID, &c.ProjectID, &agent, &c.AgentName, &c.Title, &c.SessionID, &c.Runtime, &c.CreatedBy, &created, &updated); err != nil {
		return c, notFound(err)
	}
	c.AgentID = agent.String
	return c, parseTimes([]*time.Time{&c.CreatedAt, &c.UpdatedAt}, created, updated)
}

func (r chatRepo) CreateConversation(ctx context.Context, c storage.Conversation) (storage.Conversation, error) {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = ids.New("cnv")
	}
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := r.db.ExecContext(ctx, `INSERT INTO conversations (`+convCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.ProjectID, nullStr(c.AgentID), c.AgentName, c.Title, c.SessionID, c.Runtime, c.CreatedBy, fmtTime(now), fmtTime(now))
	return c, err
}

func (r chatRepo) UpdateConversation(ctx context.Context, c storage.Conversation) error {
	return execOne(ctx, r.db, `UPDATE conversations SET agent_id=?, agent_name=?, title=?, session_id=?, runtime=?, updated_at=? WHERE id=?`,
		nullStr(c.AgentID), c.AgentName, c.Title, c.SessionID, c.Runtime, fmtTime(time.Now()), c.ID)
}

func (r chatRepo) GetConversation(ctx context.Context, id string) (storage.Conversation, error) {
	return scanConv(r.db.QueryRowContext(ctx, `SELECT `+convCols+` FROM conversations WHERE id=?`, id))
}

func (r chatRepo) ListConversations(ctx context.Context, projectID string, limit int) ([]storage.Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+convCols+` FROM conversations WHERE project_id=? ORDER BY updated_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.Conversation
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r chatRepo) DeleteConversation(ctx context.Context, id string) error {
	return execOne(ctx, r.db, `DELETE FROM conversations WHERE id=?`, id)
}

func (r chatRepo) AddMessage(ctx context.Context, m storage.Message) (storage.Message, error) {
	if m.ID == "" {
		m.ID = ids.New("msg")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.Tools == nil {
		m.Tools = []storage.ToolCall{}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO messages (id, conversation_id, role, content, tools, run_id, author, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Role, m.Content, toJSON(m.Tools), nullStr(m.RunID), m.Author, fmtTime(m.CreatedAt))
	if err == nil {
		_, _ = r.db.ExecContext(ctx, `UPDATE conversations SET updated_at=? WHERE id=?`, fmtTime(m.CreatedAt), m.ConversationID)
	}
	return m, err
}

func (r chatRepo) ListMessages(ctx context.Context, conversationID string) ([]storage.Message, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, conversation_id, role, content, tools, run_id, author, created_at
		FROM messages WHERE conversation_id=? ORDER BY created_at, id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.Message
	for rows.Next() {
		var (
			m              storage.Message
			tools, created string
			run            sql.NullString
		)
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &tools, &run, &m.Author, &created); err != nil {
			return nil, err
		}
		m.RunID = run.String
		if err := json.Unmarshal([]byte(tools), &m.Tools); err != nil {
			return nil, err
		}
		if m.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const patchCols = `id, conversation_id, message_id, diff, files, status, detail, decided_by, decided_at, created_at`

func scanPatch(row scanner) (storage.Patch, error) {
	var (
		p              storage.Patch
		files, created string
		decided        sql.NullString
	)
	if err := row.Scan(&p.ID, &p.ConversationID, &p.MessageID, &p.Diff, &files, &p.Status, &p.Detail, &p.DecidedBy, &decided, &created); err != nil {
		return p, notFound(err)
	}
	if err := json.Unmarshal([]byte(files), &p.Files); err != nil {
		return p, err
	}
	if decided.Valid {
		t, err := parseTime(decided.String)
		if err != nil {
			return p, err
		}
		p.DecidedAt = &t
	}
	var err error
	p.CreatedAt, err = parseTime(created)
	return p, err
}

func (r chatRepo) AddPatch(ctx context.Context, p storage.Patch) (storage.Patch, error) {
	if p.ID == "" {
		p.ID = ids.New("pat")
	}
	if p.Status == "" {
		p.Status = "pending"
	}
	if p.Files == nil {
		p.Files = []string{}
	}
	p.CreatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `INSERT INTO patches (`+patchCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.ConversationID, p.MessageID, p.Diff, toJSON(p.Files), p.Status, p.Detail, p.DecidedBy, nil, fmtTime(p.CreatedAt))
	return p, err
}

func (r chatRepo) GetPatch(ctx context.Context, id string) (storage.Patch, error) {
	return scanPatch(r.db.QueryRowContext(ctx, `SELECT `+patchCols+` FROM patches WHERE id=?`, id))
}

func (r chatRepo) ListPatches(ctx context.Context, conversationID string) ([]storage.Patch, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+patchCols+` FROM patches WHERE conversation_id=? ORDER BY created_at, id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.Patch
	for rows.Next() {
		p, err := scanPatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r chatRepo) DecidePatch(ctx context.Context, id, status, detail, by string, at time.Time) error {
	return execOne(ctx, r.db, `UPDATE patches SET status=?, detail=?, decided_by=?, decided_at=? WHERE id=?`, status, detail, by, fmtTime(at), id)
}
