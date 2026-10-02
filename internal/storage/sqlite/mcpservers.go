package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"bitbucket.org/senprints/agent-office/internal/ids"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

func (s *Store) MCPServers() storage.MCPServerRepo { return mcpServerRepo{s.q} }

type mcpServerRepo struct{ db dbtx }

const mcpServerCols = `id, name, kind, url, command, args, env_enc, headers_enc, scope, origin, enabled,
	last_check_at, last_check_status, last_check_error, last_tools, created_at, updated_at`

func scanMCPServer(row scanner) (storage.MCPServer, error) {
	var (
		m                storage.MCPServer
		args, tools      string
		last             sql.NullString
		created, updated string
	)
	if err := row.Scan(&m.ID, &m.Name, &m.Kind, &m.URL, &m.Command, &args, &m.EnvEnc, &m.HeadersEnc, &m.Scope, &m.Origin, &m.Enabled,
		&last, &m.LastCheckStatus, &m.LastCheckError, &tools, &created, &updated); err != nil {
		return m, notFound(err)
	}
	_ = json.Unmarshal([]byte(args), &m.Args)
	_ = json.Unmarshal([]byte(tools), &m.LastTools)
	normMCPServer(&m)
	var err error
	if m.LastCheckAt, err = optParse(last); err != nil {
		return m, err
	}
	return m, parseTimes([]*time.Time{&m.CreatedAt, &m.UpdatedAt}, created, updated)
}

func normMCPServer(m *storage.MCPServer) {
	if m.Args == nil {
		m.Args = []string{}
	}
	if m.LastTools == nil {
		m.LastTools = []storage.MCPTool{}
	}
	if m.Kind == "" {
		m.Kind = "http"
	}
	if m.Scope == "" {
		m.Scope = "machine"
	}
	if m.Origin == "" {
		m.Origin = "manual"
	}
}

func (r mcpServerRepo) Create(ctx context.Context, m storage.MCPServer) (storage.MCPServer, error) {
	now := time.Now().UTC()
	m.ID, m.CreatedAt, m.UpdatedAt = ids.New("mcp"), now, now
	normMCPServer(&m)
	_, err := r.db.ExecContext(ctx, `INSERT INTO mcp_servers (`+mcpServerCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,NULL,'','','[]',?,?)`,
		m.ID, m.Name, m.Kind, m.URL, m.Command, toJSON(m.Args), m.EnvEnc, m.HeadersEnc, m.Scope, m.Origin, m.Enabled, fmtTime(now), fmtTime(now))
	if isUnique(err) {
		return storage.MCPServer{}, storage.ErrConflict
	}
	return m, err
}

func (r mcpServerRepo) Update(ctx context.Context, m storage.MCPServer) error {
	normMCPServer(&m)
	err := execOne(ctx, r.db, `UPDATE mcp_servers SET name=?, kind=?, url=?, command=?, args=?, env_enc=?, headers_enc=?, scope=?, origin=?, enabled=?, updated_at=? WHERE id=?`,
		m.Name, m.Kind, m.URL, m.Command, toJSON(m.Args), m.EnvEnc, m.HeadersEnc, m.Scope, m.Origin, m.Enabled, fmtTime(time.Now()), m.ID)
	if isUnique(err) {
		return storage.ErrConflict
	}
	return err
}

func (r mcpServerRepo) Get(ctx context.Context, id string) (storage.MCPServer, error) {
	return scanMCPServer(r.db.QueryRowContext(ctx, `SELECT `+mcpServerCols+` FROM mcp_servers WHERE id=?`, id))
}

func (r mcpServerRepo) GetByName(ctx context.Context, name string) (storage.MCPServer, error) {
	return scanMCPServer(r.db.QueryRowContext(ctx, `SELECT `+mcpServerCols+` FROM mcp_servers WHERE name=?`, name))
}

func (r mcpServerRepo) List(ctx context.Context) ([]storage.MCPServer, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+mcpServerCols+` FROM mcp_servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storage.MCPServer{}
	for rows.Next() {
		m, err := scanMCPServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r mcpServerRepo) Delete(ctx context.Context, id string) error {
	return execOne(ctx, r.db, `DELETE FROM mcp_servers WHERE id=?`, id)
}

func (r mcpServerRepo) SetCheck(ctx context.Context, id, status, errMsg string, tools []storage.MCPTool, at time.Time) error {
	if tools == nil {
		tools = []storage.MCPTool{}
	}
	return execOne(ctx, r.db, `UPDATE mcp_servers SET last_check_at=?, last_check_status=?, last_check_error=?, last_tools=? WHERE id=?`,
		fmtTime(at), status, errMsg, toJSON(tools), id)
}
