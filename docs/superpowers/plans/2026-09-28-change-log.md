# Nhật ký thay đổi — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mọi thay đổi trong office ghi được *ai* (người/agent/tự động hóa), *ai duyệt*, *nguồn* (chat/job/việc/thẻ), *kênh*, *trước/sau*; xem, lọc và thống kê được.

**Architecture:** Mở rộng `audit_log` (migration 00024) và `AuditRepo` (lọc, phân trang, đếm). Gói mới `internal/audit` giữ "người thực hiện" (`Who`) trong context, che bí mật và dựng dòng nhật ký; mọi chỗ ghi đi qua `audit.Record`. `auditAction` của API chuyển sang gói này; các handler sửa cấu hình truyền trước/sau; duyệt thẻ của agent ghi agent là người làm và người bấm là người duyệt. Dashboard có component `AuditLog` dùng ở trang Nhật ký office, tab Nhật ký trong project và trang chi tiết tự động hóa.

**Tech Stack:** Go (net/http, SQLite modernc, goose), Nuxt 4 / Nuxt UI 4.

**Spec:** `docs/superpowers/specs/2026-09-28-office-assistant-design.md` (mục 3.1)

## Global Constraints

- Chỉ thêm, không sửa, không xóa dòng nhật ký; không tự dọn.
- Bí mật (api key, token, secret, password, hash) ghi là `"***"`, nhật ký chỉ cho biết là đã đổi.
- `actor_kind` ∈ `human | agent | automation | system`; `via` ∈ `ui | chat | task | assistant | mcp | automation | api`.
- Chữ hiển thị qua i18n (`dashboard/CLAUDE.md`), `node scripts/check-i18n.mjs` phải qua.
- Không thêm dependency mới.
- Làm trên `main`, commit theo từng task.

**Spec amendment (ruling):** spec mục 3.5 nói "ghi nhật ký thất bại thì thay đổi thất bại theo (cùng transaction)". Ở phần 1, ~55 handler ghi thẳng vào store, bọc tất cả trong transaction là refactor lớn trùng với registry của phần 2 (một đường Apply). Phần 1: lỗi ghi nhật ký không còn bị nuốt (`_ =`) mà ghi `slog.Error`; tính nguyên tử làm ở phần 2 trong `Apply` của registry. Cost if wrong: trong khoảng giữa hai phần, một thay đổi có thể thiếu dòng nhật ký khi SQLite lỗi ghi (hiếm, có log lỗi).

## Review Focus

1. Dòng nhật ký cũ (trước migration) vẫn hiện được, với `actor_kind`/`resource` suy ra từ `actor`/`action` — test ở Task 1.
2. Bí mật lồng sâu (`config.SecretHash` trong tự động hóa, key trong map) bị che, còn số đếm như `input_tokens` thì không — test ở Task 2.
3. Phân trang khi nhiều dòng trùng thời điểm không lặp/mất dòng — test ở Task 1.
4. Thẻ agent đề xuất được duyệt nhưng chạy thất bại vẫn ghi dòng với `ok=false` và đủ nguồn — test ở Task 4.
5. Thành viên (member) không đọc được `/api/audit` và `/api/audit/stats` — test ở Task 5.

---

### Task 1: Migration + storage (lọc, phân trang, đếm)

**Files:**
- Create: `migrations/sqlite/00024_change_log.sql`
- Modify: `internal/storage/storage.go` (AuditEntry, AuditRepo, AuditFilter, AuditCount)
- Modify: `internal/storage/sqlite/sqlite.go` (auditRepo)
- Modify: `internal/storage/org.go` (Action.JobID), `internal/storage/sqlite/` actions repo (cột job_id)
- Modify callers of `Audit().List(ctx, n)`: `internal/api/handlers.go`, `internal/auth/auth_test.go`, `internal/storage/storagetest/storagetest.go`
- Test: `internal/storage/storagetest/storagetest.go`

**Interfaces:**
- Produces:
```go
type AuditEntry struct {
	ID, Actor, Action, Target string
	Detail map[string]any
	At     time.Time
	ActorKind, ActorID, ActorName, ApprovedBy, Via string
	ProjectID, ConversationID, JobID, TaskID, ActionID string
	Resource, ResourceID string
	Before, After map[string]any // nil = không có
	OK bool
}
type AuditFilter struct {
	ProjectID, Resource, ResourceID, ActorKind, ActorID, ActorName, Via, ConversationID, JobID, TaskID string
	From, To time.Time // zero = không giới hạn
	BeforeID string    // con trỏ: id của dòng cuối trang trước
	Limit    int       // mặc định 100, tối đa 500
}
type AuditCount struct{ Key string; Count, Failed int }
type AuditRepo interface {
	Append(ctx context.Context, e AuditEntry) error
	List(ctx context.Context, f AuditFilter) ([]AuditEntry, error)
	// Count groups by "day" (UTC), "kind", "actor" (kind:name), "resource", "via" or "project".
	Count(ctx context.Context, f AuditFilter, by string) ([]AuditCount, error)
}
// Action gains: JobID string
```

- [ ] **Step 1: Migration**

```sql
-- +goose Up
-- Change log (ADR-043): who (person/agent/automation), who approved, where
-- from (chat/job/task/proposal), which channel, and before/after.
ALTER TABLE audit_log ADD COLUMN actor_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN actor_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN actor_name TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN approved_by TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN via TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN conversation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN action_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN resource TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN resource_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN before_json TEXT CHECK (before_json IS NULL OR json_valid(before_json));
ALTER TABLE audit_log ADD COLUMN after_json TEXT CHECK (after_json IS NULL OR json_valid(after_json));
ALTER TABLE audit_log ADD COLUMN ok INTEGER NOT NULL DEFAULT 1;
-- older rows: actor "human:<email>" or a plain name; resource from "resource.verb"
UPDATE audit_log SET actor_kind = 'human', actor_name = substr(actor, 7), via = 'ui' WHERE actor LIKE 'human:%';
UPDATE audit_log SET actor_kind = 'system', actor_name = actor WHERE actor_kind = '';
UPDATE audit_log SET resource = CASE WHEN instr(action, '.') > 0 THEN substr(action, 1, instr(action, '.') - 1) ELSE action END, resource_id = target;
UPDATE audit_log SET ok = 0 WHERE action LIKE '%failed%' OR action LIKE '%throttled%';
CREATE INDEX audit_log_project ON audit_log(project_id, at);
CREATE INDEX audit_log_resource ON audit_log(resource, resource_id, at);
CREATE INDEX audit_log_actor ON audit_log(actor_kind, actor_name, at);
CREATE INDEX audit_log_conversation ON audit_log(conversation_id);
CREATE INDEX audit_log_job ON audit_log(job_id);
-- the job (chat answer/task run) an agent proposal came from
ALTER TABLE actions ADD COLUMN job_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE actions DROP COLUMN job_id;
DROP INDEX audit_log_job;
DROP INDEX audit_log_conversation;
DROP INDEX audit_log_actor;
DROP INDEX audit_log_resource;
DROP INDEX audit_log_project;
ALTER TABLE audit_log DROP COLUMN ok;
ALTER TABLE audit_log DROP COLUMN after_json;
ALTER TABLE audit_log DROP COLUMN before_json;
ALTER TABLE audit_log DROP COLUMN resource_id;
ALTER TABLE audit_log DROP COLUMN resource;
ALTER TABLE audit_log DROP COLUMN action_id;
ALTER TABLE audit_log DROP COLUMN task_id;
ALTER TABLE audit_log DROP COLUMN job_id;
ALTER TABLE audit_log DROP COLUMN conversation_id;
ALTER TABLE audit_log DROP COLUMN project_id;
ALTER TABLE audit_log DROP COLUMN via;
ALTER TABLE audit_log DROP COLUMN approved_by;
ALTER TABLE audit_log DROP COLUMN actor_name;
ALTER TABLE audit_log DROP COLUMN actor_id;
ALTER TABLE audit_log DROP COLUMN actor_kind;
```

