package sqlite

import (
	"context"
	"database/sql"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ids"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

type consultRepo struct{ db dbtx }

const consultCols = `id, side, peer_id, remote_id, conversation_id, project_id, caller_agent_id, actor, ceiling, root_message_id,
	expert_project, expert_agent_id, expert_name, expert_project_name, from_label, question, consult_conversation_id,
	status, answer, error, deadline, created_at, done_at`

func scanConsult(row scanner) (storage.Consult, error) {
	var (
		c                 storage.Consult
		deadline, created string
		done              sql.NullString
	)
	if err := row.Scan(&c.ID, &c.Side, &c.PeerID, &c.RemoteID, &c.ConversationID, &c.ProjectID, &c.CallerAgentID, &c.Actor, &c.Ceiling, &c.RootMessageID,
		&c.ExpertProject, &c.ExpertAgentID, &c.ExpertName, &c.ExpertProjectName, &c.FromLabel, &c.Question, &c.ConsultConvID,
		&c.Status, &c.Answer, &c.Error, &deadline, &created, &done); err != nil {
		return c, notFound(err)
	}
	if err := parseTimes([]*time.Time{&c.Deadline, &c.CreatedAt}, deadline, created); err != nil {
		return c, err
	}
	var err error
	c.DoneAt, err = optParse(done)
	return c, err
}

func (r consultRepo) Create(ctx context.Context, c storage.Consult) (storage.Consult, error) {
	if c.ID == "" {
		c.ID = ids.New("cst")
	}
	if c.Status == "" {
		c.Status = storage.ConsultPending
	}
	c.CreatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `INSERT INTO consults (`+consultCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Side, c.PeerID, c.RemoteID, c.ConversationID, c.ProjectID, c.CallerAgentID, c.Actor, c.Ceiling, c.RootMessageID,
		c.ExpertProject, c.ExpertAgentID, c.ExpertName, c.ExpertProjectName, c.FromLabel, c.Question, c.ConsultConvID,
		c.Status, c.Answer, c.Error, fmtTime(c.Deadline), fmtTime(c.CreatedAt), optTime(c.DoneAt))
	return c, err
}

func (r consultRepo) Get(ctx context.Context, id string) (storage.Consult, error) {
	return scanConsult(r.db.QueryRowContext(ctx, `SELECT `+consultCols+` FROM consults WHERE id=?`, id))
}

func (r consultRepo) GetIn(ctx context.Context, peerID, remoteID string) (storage.Consult, error) {
	return scanConsult(r.db.QueryRowContext(ctx, `SELECT `+consultCols+` FROM consults WHERE side='in' AND peer_id=? AND remote_id=?`, peerID, remoteID))
}

func (r consultRepo) Finish(ctx context.Context, id, status, answer, errText string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE consults SET status=?, answer=?, error=?, done_at=? WHERE id=? AND status='pending'`,
		status, answer, errText, fmtTime(time.Now()), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r consultRepo) SetConsultConversation(ctx context.Context, id, conversationID string) error {
	return execOne(ctx, r.db, `UPDATE consults SET consult_conversation_id=? WHERE id=?`, conversationID, id)
}

func (r consultRepo) list(ctx context.Context, where string, args ...any) ([]storage.Consult, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+consultCols+` FROM consults WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.Consult
	for rows.Next() {
		c, err := scanConsult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r consultRepo) Asked(ctx context.Context, conversationID string) ([]storage.Consult, error) {
	return r.list(ctx, `conversation_id=? AND side IN ('out','local') AND status='pending' ORDER BY created_at`, conversationID)
}

func (r consultRepo) Waiting(ctx context.Context, consultConversationID string) ([]storage.Consult, error) {
	return r.list(ctx, `consult_conversation_id=? AND side IN ('in','local') AND status='pending' ORDER BY created_at`, consultConversationID)
}

func (r consultRepo) CountRoot(ctx context.Context, conversationID, rootMessageID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM consults WHERE conversation_id=? AND root_message_id=? AND side IN ('out','local')`,
		conversationID, rootMessageID).Scan(&n)
	return n, err
}

func (r consultRepo) CountInSince(ctx context.Context, peerID string, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM consults WHERE side='in' AND peer_id=? AND created_at>=?`, peerID, fmtTime(since)).Scan(&n)
	return n, err
}

func (r consultRepo) Expired(ctx context.Context, now time.Time) ([]storage.Consult, error) {
	return r.list(ctx, `status='pending' AND deadline<? ORDER BY created_at`, fmtTime(now))
}
