# Broadcast writes are deadline-bounded (`broadcastWriteTimeout`)

**Broadcast writes are deadline-bounded (`broadcastWriteTimeout`).** Events are
emitted from handlers holding `hwMu`, so an unbounded write to a subscriber
that stopped reading would block every hardware operation in the daemon behind
a socket buffer. A subscriber that cannot take a notification in time is
dropped.