- [ ] **Step 2: Failing storage test** — thay `testAudit` trong storagetest:

```go
func testAudit(t *testing.T, s storage.Store) {
	ctx := context.Background()
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	add := func(e storage.AuditEntry) {
		t.Helper()
		if e.At.IsZero() {
			e.At = at
		}
		if err := s.Audit().Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	add(storage.AuditEntry{Actor: "system", Action: "user.create", Target: "usr_1", Detail: map[string]any{"role": "admin"}, ActorKind: "system", OK: true})
	add(storage.AuditEntry{Action: "automation.update", ActorKind: "agent", ActorName: "Lead", ApprovedBy: "a@x.io", Via: "chat",
		ProjectID: "prj_1", ConversationID: "cnv_1", JobID: "job_1", ActionID: "act_1", Resource: "automation", ResourceID: "aut_1",
		Before: map[string]any{"name": "a"}, After: map[string]any{"name": "b"}, OK: true})
	add(storage.AuditEntry{Action: "automation.update", ActorKind: "human", ActorName: "a@x.io", Via: "ui", ProjectID: "prj_1",
		Resource: "automation", ResourceID: "aut_1", OK: false})

	got, err := s.Audit().List(ctx, storage.AuditFilter{ProjectID: "prj_1"})
	if err != nil || len(got) != 2 {
		t.Fatalf("project filter = %d %v", len(got), err)
	}
	agent, _ := s.Audit().List(ctx, storage.AuditFilter{ActorKind: "agent"})
	if len(agent) != 1 || agent[0].ApprovedBy != "a@x.io" || agent[0].JobID != "job_1" || agent[0].After["name"] != "b" || agent[0].Before["name"] != "a" || !agent[0].OK {
		t.Fatalf("agent row = %+v", agent)
	}
	if got, _ := s.Audit().List(ctx, storage.AuditFilter{ConversationID: "cnv_1"}); len(got) != 1 {
		t.Fatalf("conversation filter = %d", len(got))
	}

	// same timestamp: paging by id neither repeats nor skips
	seen := map[string]bool{}
	before := ""
	for range 5 {
		page, err := s.Audit().List(ctx, storage.AuditFilter{Limit: 1, BeforeID: before})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if seen[page[0].ID] {
			t.Fatalf("page repeated %s", page[0].ID)
		}
		seen[page[0].ID] = true
		before = page[0].ID
	}
	if len(seen) != 3 {
		t.Fatalf("paged %d rows, want 3", len(seen))
	}

	byKind, err := s.Audit().Count(ctx, storage.AuditFilter{ProjectID: "prj_1"}, "kind")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]storage.AuditCount{}
	for _, c := range byKind {
		counts[c.Key] = c
	}
	if counts["agent"].Count != 1 || counts["human"].Count != 1 || counts["human"].Failed != 1 {
		t.Fatalf("count by kind = %+v", byKind)
	}
	if _, err := s.Audit().Count(ctx, storage.AuditFilter{}, "nope"); err == nil {
		t.Fatal("unknown group accepted")
	}
}
```

Dòng kiểu cũ (Review Focus 1): test `TestAuditLegacyRow` trong `internal/storage/sqlite` chèn sau Migrate `INSERT INTO audit_log (id, actor, action, target, detail, at) VALUES ('aud_old','human:a@x.io','project.update','prj_9','{}', <fmtTime>)` (chỉ các cột cũ) và kiểm tra `List` đọc được dòng đó (`OK==true`, `Before==nil`). Câu UPDATE backfill được kiểm trên DB thật của office ở Task 6 bước 5 (`sqlite3 .office/office.db "select actor_kind, actor_name, resource from audit_log limit 5"`).

- [ ] **Step 3: Run — FAIL**

Run: `go test ./internal/storage/... 2>&1 | tail -20`
Expected: lỗi biên dịch (AuditFilter/Count/fields chưa có).

- [ ] **Step 4: Implement** `storage.go` types như Interfaces; `auditRepo` trong sqlite:

```go
const auditCols = `id, actor, action, target, detail, at, actor_kind, actor_id, actor_name, approved_by, via,
	project_id, conversation_id, job_id, task_id, action_id, resource, resource_id, before_json, after_json, ok`

func (r auditRepo) Append(ctx context.Context, e storage.AuditEntry) error {
	if e.ID == "" {
		e.ID = ids.New("aud")
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	detail := e.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	before, err := nullJSON(e.Before)
	if err != nil {
		return err
	}
	after, err := nullJSON(e.After)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO audit_log (`+auditCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Actor, e.Action, e.Target, string(raw), fmtTime(e.At), e.ActorKind, e.ActorID, e.ActorName, e.ApprovedBy, e.Via,
		e.ProjectID, e.ConversationID, e.JobID, e.TaskID, e.ActionID, e.Resource, e.ResourceID, before, after, boolInt(e.OK))
	return err
}

