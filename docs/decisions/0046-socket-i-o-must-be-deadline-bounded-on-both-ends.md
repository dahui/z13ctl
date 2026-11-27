# Socket I/O must be deadline-bounded on both ends

**Socket I/O must be deadline-bounded on both ends.** `net.DialTimeout` bounds
only connecting. `api.sendCommand` sets a full-exchange deadline
(`commandTimeout`, 10s) or a daemon that accepts and never replies hangs the
CLI forever; `handleConn` sets `requestReadTimeout` (30s) on the request line
or a client that connects and stays silent pins a goroutine and fd for the
daemon's lifetime. Both deadlines are cleared for `subscribe`, which is idle by
design. Timeouts are vars so tests can shorten them.
