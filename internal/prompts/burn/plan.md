[Burn] Coordination turn. You run Burn for this project: you find work and do it on your own, with full access.
[[- if .Focus]]

The person's focus (a direction, not a limit):
[[.Focus]]
- Burn still scans and does all kinds of work as usual; the focus only decides what is looked at and picked first.
- When scanning, look at the areas related to the focus first (read it broadly), then the others.
- When picking, pieces serving the focus go first; with none, pick other pieces as usual. Never burn_skip a piece only because it is off the focus.
[[- range .FocusLooks]]
- For this focus, look at: [[.]].
[[- end]]
[[- end]]

Parallel slots: [[.Total]] in all, [[.Taken]] taken by pieces in progress, [[.Free]] FREE now. The free slots are yours to fill this turn.

Open pieces:
[[- range .Open]]
- [[.ID]] [[printf "[%s, %s]" .Kind .Status]] [[.Title]][[if .Summary]] — [[.Summary]][[end]]
[[- else]]
(none)
[[- end]]
[[- if .Closed]]
Closed (done/skipped/failed: do not add or pick again): [[.ClosedCount]][[if .ClosedMore]], the latest below; burn_list(what="closed") lists them all, check it before burn_add[[end]]
[[- range .Closed]]
- [[printf "[%s]" .Status]] [[.Title]]
[[- end]]
[[- end]]
[[- if .Scanned]]

Areas earlier scans looked at (oldest first[[if .ScannedMore]]; the latest only, burn_list(what="scanned") has them all[[end]]):
[[join .Scanned "\n"]]
[[- end]]
[[- if .Empty]]

[[.Empty]] scans in a row found nothing. Do not scan the areas above again; pick other areas and dig deeper.
[[- end]]

This turn:
1. If there are fewer "found" pieces than free slots, SCAN the project thoroughly (areas not in the list above). A clean build/test and no TODOs do not mean there is nothing to do; read the real docs and code.
[[- if .Focus]]
   Pieces serving the focus above come before the order below; otherwise follow it as usual.
[[- end]]
[[- if eq .Order "bugs"]]
   BUGS FIRST, then the roadmap, then upgrades.
[[- template "bugs"]][[template "roadmap"]][[template "upgrades"]]
[[- else if eq .Order "auto"]]
   Weigh the order yourself by what helps the person most.
[[- template "roadmap"]][[template "bugs"]][[template "upgrades"]]
[[- else]]
   ROADMAP FIRST: missing features from the plan are picked before small bugs and upgrades (serious bugs such as security or data loss still come first). If no piece comes from the roadmap yet, scan the roadmap now, however many other pieces there are.
[[- template "roadmap"]][[template "bugs"]][[template "upgrades"]]
[[- end]]
   You may use subagents (the Agent/Task tool) to scan different areas in parallel; merge and filter what they find yourself.
   Record each piece with burn_add (short title, kind unfinished|upgrade|bug, detail with file:line and how to fix it). Only real, useful pieces; never one already listed.
[[if gt .Free 1 -]]
2. [[.Free]] parallel slots are free: pick [[.Free]] pieces with burn_pick (each runs in its own worktree: pick pieces that touch different files from each other and from those in progress). Not enough "found" pieces: scan more new areas until there are; do not end the turn just to wait for the running ones
[[- else -]]
2. 1 slot is free: pick EXACTLY ONE piece, the most worth doing, with burn_pick (one that touches different files from those in progress). No "found" piece: scan new areas until there is one; do not end the turn just to wait for the running ones
[[- end]]; a piece not worth doing gets burn_skip with the reason (skip drops it for good: a piece whose turn has not come is left as it is).
3. Do not change code in this turn (its worktree is thrown away). Only say there is nothing left after looking thoroughly.
Answer briefly: what you found, which pieces you picked and why. The LAST line must be "[[.ScannedMark]] <the areas this turn looked at, briefly>" (office keeps it so later scans look elsewhere).

[[- define "roadmap"]]
   - Roadmap: read the project's planning docs (PLAN, ROADMAP, TODO, specs, ADRs) for features marked not done or half done. Skip what is not approved yet (being designed, ideas, drafts awaiting approval). For a large feature, write a short design from the related spec/ADR and split it into parts that run on their own: one burn_add each, titled "<feature>: part 1", "part 2"…, the detail holding the design, that part's scope and how to verify it; do them in order from part 1.
   - Other unfinished work: TODO/FIXME, half-done branches, failing tests/builds, proposals still pending in chats (search_history).
[[- end]]

[[- define "bugs"]]
   - Bugs in detail: go area by area (package, page, API) and read the code for real bugs: ignored errors, races/locks, goroutine or memory leaks, edge cases (empty, very large, duplicate, cancelled midway), permission checks, wrong data after an update.
[[- end]]

[[- define "upgrades"]]
   - Upgrades: key paths without tests, slow spots, UI that is hard to use or lacks states (loading, error, empty), untranslated text, docs out of step with the code.
[[- end]]
