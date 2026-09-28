# Chat tag agent vào nhóm — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `@Agent` trong Chat kéo agent đó vào cuộc chat; mỗi agent giữ phiên riêng và chỉ nhận tin mới; agent tag agent khác có giới hạn; lead mặc định có quyền Vận hành.

**Architecture:** Bảng `conversation_agents` giữ phiên/tin đã thấy/context của từng agent trong một cuộc chat. `SendWithContext` tính hàng người trả lời (tag, hoặc agent mặc định); mỗi người trả lời là một `Turn` + job; `done` mang `next_turn_id`. Engine chạy từng lượt với phiên của agent đó, gửi kèm các tin mới kể từ `last_message_id`.

**Tech Stack:** Go, SQLite (goose), Nuxt 4 / Nuxt UI 4.

**Spec:** `docs/superpowers/specs/2026-09-28-group-chat-mentions-design.md`

## Global Constraints
- Tin không tag → agent mặc định (`conversations.agent_id`, ô chọn agent).
- Agent tag agent khác: tối đa 2 lượt chuyển mỗi tin người dùng; tổng tối đa 4 người trả lời.
- Chat của Việc và của tự động hóa (`single`) không tag.
- i18n bắt buộc cho chữ hiển thị; `make test` phải xanh.
- Lead mặc định: `team-lead`, `assistant` (solo), `executor` (council) → level `operate` (template + agent hiện có chưa tự chỉnh quyền).

## Review Focus
1. Hai lượt nối tiếp: `finish` của lượt trước không được xóa `active` của lượt sau (người dùng gửi tin xen giữa phải nhận `ErrBusy`).
2. Agent vào nhóm lần đầu không có phiên: nhận transcript (có tên người nói), không nhận “tin mới” trùng lặp.
3. Tag trong code block/inline code không kéo ai vào.
4. Dừng giữa hàng đợi: không lượt nào chạy tiếp.
5. Cuộc chat cũ sau migration vẫn `--resume` đúng phiên cũ.

---

### Task 1: Migration + repo thành viên + `Mentions` + lead mặc định
**Files:** Create `migrations/sqlite/00028_conversation_agents.sql`, `internal/chat/mentions.go`, `internal/chat/mentions_test.go`; Modify `internal/storage/org.go` (ChatMember, ChatRepo methods), `internal/storage/sqlite/chat.go`, `templates/models/{solo,team,council}.json`; Test `internal/storage/storagetest` or sqlite test.

**Produces:**
```go
type ChatMember struct {
	ConversationID, AgentID, AgentName, SessionID, Runtime, LastMessageID string
	ContextTokens, ContextWindow int
	JoinedAt time.Time
}
// ChatRepo:
UpsertMember(ctx, m ChatMember) error          // insert or update every field but joined_at
Members(ctx, conversationID string) ([]ChatMember, error) // by joined_at
func Mentions(text string, agents []storage.Agent) []storage.Agent
```
- [ ] Test `Mentions`: `"@Dev sửa"`→Dev; `"@trưởng nhóm và @DEV"`→[Trưởng nhóm, Dev]; `"@Dev Lead"` với agents Dev, Dev Lead → Dev Lead; ``"`@Dev` và ```\n@Dev\n```"`` → none; trùng → một; key `@team-lead` → lead.
- [ ] Test sqlite: UpsertMember hai lần giữ joined_at, cập nhật session; Members theo thứ tự vào; migration backfill (tạo conversation có session trước khi migrate không làm được → kiểm SQL backfill bằng cách chạy lại câu INSERT…SELECT trong test) — ruling: kiểm backfill trên DB thật của office (sqlite3) ở task 5.
- [ ] Migration:
```sql
-- +goose Up
CREATE TABLE conversation_agents (
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL,
    agent_name      TEXT NOT NULL DEFAULT '',
    session_id      TEXT NOT NULL DEFAULT '',
    runtime         TEXT NOT NULL DEFAULT '',
    last_message_id TEXT NOT NULL DEFAULT '',
    context_tokens  INTEGER NOT NULL DEFAULT 0,
    context_window  INTEGER NOT NULL DEFAULT 0,
    joined_at       TEXT NOT NULL,
    PRIMARY KEY (conversation_id, agent_id)
);
INSERT INTO conversation_agents (conversation_id, agent_id, agent_name, session_id, runtime, last_message_id, context_tokens, context_window, joined_at)
SELECT c.id, c.agent_id, c.agent_name, c.session_id, c.runtime,
       COALESCE((SELECT m.id FROM messages m WHERE m.conversation_id = c.id ORDER BY m.created_at DESC, m.id DESC LIMIT 1), ''),
       c.context_tokens, c.context_window, c.created_at
FROM conversations c WHERE c.agent_id IS NOT NULL;
UPDATE agents SET permissions = json_set(permissions, '$.level', 'operate', '$.read_only', json('false'))
WHERE tier = 'lead' AND key IN ('team-lead', 'assistant', 'executor')
  AND json_extract(permissions, '$.level') IS NULL AND json_type(permissions, '$.caps') IS NULL;
-- +goose Down
DROP TABLE conversation_agents;
```
- [ ] Templates: team-lead, assistant, executor → `"permissions": { "level": "operate", "read_only": false }`.
- [ ] Commit.

