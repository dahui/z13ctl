# Every handler that mutates profile or thermal state calls `saveAndNotify`, not `saveState`

**Every handler that mutates profile or thermal state calls `saveAndNotify`,
not `saveState`.** That is the funnel which keeps a new handler from silently
leaving clients showing stale values. Lighting handlers deliberately still call
`saveState` directly — a brightness slider drag would otherwise emit a burst of
events describing values the client just set.
