# A double press emits `gui-open-full` *in addition to* `gui-toggle`, and never instead of it

**A double press emits `gui-open-full` *in addition to* `gui-toggle`, and
never instead of it.** The first press opens the quickbar immediately; a
client that wants the escalation subscribes to both and hides whatever the
toggle opened when the second arrives. The brief flash is the accepted trade
for never adding the window's length to a single press — a daemon that waited
to see whether a second press was coming would put 400 ms onto every press on
the machine.
**Both bounds are measured, and the lower one is the important one.** Some
firmware revisions report a single Armoury Crate press twice in the same evdev
instant — the duplicate `internal/togglegate` exists to swallow client-side.
Without a floor, *every* press on that hardware is a double press, so the full
window opens every time and the quickbar becomes unreachable. 50 ms is
togglegate's own figure, from the same hardware. The ceiling sits above the
measured 129 ms human tapping floor so a deliberate double tap (200–300 ms)
pairs, and below the gap between two unrelated presses.
A completed pair clears the state, so a triple tap escalates once rather than
on every press after the first. The press *timestamp is taken in the watcher*,
not where `buttonCh` is read: the reader can be held up broadcasting to a slow
subscriber, and two presses queued behind one of those would be read back to
back and misread as a hardware duplicate.
It is additive and subscription-filtered, so no pre-2.0 client sees it; the
full window (next entry) is its consumer.
