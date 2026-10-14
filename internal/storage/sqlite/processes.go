package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ids"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

type processRepo struct{ db dbtx }

const processCols = `id, project_id, name, command, cwd, kind, source, autostart, autorestart, env, created_at, updated_at`

func scanProcess(row scanner) (storage.Process, error) {
	var (
		p                     storage.Process
		env, created, updated string
	)
	if err := row.Scan(&p.ID, &p.ProjectID, &p.Name, &p.Command, &p.Cwd, &p.Kind, &p.Source, &p.Autostart, &p.Autorestart, &env, &created, &updated); err != nil {
		return p, notFound(err)
	}
	_ = json.Unmarshal([]byte(env), &p.Env)
	var err error
	if p.CreatedAt, err = parseTime(created); err != nil {
		return p, err
	}
	p.UpdatedAt, err = parseTime(updated)
	return p, err
}

func (r processRepo) Create(ctx context.Context, p storage.Process) (storage.Process, error) {
	if p.ID == "" {
		p.ID = ids.New("proc")
	}
	if p.Kind == "" {
		p.Kind = "service"
	}
	p.CreatedAt = time.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	_, err := r.db.ExecContext(ctx, `INSERT INTO processes (`+processCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.ProjectID, p.Name, p.Command, p.Cwd, p.Kind, p.Source, boolInt(p.Autostart), boolInt(p.Autorestart), jsonMap(p.Env), fmtTime(p.CreatedAt), fmtTime(p.UpdatedAt))
	if isUnique(err) {
		return storage.Process{}, storage.ErrConflict
	}
	return p, err
}

// Update applies p, refusing with storage.ErrStale if the row changed since
// p was read (CAS on updated_at) so a concurrent PATCH can't silently
// overwrite another one's fields, storage.ErrNotFound if it was deleted
// meanwhile, and storage.ErrConflict only for the name-uniqueness violation
// (kept distinct so the handler can tell the two apart).
func (r processRepo) Update(ctx context.Context, p storage.Process) error {
	prevUpdatedAt := p.UpdatedAt
	p.UpdatedAt = time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `UPDATE processes SET name=?, command=?, cwd=?, kind=?, autostart=?, autorestart=?, env=?, updated_at=? WHERE id=? AND updated_at=?`,
		p.Name, p.Command, p.Cwd, p.Kind, boolInt(p.Autostart), boolInt(p.Autorestart), jsonMap(p.Env), fmtTime(p.UpdatedAt), p.ID, fmtTime(prevUpdatedAt))
	if isUnique(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, getErr := r.Get(ctx, p.ID); errors.Is(getErr, storage.ErrNotFound) {
			return storage.ErrNotFound
		}
		return storage.ErrStale
	}
	return nil
}

func (r processRepo) Get(ctx context.Context, id string) (storage.Process, error) {
	return scanProcess(r.db.QueryRowContext(ctx, `SELECT `+processCols+` FROM processes WHERE id=?`, id))
}

func (r processRepo) List(ctx context.Context, projectID string) ([]storage.Process, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+processCols+` FROM processes WHERE (?='' OR project_id=?) ORDER BY created_at, name`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storage.Process{}
	for rows.Next() {
		p, err := scanProcess(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r processRepo) Delete(ctx context.Context, id string) error {
	return execOne(ctx, r.db, `DELETE FROM processes WHERE id=?`, id)
}
