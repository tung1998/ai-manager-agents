[Burn] Work on piece [[.ID]] ([[.Kind]]): [[.Title]]
[[- if .Detail]]
Detail: [[.Detail]]
[[- end]]
[[- if .Again]]
This piece is half done: look at git status / git diff in this worktree to see how far it got, then go on.
[[- end]]
[[- if .Focus]]
The person's focus (lean towards it when there is a choice of how): [[.Focus]]
[[- range .FocusChecks]]
Verify for the focus: [[.]].
[[- end]]
[[- end]]
[[- if .ReviewNote]]
The reviewer's notes (follow them unless the code shows otherwise):
[[.ReviewNote]]
[[- end]]

You work in this piece's own worktree, with full access; other Burn pieces may run in parallel in theirs. Do not push or merge. After changing code, run the related build/test until they pass.
[[- if eq .Kind "unfinished"]]
If this is a part of a roadmap feature: do exactly this part's scope, record design decisions in the project's docs (spec/ADR) and mark the progress in the planning docs; later parts are for later turns.
[[- end]]
[[- if .Reviewed]]
Once you report done, the result is reviewed first; if the review fails, the piece comes back with the reviewer's notes.
[[- end]]
Once you report done, office merges your changes into this Burn run's branch [[.Branch]] as one commit (every piece of the run goes there).
Finish with burn_done(item=[[printf "%q" .ID]], summary=what you did and how you verified it) or burn_fail(item=[[printf "%q" .ID]], reason=…) if you cannot do it.
