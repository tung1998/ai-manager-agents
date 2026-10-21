package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ids"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// The support pack's data (ADR-171).

type supportRepo struct{ db dbtx }

func (s *Store) Support() storage.SupportRepo { return supportRepo{s.q} }

const sourceCols = `id, project_id, kind, name, automation_id, mode, token_hash, config, rate_per_min, enabled, created_at, updated_at`

func scanSource(row scanner) (storage.SupportSource, error) {
	var (
		x                        storage.SupportSource
		config, created, updated string
	)
	if err := row.Scan(&x.ID, &x.ProjectID, &x.Kind, &x.Name, &x.AutomationID, &x.Mode, &x.TokenHash, &config, &x.RatePerMin, &x.Enabled, &created, &updated); err != nil {
		return x, notFound(err)
	}
	_ = json.Unmarshal([]byte(config), &x.Config)
	if x.Config == nil {
		x.Config = map[string]any{}
	}
	return x, parseTimes([]*time.Time{&x.CreatedAt, &x.UpdatedAt}, created, updated)
}

func (r supportRepo) CreateSource(ctx context.Context, x storage.SupportSource) (storage.SupportSource, error) {
	now := time.Now().UTC()
	x.ID, x.CreatedAt, x.UpdatedAt = ids.New("sps"), now, now
	if x.Config == nil {
		x.Config = map[string]any{}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO support_sources (`+sourceCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.ProjectID, x.Kind, x.Name, x.AutomationID, x.Mode, x.TokenHash, toJSON(x.Config), x.RatePerMin, x.Enabled, fmtTime(now), fmtTime(now))
	return x, err
}

func (r supportRepo) UpdateSource(ctx context.Context, x storage.SupportSource) error {
	if x.Config == nil {
		x.Config = map[string]any{}
	}
	res, err := r.db.ExecContext(ctx, `UPDATE support_sources SET name=?, automation_id=?, mode=?, token_hash=?, config=?, rate_per_min=?, enabled=?, updated_at=? WHERE id=?`,
		x.Name, x.AutomationID, x.Mode, x.TokenHash, toJSON(x.Config), x.RatePerMin, x.Enabled, fmtTime(time.Now()), x.ID)
	return affected(res, err)
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (r supportRepo) Source(ctx context.Context, id string) (storage.SupportSource, error) {
	return scanSource(r.db.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM support_sources WHERE id=?`, id))
}

func (r supportRepo) Sources(ctx context.Context, projectID string) ([]storage.SupportSource, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sourceCols+` FROM support_sources WHERE project_id=? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storage.SupportSource{}
	for rows.Next() {
		x, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

const ticketCols = `id, project_id, source_id, external_thread, customer_id, subject, status, case_type, assignee, priority, tags,
	first_response_at, resolved_at, created_at, updated_at`

func scanTicket(row scanner) (storage.SupportTicket, error) {
	var (
		t                      storage.SupportTicket
		tags, created, updated string
		first, resolved        sql.NullString
	)
	if err := row.Scan(&t.ID, &t.ProjectID, &t.SourceID, &t.ExternalThread, &t.CustomerID, &t.Subject, &t.Status, &t.CaseType, &t.Assignee, &t.Priority, &tags,
		&first, &resolved, &created, &updated); err != nil {
		return t, notFound(err)
	}
	_ = json.Unmarshal([]byte(tags), &t.Tags)
	if t.Tags == nil {
		t.Tags = []string{}
	}
	var err error
	if t.FirstResponseAt, err = optParse(first); err != nil {
		return t, err
	}
	if t.ResolvedAt, err = optParse(resolved); err != nil {
		return t, err
	}
	return t, parseTimes([]*time.Time{&t.CreatedAt, &t.UpdatedAt}, created, updated)
}

func (r supportRepo) CreateTicket(ctx context.Context, t storage.SupportTicket) (storage.SupportTicket, error) {
	now := time.Now().UTC()
	t.ID, t.CreatedAt, t.UpdatedAt = ids.New("tkt"), now, now
	if t.Status == "" {
		t.Status = "new"
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO support_tickets (`+ticketCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ProjectID, t.SourceID, t.ExternalThread, t.CustomerID, t.Subject, t.Status, t.CaseType, t.Assignee, t.Priority, toJSON(t.Tags),
		optTime(t.FirstResponseAt), optTime(t.ResolvedAt), fmtTime(now), fmtTime(now))
	return t, err
}

