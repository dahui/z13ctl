# `telemetry-history` is a typed `history` field, not JSON stuffed into `value`

**`telemetry-history` is a typed `history` field, not JSON stuffed into
`value`.** `Value`'s embedded-JSON convention exists for the commands that
predate a structured reply; `device-get` established the typed field, and
double-encoding hurts most at exactly this size — a full window is 3600 samples
whose every quote would be escaped to travel as a string. Sending it also
raised `api.sendCommand`'s reader ceiling: `bufio.Scanner` defaults to 64 KiB,
which every command fitted under until this one, and a device declaring an hour
of history would answer with 3600 samples and fail as "token too long" — a
failure that looks like a broken daemon and depends on device data. The ceiling
is raised to 4 MiB rather than removed, since it is what stops a wedged daemon
growing the client's memory without bound.
**That hypothetical is now the shipped configuration**: the Z13 declares 3600
seconds, and a whole-window reply is ~1.5 MiB at the measured 440 bytes per
sample. The ceiling is therefore also the real bound on how much history a
device may declare — about two and a half hours — and `device.DefaultHistorySeconds`
says so where someone would go to raise it. Growing the ring past that needs
the reply paged first; it is not a number to raise on its own.
The **wire default differs from the ring's on purpose**: absent `seconds` means
the whole retained window, while `telemetryring.Since` refuses a non-positive
duration. The ring cannot tell a caller that meant "everything" from one that
failed to parse its own field; the protocol can, and every other read command
here answers with all it has when given no argument.
