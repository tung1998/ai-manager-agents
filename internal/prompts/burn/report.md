[Burn] Piece [[.ID]]: your turn ended without a report, so office cannot tell how it went.
If you were waiting on a background command, nothing wakes you when it ends: run it again in the foreground and wait for it.
Then finish now with burn_done(item=[[printf "%q" .ID]], summary=what you did and how you verified it) or burn_fail(item=[[printf "%q" .ID]], reason=…).