func (r supportRepo) UpdateTicket(ctx context.Context, t storage.SupportTicket) error {
	if t.Tags == nil {
		t.Tags = []string{}
	}
	res, err := r.db.ExecContext(ctx, `UPDATE support_tickets SET customer_id=?, subject=?, status=?, case_type=?, assignee=?, priority=?, tags=?,
		first_response_at=?, resolved_at=?, updated_at=? WHERE id=?`,
		t.CustomerID, t.Subject, t.Status, t.CaseType, t.Assignee, t.Priority, toJSON(t.Tags), optTime(t.FirstResponseAt), optTime(t.ResolvedAt), fmtTime(time.Now()), t.ID)
	return affected(res, err)
}

func (r supportRepo) Ticket(ctx context.Context, id string) (storage.SupportTicket, error) {
	return scanTicket(r.db.QueryRowContext(ctx, `SELECT `+ticketCols+` FROM support_tickets WHERE id=?`, id))
}

func (r supportRepo) OpenTicket(ctx context.Context, sourceID, thread string) (storage.SupportTicket, error) {
	return scanTicket(r.db.QueryRowContext(ctx, `SELECT `+ticketCols+` FROM support_tickets
		WHERE source_id=? AND external_thread=? AND status NOT IN ('resolved', 'closed') ORDER BY created_at DESC LIMIT 1`, sourceID, thread))
}

func (r supportRepo) Tickets(ctx context.Context, projectID, status string, limit int) ([]storage.SupportTicket, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q, args := `SELECT `+ticketCols+` FROM support_tickets WHERE project_id=?`, []any{projectID}
	if status != "" {
		q, args = q+` AND status=?`, append(args, status)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY updated_at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storage.SupportTicket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const supportEventCols = `id, ticket_id, source_id, kind, body, actor, external_id, job_id, conversation_id, reply_to, state, meta, created_at`

func scanEvent(row scanner) (storage.SupportEvent, error) {
	var (
		e             storage.SupportEvent
		meta, created string
	)
	if err := row.Scan(&e.ID, &e.TicketID, &e.SourceID, &e.Kind, &e.Body, &e.Actor, &e.ExternalID, &e.JobID, &e.ConversationID, &e.ReplyTo, &e.State, &meta, &created); err != nil {
		return e, notFound(err)
	}
	_ = json.Unmarshal([]byte(meta), &e.Meta)
	var err error
	e.CreatedAt, err = parseTime(created)
	return e, err
}

func (r supportRepo) AddEvent(ctx context.Context, e storage.SupportEvent) (storage.SupportEvent, error) {
	e.ID, e.CreatedAt = ids.New("tev"), time.Now().UTC()
	if e.Meta == nil {
		e.Meta = map[string]any{}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO support_events (`+supportEventCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.TicketID, e.SourceID, e.Kind, e.Body, e.Actor, e.ExternalID, e.JobID, e.ConversationID, e.ReplyTo, e.State, toJSON(e.Meta), fmtTime(e.CreatedAt))
	if isUnique(err) {
		return e, storage.ErrConflict
	}
	return e, err
}

func (r supportRepo) UpdateEvent(ctx context.Context, e storage.SupportEvent) error {
	if e.Meta == nil {
		e.Meta = map[string]any{}
	}
	res, err := r.db.ExecContext(ctx, `UPDATE support_events SET body=?, state=?, job_id=?, conversation_id=?, meta=? WHERE id=?`,
		e.Body, e.State, e.JobID, e.ConversationID, toJSON(e.Meta), e.ID)
	return affected(res, err)
}

func (r supportRepo) Event(ctx context.Context, id string) (storage.SupportEvent, error) {
	return scanEvent(r.db.QueryRowContext(ctx, `SELECT `+supportEventCols+` FROM support_events WHERE id=?`, id))
}

func (r supportRepo) EventByExternal(ctx context.Context, sourceID, externalID string) (storage.SupportEvent, error) {
	return scanEvent(r.db.QueryRowContext(ctx, `SELECT `+supportEventCols+` FROM support_events WHERE source_id=? AND external_id=?`, sourceID, externalID))
}

func (r supportRepo) EventByJob(ctx context.Context, jobID string) (storage.SupportEvent, error) {
	return scanEvent(r.db.QueryRowContext(ctx, `SELECT `+supportEventCols+` FROM support_events WHERE job_id=? ORDER BY created_at LIMIT 1`, jobID))
}

func (r supportRepo) Events(ctx context.Context, ticketID string) ([]storage.SupportEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+supportEventCols+` FROM support_events WHERE ticket_id=? ORDER BY created_at, id`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storage.SupportEvent{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
