# MCP other_office Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One built-in MCP server `other_office` (tools `offices`, `send`, `read_chat`, `search`) is the only way agents reach other offices; the peer branches of `send_to_chat`/`read_link`/`search_history` go away.

**Architecture:** The tools live in `internal/officetools/other.go` on the same `Toolbox` and `Scope`. `internal/mcpserver` serves them on `/mcp/other_office` with the run's token (same grants). Runs get the server only when the scope reaches at least one project of another office. `send` proposes the existing `send_message` action with `Args.Peer` set; `actions.checkSend` validates it, and `chat.relayPeer` delivers it as today.

**Tech Stack:** Go (net/http, encoding/json), existing `internal/peer` client.

**Spec:** `docs/superpowers/specs/2026-10-20-other-office-mcp-design.md`

## Global Constraints

- Server name `other_office`; agents see `mcp__other_office__offices|send|read_chat|search`.
- Project agent: only projects in `Policy.LinkedProjects` (`peer:<peer>/<project>`) still shared; `read_chat` also on chats this project already sent to (done `send_message` with that `Args.Peer` and `TargetID`).
- Office assistant (`Scope.Office`): every paired office, every project it opens to us; any account may `send`; AnswerOnly mode gets no `send`.
- `send` needs level ≥ propose and a conversation, like `send_to_chat`; `relay_hops`, `chat.cross` unchanged.
- No change to `/api/peer/*`, the receiving side, or the Office khác page.
- Old entry points answer with a short error pointing to `mcp__other_office__*`.

## Review Focus

- Office or project named by name with different case, or by id: resolves.
- A `↩ Báo lại` link read after the link was removed in settings: still readable (chat sent to before).
- Project agent with no peer links: no `other_office` server in its run, tool list empty.
- Peer removed between proposal and approval: the send fails with "office liên kết không còn".
- AnswerOnly assistant calling `send`: refused (not listed, `Has` false).

---

### Task 1: other_office tools in officetools

**Files:**
- Create: `internal/officetools/other.go`, `internal/officetools/other_test.go`
- Modify: `internal/officetools/officetools.go` (move `searchPeer` formatting into other.go)

**Interfaces:**
- Produces: `const OtherServer = "other_office"`; `func (t *Toolbox) OtherTools(ctx context.Context, sc Scope) []Tool` (nil when nothing reachable); `func (t *Toolbox) HasOther(ctx, sc, name string) bool`; `func (t *Toolbox) CallOther(ctx, sc, name string, raw json.RawMessage) (string, bool)`.
- Tool params: `offices` {}; `send` {office, project, chat?, text, reason}; `read_chat` {office?, chat (id or link), message?}; `search` {office, project, query, days?, author?}.

- [ ] Tests (fake peer office via httptest serving `/api/peer/search` and `/api/peer/chats/`): `TestOtherToolsScope` (agent without links → nil; with link → 4 tools, `offices` lists only linked project; assistant → all remote projects), `TestOtherResolveByNameOrID`, `TestOtherReadChatSentBefore` (unlinked project, done send_message action to that chat → allowed; other chat → refused).
- [ ] Implement; run `go test ./internal/officetools/`.

### Task 2: send through actions

**Files:** Modify `internal/actions/actions.go` (`checkSend`, drop `peerLink`), test in `internal/actions` test file.

- [ ] `checkSend`: when `a.Args.Peer != ""`: peer must exist; new chat → project must be in `p.Remote` and (sc.Office or linked); existing chat (`TargetID`) → sc.Office, linked, or sent before. Target label `<office>/<project> · chat mới|<chat>`.
- [ ] Without `Args.Peer`, an absolute link of a paired office or a `<office>/<project>` name → error "Office khác: dùng mcp__other_office__send".
- [ ] `go test ./internal/actions/ ./internal/chat/`.

### Task 3: drop peer branches of old tools

**Files:** `internal/officetools/readlink.go`, `officetools.go` (`searchHistory`, `send_to_chat` descriptions), `internal/chat/relay.go` (`linkedBrief`), `internal/prompts/chat/linked.md`, `internal/prompts/assistant/instructions.md`, `internal/chat/relay_peer.go` (`Peers` interface: drop `LinksOf` if unused).

- [ ] `read_link` on a paired office's link and `search_history` with `<office>/<project>` → hint error.
- [ ] Linked brief lists only this office's projects; when the run has other_office, one line: use `mcp__other_office__*` for other offices (its replies' links read with `read_chat`).
- [ ] Assistant instructions: other offices only via other_office; never SSH or call their API.
- [ ] Update tests that used the old paths (`readlink_test.go`, `internal/api/peers_test.go`, chat relay tests).

### Task 4: serve and wire into runs

**Files:** `internal/mcpserver/mcpserver.go`, `internal/api/org.go`, `internal/chat/engine.go` (`OfficeAccess.Other`), `runner.go`, `run_claude.go`, `run_codex.go`, `run_gemini.go`, `run_api.go`.

- [ ] mcpserver: request path ending `/other_office` uses `OtherTools`/`HasOther`/`CallOther`; test in `mcpserver_test.go` (tools/list on that path lists `offices`).
- [ ] `officeAccess` sets `Other = len(Tools.OtherTools(ctx, sc)) > 0`; CLI runners add server `other_office` at `MCPURL+"/other_office"` and allow `mcp__other_office__*`; API runs list tools as `mcp__other_office__<name>` and dispatch them to `CallOther`.
- [ ] `go build ./... && go vet ./... && go test ./...`.

### Task 5: docs

- [ ] ADR-169 in `docs/DECISIONS.md`; ADR-167 "Liên kết"/"Đọc và tìm" point to it; `docs/INTERFACES.md` if it lists MCP tools.
