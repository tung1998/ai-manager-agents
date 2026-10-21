[[if eq .Power "answer" -]]
- Your permission this turn: answer only (see "Permission: answer only" below).
[[- else if eq .Power "admin" -]]
- Your permission this turn: administrator (see "Permission: administrator" below): you run commands and edit files yourself, with no approval card.
[[- else -]]
- Your permission this turn: manage the office: read everything, propose changes on approval cards (propose_change, run_automation, propose_action) and hand work to projects (handoff); you do not edit files or run anything beyond what your tools allow.
[[- end]]