### Task 2: Engine — phiên của từng agent, tin mới, `SetAgent` giữ phiên
**Files:** `internal/chat/engine.go`, `internal/chat/members.go` (helpers), tests in `internal/chat/engine_test.go`.
- `e.member(ctx, conv, agent) ChatMember` — get or new (joined now).
- In `run`: session comes from the member (`conv.SessionID/Runtime` replaced by member's); after the run: member.SessionID/Runtime/Context/LastMessageID (= its reply id) saved via UpsertMember; keep writing conv.ContextTokens/Window when agent is the default one (context meter).
- Prompt: when the member has a session, prefix the new messages after `LastMessageID` other than the one being answered: `"Tin mới trong cuộc chat từ lượt trước của bạn:\n[Tên]\n…\n---\n"`. Without a session: `req.History = HistoryFor(history, agent.Name)` (transcript, as today).
- `SetAgent`: only `AgentID/AgentName` change; sessions kept.
- [ ] Tests (fake claude logging args + stdin): (a) lượt 2 của cùng agent có `--resume sess-A`; (b) sau khi đổi sang B rồi lại A, lượt của A có `--resume sess-A` và stdin chứa câu của B dưới nhãn `[B]`; (c) B lần đầu: không có `--resume`, stdin là transcript.
- [ ] Commit.

### Task 3: Hàng người trả lời, tag, chuyển tiếp có giới hạn
**Files:** `internal/chat/engine.go`, `internal/chat/members.go`, tests.
- `Turn` gains `queue []storage.Agent`, `hops int`, `answered int`, `NextTurnID string` on `done` Event (`Event.NextTurnID string json:"next_turn_id,omitempty"`).
- `SendWithContext`: if `conv.TaskID == "" && conv.Purpose == ""`: responders = `Mentions(text, agents)` or [default]; first runs now, the rest in `turn.queue`.
- After a successful reply by A: `Mentions(reply, agents)` minus A, if `hops < 2 && answered+len(queue) < 4` → append, `hops++`; if a handoff is cut, add message role `error`… ruling: a `system`-like note is stored as role `error` is wrong → store as assistant message with Author "office"? Use role `error`? Decision: role `error` with content "Đã dừng chuyển tiếp…" (no new role; UI shows it muted). 
- Next responder: new Turn registered as `active[conv]` BEFORE the previous turn finishes; `finish` deletes `active[conv]` only if it is still that turn. `done` of the previous carries `next_turn_id`. Its prompt: `"<Tên> vừa tag bạn trong cuộc chat nhóm. Trả lời phần dành cho bạn."` + new messages since its last seen (includes the user's message and earlier replies).
- Cancel: cancelling a turn clears its queue (next not started).
- Error in a reply: continue with the queue.
- System prompt (chat, not task): members + levels + rule "Chỉ tag @agent khác khi thật sự cần (việc cần quyền hay chuyên môn bạn không có), ghi rõ cần họ làm gì; việc tự làm được thì tự làm."
- [ ] Tests: `@A @B` → two replies in order, B's stdin has A's reply; A's reply "@B giúp" → B answers; chain > 2 hops stops with note; cancel stops queue; `ErrBusy` while the second is running.
- [ ] Commit.

### Task 4: API + SSE
**Files:** `internal/api/chat.go`, tests `internal/api/chat_test.go`.
- `GET /api/conversations/:id` adds `members: [{agent_id, agent_name, level, context_tokens, context_window}]`; `conversationDTO` context = default member's.
- SSE `done` event carries `next_turn_id` (Event field, no change needed beyond Task 3).
- [ ] Test: conversation with two members lists both.
- [ ] Commit.

### Task 5: Dashboard
**Files:** `app/components/ChatPanel.vue`, `app/components/PromptInput.vue` (mention popover), locales.
- `@` autocomplete: when the text before the caret matches `/@([\p{L}\w-]*)$/u`, show a list of agents (name · level) filtered by the typed part; Enter/click inserts `@Name `.
- Header row: member avatars (initials, colors as in the task conversation view), tooltip name · level · context %.
- Message content: highlight `@Name` of members (post-process rendered HTML safely: only exact names, escaped).
- Follow `next_turn_id` after `done`: keep streaming UI with "X đang trả lời…".
- [ ] `node scripts/check-i18n.mjs`, `pnpm typecheck`, `pnpm build`.
- [ ] Live check on the office: sqlite3 `select count(*) from conversation_agents` > 0; lead of storefront-v5 now operate.
- [ ] Commit.

### Task 6: Docs
- ADR-044 in `docs/DECISIONS.md`, row in `docs/PLAN.md`. `make test`. Commit.