// nullJSON: nil map = SQL NULL (no snapshot), otherwise its JSON.
func nullJSON(m map[string]any) (any, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func auditWhere(f storage.AuditFilter) (string, []any) {
	var conds []string
	var args []any
	eq := func(col, v string) {
		if v != "" {
			conds, args = append(conds, col+" = ?"), append(args, v)
		}
	}
	eq("project_id", f.ProjectID)
	eq("resource", f.Resource)
	eq("resource_id", f.ResourceID)
	eq("actor_kind", f.ActorKind)
	eq("actor_id", f.ActorID)
	eq("actor_name", f.ActorName)
	eq("via", f.Via)
	eq("conversation_id", f.ConversationID)
	eq("job_id", f.JobID)
	eq("task_id", f.TaskID)
	if !f.From.IsZero() {
		conds, args = append(conds, "at >= ?"), append(args, fmtTime(f.From))
	}
	if !f.To.IsZero() {
		conds, args = append(conds, "at < ?"), append(args, fmtTime(f.To))
	}
	if f.BeforeID != "" {
		conds = append(conds, "(at, id) < (SELECT at, id FROM audit_log WHERE id = ?)")
		args = append(args, f.BeforeID)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (r auditRepo) List(ctx context.Context, f storage.AuditFilter) ([]storage.AuditEntry, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	where, args := auditWhere(f)
	rows, err := r.db.QueryContext(ctx, `SELECT `+auditCols+` FROM audit_log`+where+` ORDER BY at DESC, id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.AuditEntry
	for rows.Next() {
		var (
			e              storage.AuditEntry
			raw, at        string
			before, after  sql.NullString
			ok             int
		)
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &raw, &at, &e.ActorKind, &e.ActorID, &e.ActorName, &e.ApprovedBy, &e.Via,
			&e.ProjectID, &e.ConversationID, &e.JobID, &e.TaskID, &e.ActionID, &e.Resource, &e.ResourceID, &before, &after, &ok); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &e.Detail); err != nil {
			return nil, err
		}
		if before.Valid {
			if err := json.Unmarshal([]byte(before.String), &e.Before); err != nil {
				return nil, err
			}
		}
		if after.Valid {
			if err := json.Unmarshal([]byte(after.String), &e.After); err != nil {
				return nil, err
			}
		}
		e.OK = ok == 1
		if e.At, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

var auditGroups = map[string]string{
	"day": "substr(at, 1, 10)", "kind": "actor_kind", "actor": "actor_kind || ':' || actor_name",
	"resource": "resource", "via": "via", "project": "project_id",
}

func (r auditRepo) Count(ctx context.Context, f storage.AuditFilter, by string) ([]storage.AuditCount, error) {
	expr, ok := auditGroups[by]
	if !ok {
		return nil, fmt.Errorf("không nhóm được theo %q", by)
	}
	f.BeforeID = ""
	where, args := auditWhere(f)
	rows, err := r.db.QueryContext(ctx, `SELECT `+expr+` AS k, COUNT(*), SUM(ok = 0) FROM audit_log`+where+` GROUP BY k ORDER BY COUNT(*) DESC, k LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.AuditCount
	for rows.Next() {
		var c storage.AuditCount
		if err := rows.Scan(&c.Key, &c.Count, &c.Failed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
```

`Action.JobID`: thêm vào struct, vào INSERT/SELECT/UPDATE của actions repo (`job_id`). Callers cũ: `handlers.go` gọi `List(ctx, storage.AuditFilter{Limit: limit})` (Task 5 thay hẳn); `auth_test.go` gọi `List(ctx, storage.AuditFilter{Limit: 10})`.

- [ ] **Step 5: Run — PASS**

Run: `go test ./internal/storage/... ./internal/auth/... 2>&1 | tail -5 && go build ./...`
Expected: `ok` cho các gói; build sạch.

- [ ] **Step 6: Commit**

```bash
git add migrations/sqlite/00024_change_log.sql internal/storage internal/auth internal/api/handlers.go
git commit -m "feat(audit): nhật ký thay đổi có người làm, người duyệt, nguồn, trước/sau (ADR-043)"
```

---

### Task 2: Gói `internal/audit`

**Files:**
- Create: `internal/audit/audit.go`
- Test: `internal/audit/audit_test.go`

**Interfaces:**
- Consumes: `storage.AuditEntry`, `storage.AuditRepo` (Task 1); `actor.From(ctx)` (có sẵn).
- Produces:
```go
type Who struct {
	Kind, ID, Name, ApprovedBy, Via string
	ConversationID, JobID, TaskID, ActionID string
}
func With(ctx context.Context, w Who) context.Context
func From(ctx context.Context) (Who, bool) // false: nothing set by With (falls back to actor.From)
type Change struct {
	Action, Resource, ResourceID, ProjectID string
	Before, After any          // structs/maps; nil = no snapshot
	Detail map[string]any
	Err    error               // non-nil = the change failed (ok=0, detail.error)
}
func Snapshot(v any) map[string]any
func Entry(ctx context.Context, c Change) storage.AuditEntry
func Record(ctx context.Context, repo storage.AuditRepo, c Change) error
```

- [ ] **Step 1: Failing tests**

```go
package audit_test

import (
	"context"
	"errors"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/audit"
)

func TestSnapshotRedactsSecrets(t *testing.T) {
	type cfg struct {
		SecretHash string
		Cron       string
	}
	type auto struct {
		Name        string
		Config      cfg
		InputTokens int `json:"input_tokens"`
		APIKey      string `json:"api_key"`
		Empty       string `json:"token"`
		Headers     map[string]any `json:"headers"`
	}
	got := audit.Snapshot(auto{Name: "n", Config: cfg{SecretHash: "abc", Cron: "* * * * *"}, InputTokens: 5, APIKey: "sk-1",
		Headers: map[string]any{"Authorization": "Bearer x", "x_token": "y"}})
	if got["Name"] != "n" || got["input_tokens"] != float64(5) {
		t.Fatalf("plain fields changed: %v", got)
	}
	if got["api_key"] != "***" || got["Config"].(map[string]any)["SecretHash"] != "***" || got["Config"].(map[string]any)["Cron"] != "* * * * *" {
		t.Fatalf("secrets not redacted: %v", got)
	}
	if got["token"] != "" {
		t.Fatalf("empty secret should stay empty (not set), got %v", got["token"])
	}
	h := got["headers"].(map[string]any)
	if h["Authorization"] != "***" || h["x_token"] != "***" {
		t.Fatalf("nested map secrets: %v", h)
	}
	if audit.Snapshot(nil) != nil {
		t.Fatal("nil snapshot")
	}
}

func TestEntryFromWho(t *testing.T) {
	ctx := audit.With(context.Background(), audit.Who{Kind: "agent", Name: "Lead", ApprovedBy: "a@x.io", Via: "chat", ConversationID: "cnv_1", JobID: "job_1", ActionID: "act_1"})
	e := audit.Entry(ctx, audit.Change{Action: "automation.update", ResourceID: "aut_1", ProjectID: "prj_1",
		Before: map[string]any{"name": "a"}, After: map[string]any{"name": "b"}})
	if e.ActorKind != "agent" || e.ActorName != "Lead" || e.ApprovedBy != "a@x.io" || e.Via != "chat" || e.Actor != "agent:Lead" {
		t.Fatalf("who: %+v", e)
	}
	if e.Resource != "automation" || e.Target != "aut_1" || e.ResourceID != "aut_1" || e.ConversationID != "cnv_1" || e.JobID != "job_1" || e.ActionID != "act_1" || !e.OK {
		t.Fatalf("change: %+v", e)
	}
	if e.Before["name"] != "a" || e.After["name"] != "b" {
		t.Fatalf("snapshots: %+v", e)
	}
	failed := audit.Entry(ctx, audit.Change{Action: "automation.update", Err: errors.New("boom")})
	if failed.OK || failed.Detail["error"] != "boom" {
		t.Fatalf("failed change: %+v", failed)
	}
}

func TestEntryFallsBackToActorString(t *testing.T) {
	cases := map[string][2]string{
		"human:a@x.io":       {"human", "a@x.io"},
		"auto:Nightly":       {"automation", "Nightly"},
		"monitor:api":        {"system", "monitor:api"},
		"":                   {"system", "system"},
	}
	for in, want := range cases {
		ctx := context.Background()
		if in != "" {
			ctx = actor.With(ctx, in)
		}
		e := audit.Entry(ctx, audit.Change{Action: "x.y"})
		if e.ActorKind != want[0] || e.ActorName != want[1] {
			t.Errorf("%q → %s/%s, want %v", in, e.ActorKind, e.ActorName, want)
		}
	}
}
```

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/audit/ 2>&1 | tail -5`
Expected: `no non-test Go files` / undefined: audit.

- [ ] **Step 3: Implement**

```go
// Package audit records who changed what (ADR-043): a person, an agent (and
// the person who approved it) or an automation, where it came from (chat,
// job, task, proposal), through which channel, and the value before/after.
// Every write to audit_log goes through Record.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Who did it, and where from.
type Who struct {
	Kind       string // human | agent | automation | system
	ID         string
	Name       string
	ApprovedBy string // the person who approved an agent's proposal
	Via        string // ui | chat | task | assistant | mcp | automation | api

	ConversationID, JobID, TaskID, ActionID string
}

type key struct{}

func With(ctx context.Context, w Who) context.Context { return context.WithValue(ctx, key{}, w) }

func From(ctx context.Context) (Who, bool) {
	w, ok := ctx.Value(key{}).(Who)
	return w, ok
}

// fromActor reads the older actor string ("human:<email>", "auto:<name>", …).
func fromActor(ctx context.Context) Who {
	a := actor.From(ctx)
	switch {
	case strings.HasPrefix(a, "human:"):
		return Who{Kind: "human", Name: strings.TrimPrefix(a, "human:"), Via: "ui"}
	case strings.HasPrefix(a, "auto:"):
		return Who{Kind: "automation", Name: strings.TrimPrefix(a, "auto:"), Via: "automation"}
	}
	return Who{Kind: "system", Name: a}
}

// Change is one change to record.
type Change struct {
	Action     string // resource.verb, e.g. automation.update
	Resource   string // "" = the part of Action before the dot
	ResourceID string
	ProjectID  string
	Before     any
	After      any
	Detail     map[string]any
	Err        error
}

// Entry builds the audit row for c, done by the Who in ctx.
func Entry(ctx context.Context, c Change) storage.AuditEntry {
	w, ok := From(ctx)
	if !ok {
		w = fromActor(ctx)
	}
	res := c.Resource
	if res == "" {
		res, _, _ = strings.Cut(c.Action, ".")
	}
	detail := c.Detail
	if c.Err != nil {
		detail = make(map[string]any, len(c.Detail)+1)
		for k, v := range c.Detail {
			detail[k] = v
		}
		detail["error"] = c.Err.Error()
	}
	return storage.AuditEntry{
		Actor: w.Kind + ":" + w.Name, Action: c.Action, Target: c.ResourceID, Detail: detail,
		ActorKind: w.Kind, ActorID: w.ID, ActorName: w.Name, ApprovedBy: w.ApprovedBy, Via: w.Via,
		ProjectID: c.ProjectID, ConversationID: w.ConversationID, JobID: w.JobID, TaskID: w.TaskID, ActionID: w.ActionID,
		Resource: res, ResourceID: c.ResourceID, Before: Snapshot(c.Before), After: Snapshot(c.After), OK: c.Err == nil,
	}
}

// Record appends c; a failure is logged, never silently dropped.
func Record(ctx context.Context, repo storage.AuditRepo, c Change) error {
	err := repo.Append(ctx, Entry(ctx, c))
	if err != nil {
		slog.Error("audit: ghi nhật ký thất bại", "action", c.Action, "resource", c.ResourceID, "err", err)
	}
	return err
}

// Snapshot turns v into a JSON object with secrets replaced by "***"
// (an empty secret stays empty, so the log still shows "not set").
func Snapshot(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok && m == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{"value": string(b)}
	}
	redact(m)
	return m
}

func redact(m map[string]any) {
	for k, v := range m {
		if isSecret(k) {
			if s, ok := v.(string); !ok || s != "" {
				m[k] = "***"
			}
			continue
		}
		redactValue(v)
	}
}

func redactValue(v any) {
	switch x := v.(type) {
	case map[string]any:
		redact(x)
	case []any:
		for _, e := range x {
			redactValue(e)
		}
	}
}

// isSecret matches names like api_key, APIKey, password, token, x_token,
// SecretHash, authorization; not counts like input_tokens.
func isSecret(name string) bool {
	n := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(name))
	switch n {
	case "password", "token", "secret", "apikey", "authorization", "cookie":
		return true
	}
	for _, suf := range []string{"token", "secret", "hash", "apikey", "password"} {
		if strings.HasSuffix(n, suf) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/audit/ -v 2>&1 | tail -12`
Expected: 3 test PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audit
git commit -m "feat(audit): gói audit — người thực hiện trong context, che bí mật, dựng dòng nhật ký"
```

---

### Task 3: API ghi qua `audit`, có trước/sau

**Files:**
- Modify: `internal/api/org.go` (`auditAction` → gọi `s.audit`; provider/agent/org_model/project handlers)
- Create: `internal/api/audit.go` (`s.audit`, `s.agentProject`)
- Modify: `internal/api/automations.go`, `monitors.go`, `ops.go`, `policy.go`, `usage.go`, `agents.go`
- Test: `internal/api/audit_test.go`

**Interfaces:**
- Consumes: `audit.Change`, `audit.Record`, `audit.With/From`, `audit.Who` (Task 2).
- Produces: `func (s *server) audit(r *http.Request, c audit.Change)`; `auditAction(r, action, target, detail)` giữ nguyên chữ ký (gọi `s.audit`).

- [ ] **Step 1: Failing API test** (`internal/api/audit_test.go`):

```go
package api_test

import (
	"context"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func TestAuditRecordsBeforeAfter(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "Hook", "source": "webhook", "action": "chat", "prompt": "x"}, nil)
	aid := body["automation"].(map[string]any)["id"].(string)
	resp, _ := do(t, admin, "PATCH", e.srv.URL+"/api/automations/"+aid, map[string]any{
		"name": "Hook 2", "source": "webhook", "action": "chat", "prompt": "x", "enabled": true}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("update = %d", resp.StatusCode)
	}
	rows, err := e.st.Audit().List(context.Background(), storage.AuditFilter{Resource: "automation", ResourceID: aid})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %d %v", len(rows), err)
	}
	up := rows[0]
	if up.Action != "automation.update" || up.ActorKind != "human" || up.ActorName != "admin@x.io" || up.ActorID == "" || up.Via != "ui" || up.ProjectID != pid {
		t.Fatalf("update row: %+v", up)
	}
	if up.Before["name"] != "Hook" || up.After["name"] != "Hook 2" {
		t.Fatalf("before/after: %v → %v", up.Before, up.After)
	}
	if create := rows[1]; create.Before != nil || create.After["name"] != "Hook" || create.ProjectID != pid {
		t.Fatalf("create row: %+v", create)
	}
}

func TestAuditRedactsProviderKey(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	resp, body := do(t, admin, "POST", e.srv.URL+"/api/providers", map[string]any{"name": "p", "kind": "anthropic", "api_key": "sk-secret-1"}, nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create provider = %d %v", resp.StatusCode, body)
	}
	id := body["provider"].(map[string]any)["id"].(string)
	do(t, admin, "PATCH", e.srv.URL+"/api/providers/"+id, map[string]any{"name": "p2", "api_key": "sk-secret-2"}, nil)
	rows, _ := e.st.Audit().List(context.Background(), storage.AuditFilter{Resource: "provider", ResourceID: id})
	for _, r := range rows {
		for _, snap := range []map[string]any{r.Before, r.After, r.Detail} {
			for k, v := range snap {
				if s, ok := v.(string); ok && (s == "sk-secret-1" || s == "sk-secret-2") {
					t.Fatalf("%s leaked key in %q", r.Action, k)
				}
			}
		}
	}
	if len(rows) != 2 || rows[0].Before["name"] != "p" || rows[0].After["name"] != "p2" {
		t.Fatalf("provider rows: %+v", rows)
	}
}
```

(Nếu body tạo provider cần trường khác, đọc `providerInput` trong `org.go` và điều chỉnh body test cho hợp lệ — ghi ruling.)

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/api/ -run TestAudit 2>&1 | tail -15`
Expected: FAIL — `ActorKind` rỗng / `Before` nil.

- [ ] **Step 3: Implement** `internal/api/audit.go`:

```go
package api

import (
	"context"
	"net/http"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// audit records a change made through the API: by the signed-in person
// unless the request context already names someone (an approved proposal).
func (s *server) audit(r *http.Request, c audit.Change) {
	ctx := r.Context()
	w, ok := audit.From(ctx)
	if !ok {
		u := userFrom(r)
		w = audit.Who{Kind: "human", ID: u.ID, Name: u.Email, Via: "ui"}
	}
	res := c.Resource
	if res == "" {
		res, _, _ = strings.Cut(c.Action, ".")
	}
	if c.ProjectID == "" {
		if p, ok := c.Detail["project"].(string); ok {
			c.ProjectID = p
		} else if res == "project" {
			c.ProjectID = c.ResourceID
		}
	}
	switch res {
	case "job":
		w.JobID = c.ResourceID
	case "task":
		w.TaskID = c.ResourceID
	}
	_ = audit.Record(audit.With(ctx, w), s.cfg.Store.Audit(), c)
}

// agentProject is the project an agent belongs to ("" = a template's agent).
func (s *server) agentProject(ctx context.Context, a storage.Agent) string {
	m, err := s.cfg.Store.OrgModels().Get(ctx, a.OrgModelID)
	if err != nil {
		return ""
	}
	return m.RepoID
}
```

`org.go`:
```go
func (s *server) auditAction(r *http.Request, action, target string, detail map[string]any) {
	s.audit(r, audit.Change{Action: action, ResourceID: target, Detail: detail})
}
```

Handler nào sửa cấu hình thì đọc bản cũ trước khi ghi và gọi `s.audit` với `Before`/`After` (dùng DTO trả về cho dashboard nếu có, không thì struct storage — `Snapshot` che bí mật):

| Handler | Before | After | ProjectID |
|---|---|---|---|
| `createAutomation` / `updateAutomation` / `deleteAutomation` (automations.go) | `s.toAutomationDTO(r, old)` (update: copy `a` trước `applyAutomation`; delete: `Get` trước `Delete`) | `s.toAutomationDTO(r, a)` | `a.ProjectID` |
| `createMonitor` / `updateMonitor` / `deleteMonitor` (monitors.go) | bản cũ từ `Monitors().Get` | bản mới | `m.ProjectID` |
| `createProcess` / `updateProcess` / `deleteProcess` (ops.go) | bản cũ từ `Processes().Get` | bản mới | `p.ProjectID` |
| agent create/update/delete (org.go), `agent.restore` (agents.go) | `Agents().Get` trước | bản mới | `s.agentProject(ctx, a)` |
| `project.update` / `project.delete` (org.go) | `Repos().Get` trước | bản mới | id project |
| `project.policy` (policy.go) | policy cũ (đọc trước khi lưu) | `in` | `projectID` |
| provider create/update/delete (org.go) | `toProviderDTO(old)` | `toProviderDTO(p)` | "" |
| `org_model.update` (org.go) | model cũ | model mới | `m.RepoID` |
| `usage.settings` (usage.go) | settings cũ | `in` | "" |

Mẫu cho update (áp cho từng dòng bảng, giữ `Detail` cũ nếu có):
```go
old := a // before applyAutomation mutates a
...
s.audit(r, audit.Change{Action: "automation.update", ResourceID: a.ID, ProjectID: a.ProjectID,
	Before: s.toAutomationDTO(r, old), After: s.toAutomationDTO(r, a), Detail: map[string]any{"name": a.Name, "enabled": a.Enabled}})
```
Handler thất bại sau khi đã validate (lỗi ghi store) thì gọi `s.audit(..., Err: err)` trước khi trả lỗi — áp cho bảng trên.

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/api/ 2>&1 | tail -5`
Expected: `ok` (cả test cũ).

- [ ] **Step 5: Commit**

```bash
git add internal/api
git commit -m "feat(audit): API ghi người làm, project và trước/sau cho các thay đổi cấu hình"
```

---

### Task 4: Thay đổi do agent đề xuất (thẻ duyệt, patch)

**Files:**
- Modify: `internal/actions/actions.go` (Scope.JobID; `Propose` lưu `JobID`), `internal/actions/automation.go` (ghi nhật ký với trước/sau)
- Modify: `internal/chat/engine.go` (Scope có `JobID`; patch tự duyệt ghi nhật ký)
- Modify: `internal/api/actions.go` (`decideAction` dựng Who agent + người duyệt), `internal/api/chat.go` (`decidePatch`)
- Test: `internal/actions/automation_test.go`, `internal/api/audit_test.go`

**Interfaces:**
- Consumes: `audit.*` (Task 2), `Action.JobID` (Task 1), `s.audit` (Task 3).
- Produces: `actions.Scope.JobID string`; `func ProposerWho(a storage.Action, approver storage.User) audit.Who` trong `internal/api/actions.go`.

- [ ] **Step 1: Failing tests**

`internal/actions/automation_test.go` — thêm (dùng helper có sẵn trong file để tạo service/project/đề xuất `create_automation`; đọc file để lấy đúng tên helper):

```go
func TestApprovedAutomationIsAudited(t *testing.T) {
	// set up like the existing create_automation test, with Scope{…, JobID: "job_1", ConversationID: conv}
	// approve with ctx := audit.With(ctx, audit.Who{Kind: "agent", Name: "Lead", ApprovedBy: "a@x.io", Via: "chat", ConversationID: conv, JobID: "job_1", ActionID: act.ID})
	// then:
	rows, _ := st.Audit().List(ctx, storage.AuditFilter{Resource: "automation"})
	if len(rows) != 1 || rows[0].Action != "automation.create" || rows[0].ActorName != "Lead" || rows[0].ApprovedBy != "a@x.io" ||
		rows[0].JobID != "job_1" || rows[0].After["Name"] == nil || rows[0].Before != nil {
		t.Fatalf("audit: %+v", rows)
	}
}

func TestProposalKeepsJob(t *testing.T) {
	// Propose with Scope{JobID: "job_1", …}; Actions().Get(id).JobID == "job_1"
}
```

`internal/api/audit_test.go` — thêm test thẻ bị duyệt nhưng chạy lỗi (Review Focus 4): tạo action `update_automation` trỏ tới tự động hóa rồi xóa tự động hóa đó trước khi duyệt (chèn thẳng bằng `e.st.Actions().Create` với `ProposedBy: "Lead"`, `ConversationID` rỗng, `TaskID` rỗng, `JobID: "job_9"`, `Args.Automation` là JSON spec hợp lệ trỏ `automation_id` đã xóa), `POST /api/actions/{id}/approve`, rồi:

```go
rows, _ := e.st.Audit().List(ctx, storage.AuditFilter{JobID: "job_9"})
// có dòng action.approve với ActorKind "agent", ActorName "Lead", ApprovedBy "admin@x.io", ActionID = id, OK=false (status failed)
```

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/actions/ ./internal/api/ -run 'Audit|Proposal' 2>&1 | tail -15`
Expected: FAIL (Scope.JobID chưa có / không có dòng nhật ký / ActorKind human).

- [ ] **Step 3: Implement**

- `actions.Scope` thêm `JobID string`; trong `Propose` thêm `JobID: sc.JobID` vào `storage.Action`.
- `saveAutomation`:
```go
	if a.Kind == "update_automation" {
		x, err := s.store.Automations().Get(ctx, spec.AutomationID)
		if err != nil {
			return err
		}
		old := x
		spec.Apply(&x, now)
		err = s.store.Automations().Update(ctx, x)
		_ = audit.Record(ctx, s.store.Audit(), audit.Change{Action: "automation.update", ResourceID: x.ID, ProjectID: x.ProjectID, Before: old, After: x, Err: err})
		return err
	}
	x := storage.Automation{…}
	spec.Apply(&x, now)
	created, err := s.store.Automations().Create(ctx, x)
	_ = audit.Record(ctx, s.store.Audit(), audit.Change{Action: "automation.create", ResourceID: created.ID, ProjectID: x.ProjectID, After: created, Err: err})
	return err
```
- `engine.go` dòng tạo Scope của chat: thêm `JobID: turn.JobID`; của task (dòng ~850): `JobID: usage.JobFrom(ctx)`.
- `engine.go` hai chỗ tự duyệt patch (`DecidePatch(actor.With(... "auto:"+agent.Name ...))`): sau khi duyệt thành công ghi
```go
_ = audit.Record(audit.With(context.Background(), audit.Who{Kind: "agent", Name: agent.Name, Via: "chat", ConversationID: conv.ID, JobID: turn.JobID}),
	e.store.Audit(), audit.Change{Action: "patch." + d.Status, ResourceID: d.ID, ProjectID: project.ID, Detail: map[string]any{"files": d.Files, "auto": true}})
```
(chỗ thứ hai dùng biến tương ứng của nhánh đó; nhánh task dùng `Via: "task"`, `TaskID`).
- `internal/api/actions.go`:
```go
// proposerWho: an approved proposal is the agent's change, approved by the person.
func proposerWho(a storage.Action, u storage.User) audit.Who {
	via := "chat"
	if a.ConversationID == "" && a.TaskID != "" {
		via = "task"
	}
	return audit.Who{Kind: "agent", Name: a.ProposedBy, ApprovedBy: u.Email, Via: via,
		ConversationID: a.ConversationID, JobID: a.JobID, TaskID: a.TaskID, ActionID: a.ID}
}

func (s *server) decideAction(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		ctx := r.Context()
		if cur, err := s.cfg.Store.Actions().Get(ctx, r.PathValue("id")); err == nil {
			who := proposerWho(cur, u)
			if !approve { // the person's decision
				who = audit.Who{Kind: "human", ID: u.ID, Name: u.Email, Via: "ui", ConversationID: cur.ConversationID, JobID: cur.JobID, TaskID: cur.TaskID, ActionID: cur.ID}
			}
			r = r.WithContext(audit.With(ctx, who))
		}
		a, err := s.cfg.Actions.Decide(r.Context(), r.PathValue("id"), approve, u.Email)
		… (giữ nguyên xử lý lỗi)
		verb := "action.reject"
		if approve {
			verb = "action.approve"
		}
		var runErr error
		if a.Status == "failed" {
			runErr = errors.New(a.Detail)
		}
		s.audit(r, audit.Change{Action: verb, Resource: "action", ResourceID: a.ID, ProjectID: a.ProjectID,
			Detail: map[string]any{"kind": a.Kind, "target": a.Target, "status": a.Status}, Err: runErr})
		writeJSON(…)
	}
}
```
- `decidePatch` (chat.go): lấy conversation của patch (nếu có) để biết `AgentName`; duyệt thì Who agent + `ApprovedBy`, từ chối thì human; `ProjectID` từ conversation/task; `Err` khi `p.Status == "failed"`.

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/... 2>&1 | grep -v '^ok' | tail -15`
Expected: không có dòng FAIL.

- [ ] **Step 5: Commit**

```bash
git add internal/actions internal/chat internal/api
git commit -m "feat(audit): thay đổi agent đề xuất ghi agent là người làm, kèm người duyệt, chat và job"
```

---

### Task 5: API xem và thống kê

**Files:**
- Modify: `internal/api/handlers.go` (`listAudit`), `internal/api/server.go` (route `GET /api/audit/stats`)
- Test: `internal/api/audit_test.go`

**Interfaces:**
- Consumes: `AuditRepo.List/Count`, `AuditFilter` (Task 1).
- Produces (JSON, dashboard dùng ở Task 6):
  - `GET /api/audit?project=&resource=&resource_id=&actor_kind=&actor=&via=&conversation_id=&job_id=&task_id=&since=168h&before=&limit=` → `{entries: AuditDTO[], next_before: string}`
  - `AuditDTO = {id, action, actor_kind, actor_id, actor_name, approved_by, via, project_id, project_name, conversation_id, job_id, task_id, action_id, resource, resource_id, target, detail, before, after, ok, at}`
  - `GET /api/audit/stats?by=day|kind|actor|resource|via|project&…cùng bộ lọc` → `{by, rows: [{key, count, failed}]}`

- [ ] **Step 1: Failing test**

```go
func TestAuditListAndStats(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	for _, n := range []string{"a", "b", "c"} {
		do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{"name": n, "source": "webhook", "action": "chat", "prompt": "x"}, nil)
	}
	resp, body := do(t, admin, "GET", e.srv.URL+"/api/audit?project="+pid+"&resource=automation&limit=2", nil, nil)
	entries := body["entries"].([]any)
	if resp.StatusCode != 200 || len(entries) != 2 || body["next_before"] == "" {
		t.Fatalf("page 1 = %d %v", resp.StatusCode, body)
	}
	first := entries[0].(map[string]any)
	if first["actor_kind"] != "human" || first["project_name"] != "shop" || first["after"] == nil {
		t.Fatalf("entry: %v", first)
	}
	_, body = do(t, admin, "GET", e.srv.URL+"/api/audit?project="+pid+"&resource=automation&limit=2&before="+body["next_before"].(string), nil, nil)
	if n := len(body["entries"].([]any)); n != 1 || body["next_before"] != "" {
		t.Fatalf("page 2 = %d next=%v", n, body["next_before"])
	}
	_, body = do(t, admin, "GET", e.srv.URL+"/api/audit/stats?by=kind&project="+pid, nil, nil)
	rows := body["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["key"] != "human" || rows[0].(map[string]any)["count"].(float64) < 3 {
		t.Fatalf("stats = %v", body)
	}
	if resp, _ := do(t, admin, "GET", e.srv.URL+"/api/audit/stats?by=nope", nil, nil); resp.StatusCode != 400 {
		t.Fatalf("bad by = %d", resp.StatusCode)
	}

	member := e.client(t)
	login(t, e, member, "member@x.io", "member-password")
	for _, p := range []string{"/api/audit", "/api/audit/stats?by=day"} {
		if resp, _ := do(t, member, "GET", e.srv.URL+p, nil, nil); resp.StatusCode != 403 {
			t.Fatalf("member %s = %d", p, resp.StatusCode)
		}
	}
}
```

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/api/ -run TestAuditListAndStats 2>&1 | tail -10`
Expected: FAIL (thiếu `next_before`/`project_name`, route stats 404/405).

- [ ] **Step 3: Implement**

```go
func auditFilter(r *http.Request) storage.AuditFilter {
	q := r.URL.Query()
	f := storage.AuditFilter{ProjectID: q.Get("project"), Resource: q.Get("resource"), ResourceID: q.Get("resource_id"),
		ActorKind: q.Get("actor_kind"), ActorName: q.Get("actor"), Via: q.Get("via"),
		ConversationID: q.Get("conversation_id"), JobID: q.Get("job_id"), TaskID: q.Get("task_id"), BeforeID: q.Get("before")}
	if d, err := time.ParseDuration(q.Get("since")); err == nil && d > 0 {
		f.From = time.Now().Add(-d)
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	return f
}

type auditDTO struct {
	ID             string         `json:"id"`
	Action         string         `json:"action"`
	ActorKind      string         `json:"actor_kind"`
	ActorID        string         `json:"actor_id"`
	ActorName      string         `json:"actor_name"`
	ApprovedBy     string         `json:"approved_by"`
	Via            string         `json:"via"`
	ProjectID      string         `json:"project_id"`
	ProjectName    string         `json:"project_name"`
	ConversationID string         `json:"conversation_id"`
	JobID          string         `json:"job_id"`
	TaskID         string         `json:"task_id"`
	ActionID       string         `json:"action_id"`
	Resource       string         `json:"resource"`
	ResourceID     string         `json:"resource_id"`
	Target         string         `json:"target"`
	Detail         map[string]any `json:"detail"`
	Before         map[string]any `json:"before"`
	After          map[string]any `json:"after"`
	OK             bool           `json:"ok"`
	At             time.Time      `json:"at"`
}

func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	f := auditFilter(r)
	entries, err := s.cfg.Store.Audit().List(r.Context(), f)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	names := map[string]string{}
	out := make([]auditDTO, 0, len(entries))
	for _, e := range entries {
		if _, ok := names[e.ProjectID]; !ok && e.ProjectID != "" {
			if p, err := s.cfg.Store.Repos().Get(r.Context(), e.ProjectID); err == nil {
				names[e.ProjectID] = p.Name
			} else {
				names[e.ProjectID] = ""
			}
		}
		out = append(out, auditDTO{e.ID, e.Action, e.ActorKind, e.ActorID, e.ActorName, e.ApprovedBy, e.Via, e.ProjectID, names[e.ProjectID],
			e.ConversationID, e.JobID, e.TaskID, e.ActionID, e.Resource, e.ResourceID, e.Target, e.Detail, e.Before, e.After, e.OK, e.At})
	}
	next := ""
	if len(entries) == f.Limit {
		next = entries[len(entries)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "next_before": next})
}

func (s *server) auditStats(w http.ResponseWriter, r *http.Request) {
	by := r.URL.Query().Get("by")
	rows, err := s.cfg.Store.Audit().Count(r.Context(), auditFilter(r), by)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	type row struct {
		Key    string `json:"key"`
		Count  int    `json:"count"`
		Failed int    `json:"failed"`
	}
	out := make([]row, 0, len(rows))
	for _, c := range rows {
		out = append(out, row{c.Key, c.Count, c.Failed})
	}
	writeJSON(w, http.StatusOK, map[string]any{"by": by, "rows": out})
}
```

Route (server.go, cạnh `GET /api/audit`):
```go
mux.Handle("GET /api/audit/stats", s.requireRole(storage.RoleAdmin, http.HandlerFunc(s.auditStats)))
```

Ruling đã chốt: khi đúng `Limit` dòng thì trả `next_before` kể cả trang sau rỗng — trang sau rỗng trả `next_before: ""`; test page 2 có 1 dòng < limit nên `""`.

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/api/ 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/api
git commit -m "feat(audit): API lọc, phân trang và thống kê nhật ký thay đổi"
```

---

### Task 6: Dashboard — Nhật ký

**Files:**
- Create: `dashboard/app/composables/useAudit.ts` (types, `auditDiff`, `whoIcon`)
- Create: `dashboard/app/components/AuditLog.vue`
- Create: `dashboard/scripts/test-audit-diff.ts`; Modify `dashboard/package.json` (`test:unit` chạy thêm file này)
- Modify: `dashboard/app/pages/admin/audit.vue` (dùng `AuditLog` + dải thống kê)
- Modify: `dashboard/app/pages/projects/[id]/index.vue` (tab `log`), `dashboard/app/layouts/default.vue` (mục Nhật ký trong project, chỉ admin)
- Modify: `dashboard/app/pages/projects/[id]/automations/[aid]/index.vue` (mục "Lịch sử thay đổi")
- Modify: `dashboard/app/locales/parts/audit.vi.ts` + `audit.en.ts` (mới), đăng ký trong `app/locales/vi.ts`/`en.ts` theo cách các part khác đăng ký; `nav.log`

**Interfaces:**
- Consumes: API Task 5.
- Produces: `<AuditLog :filter="{ project?, resource?, resource_id? }" :show-filters :projects compact />`; `auditDiff(before, after): { key: string, before: unknown, after: unknown }[]`.

- [ ] **Step 1: Failing unit test** (`scripts/test-audit-diff.ts`, cùng kiểu `test-merge-draft.ts` — đọc file đó để theo đúng cách import/assert):

```ts
import assert from 'node:assert/strict'
import { auditDiff } from '../app/composables/useAudit.ts'

assert.deepEqual(auditDiff({ name: 'a', n: 1, same: true }, { name: 'b', n: 1, same: true, added: 'x' }),
  [{ key: 'added', before: undefined, after: 'x' }, { key: 'name', before: 'a', after: 'b' }])
assert.deepEqual(auditDiff(null, { a: 1 }), [{ key: 'a', before: undefined, after: 1 }])
assert.deepEqual(auditDiff({ a: 1 }, null), [{ key: 'a', before: 1, after: undefined }])
assert.deepEqual(auditDiff({ cfg: { cron: '1' } }, { cfg: { cron: '2' } }), [{ key: 'cfg.cron', before: '1', after: '2' }])
assert.deepEqual(auditDiff({ list: [1, 2] }, { list: [1, 2] }), [])
assert.deepEqual(auditDiff({ list: [1] }, { list: [1, 2] }), [{ key: 'list', before: [1], after: [1, 2] }])
console.log('audit diff ok')
```

- [ ] **Step 2: Run — FAIL**

Run: `cd dashboard && node --experimental-strip-types --no-warnings scripts/test-audit-diff.ts`
Expected: lỗi không tìm thấy module/`auditDiff`.

- [ ] **Step 3: Implement**

`useAudit.ts`:
```ts
// Change log (ADR-043): who changed what, where from, before/after.
export interface AuditEntry {
  id: string, action: string, actor_kind: 'human' | 'agent' | 'automation' | 'system' | '', actor_id: string, actor_name: string
  approved_by: string, via: string, project_id: string, project_name: string, conversation_id: string, job_id: string
  task_id: string, action_id: string, resource: string, resource_id: string, target: string
  detail: Record<string, unknown>, before: Record<string, unknown> | null, after: Record<string, unknown> | null, ok: boolean, at: string
}
export interface AuditDiff { key: string, before: unknown, after: unknown }

const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)

// auditDiff lists the fields that differ, nested objects as dotted keys,
// arrays compared whole; sorted by key.
export function auditDiff(before: Record<string, unknown> | null, after: Record<string, unknown> | null, prefix = ''): AuditDiff[] {
  const out: AuditDiff[] = []
  const keys = new Set([...Object.keys(before ?? {}), ...Object.keys(after ?? {})])
  for (const k of keys) {
    const b = before?.[k]
    const a = after?.[k]
    const key = prefix + k
    if (isObj(b) && isObj(a)) out.push(...auditDiff(b, a, key + '.'))
    else if (JSON.stringify(b) !== JSON.stringify(a)) out.push({ key, before: b, after: a })
  }
  return out.sort((x, y) => x.key.localeCompare(y.key))
}

export const whoIcon = (k: string) => ({ human: 'i-lucide-user', agent: 'i-lucide-bot', automation: 'i-lucide-alarm-clock' } as Record<string, string>)[k] ?? 'i-lucide-cog'
```

`AuditLog.vue` — theo khuôn `JobsTable.vue` (bộ lọc một hàng, bảng trong `UCard`, nút "Xem thêm" theo `next_before`, sentinel `__all` cho `USelect`):
- Bộ lọc (khi `showFilters`): loại người thực hiện (`actor_kind`), kênh (`via`), loại cài đặt (`resource`: automation, agent, monitor, process, project, provider, action, patch, task, job), project (nếu truyền `projects`), khoảng thời gian (24h/7d/30d).
- Cột: Thời gian · Ai (icon `whoIcon`, tên, dòng phụ `t('audit.approvedBy', { name })` khi có `approved_by`) · Thay đổi (`t('audit.action.' + action)` nếu có key, không thì hiện `action` thô; badge đỏ khi `!ok`) · Đối tượng (tên từ `after?.name ?? before?.name ?? resource_id`; dòng phụ `project_name` khi không lọc project) · Nguồn (nút icon: chat → `chat-prefill` với `conversationId` rồi tới tab chat của project; job → mở `JobDetailModal` với `job_id`; việc → tab tasks `task=task_id`; kênh `t('audit.via.' + via)`).
- Bấm một dòng mở phần diff bên dưới: bảng 3 cột Trường · Trước · Sau từ `auditDiff(before, after)`; `"***"` hiện là `t('audit.secretChanged')`; không có trước/sau thì hiện `detail` dạng JSON gọn.
- `compact`: ẩn cột Nguồn và project, dùng cho trang chi tiết.

`admin/audit.vue`: `PageShell` tiêu đề `t('admin.auditTitle')`; dải 4 ô số liệu 7 ngày từ `GET /api/audit/stats?by=kind&since=168h` (Tổng · Người · Agent · Thất bại — tổng/thất bại cộng từ các dòng); dưới là `<AuditLog show-filters :projects="projects" />` (danh sách project từ `/api/projects`).

Project: thêm `'log'` vào `Tab`/`tabs`; `<AuditLog v-else-if="tab === 'log'" :filter="{ project: project.id }" show-filters />`; sidebar thêm `{ label: t('nav.log'), icon: 'i-lucide-scroll-text', to: to('log'), exactQuery: 'partial' }` trong nhánh `isAdmin`.

Chi tiết tự động hóa: cuối trang, nếu admin, `UCard` tiêu đề `t('audit.history')` chứa `<AuditLog :filter="{ resource: 'automation', resource_id: aid }" compact />`.

Ruling: trang chi tiết agent đã có lịch sử phiên bản (org_revisions, có khôi phục), không thêm `AuditLog` ở đó ở phần này — cost if wrong: thêm một thẻ ở trang agent sau.

i18n keys (`audit.vi.ts`): `audit.history`, `audit.approvedBy`, `audit.secretChanged`, `audit.noDiff`, `audit.colTime/colWho/colChange/colTarget/colSource`, `audit.kindAll`, `audit.kind.human/agent/automation/system`, `audit.viaAll`, `audit.via.ui/chat/task/assistant/mcp/automation/api`, `audit.resourceAll`, `audit.resource.<các loại ở trên>`, `audit.action.<resource.verb phổ biến: automation.create/update/delete, agent.create/update/delete/restore, monitor.*, process.*, project.update/delete/policy, provider.*, action.approve/reject, patch.applied/rejected/failed, task.start/cancel/retry, job.cancel/retry>`, `audit.statTotal/statHuman/statAgent/statFailed`, `audit.empty`, `audit.more`, `audit.openChat/openJob/openTask`; `nav.log`. Tiếng Anh tương ứng trong `audit.en.ts`.

`package.json`: `"test:unit": "node --experimental-strip-types --no-warnings scripts/test-merge-draft.ts && node --experimental-strip-types --no-warnings scripts/test-audit-diff.ts"`.

- [ ] **Step 4: Run — PASS**

Run: `cd dashboard && pnpm test:unit && node scripts/check-i18n.mjs && pnpm build 2>&1 | tail -5`
Expected: `audit diff ok`, i18n không lỗi, build xong.

- [ ] **Step 5: Kiểm tra thật** — `make ui-build` (hoặc cách build office đang dùng), khởi động lại office, mở `/admin/audit`: dòng cũ vẫn hiện (người = email, không lỗi); sửa một tự động hóa → dòng mới có diff Trước/Sau; tab Nhật ký trong project chỉ hiện dòng của project đó.

- [ ] **Step 6: Commit**

```bash
git add dashboard
git commit -m "feat(dashboard): trang Nhật ký thay đổi, tab Nhật ký trong project, lịch sử tự động hóa"
```

---

### Task 7: Tài liệu

**Files:**
- Modify: `docs/DECISIONS.md` (ADR-043), `docs/PLAN.md` (dòng tiến độ)

- [ ] **Step 1:** Thêm ADR-043 "Nhật ký thay đổi" tóm tắt: các cột mới, `internal/audit` là cổng ghi duy nhất, quy tắc che bí mật, agent đề xuất = `actor` agent + `approved_by`, `actions.job_id`, API `/api/audit` + `/api/audit/stats`, giao diện; ghi rõ tính nguyên tử (cùng transaction) để phần 2 (registry Apply).
- [ ] **Step 2:** `docs/PLAN.md`: thêm dòng "ADR-043 nhật ký thay đổi — xong", phần 2–4 của spec là việc tiếp theo.
- [ ] **Step 3: Run** `make test 2>&1 | tail -15` — Expected: tất cả qua.
- [ ] **Step 4: Commit**

```bash
git add docs
git commit -m "docs: ADR-043 nhật ký thay đổi"
```
