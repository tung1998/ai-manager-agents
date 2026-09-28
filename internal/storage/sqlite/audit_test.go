package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// A row written before migration 00024 (old columns only) still reads back.
func TestAuditLegacyRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "office.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO audit_log (id, actor, action, target, detail, at) VALUES ('aud_old','human:a@x.io','project.update','prj_9','{}','2026-01-01T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Audit().List(ctx, storage.AuditFilter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %d %v", len(rows), err)
	}
	if r := rows[0]; r.ID != "aud_old" || !r.OK || r.Before != nil || r.After != nil || r.Actor != "human:a@x.io" {
		t.Fatalf("legacy row: %+v", r)
	}
}
