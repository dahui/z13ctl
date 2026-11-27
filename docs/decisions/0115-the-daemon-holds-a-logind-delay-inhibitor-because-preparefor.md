# The daemon holds a logind delay inhibitor, because `PrepareForSleep(true)` is otherwise advisory

**The daemon holds a logind delay inhibitor, because
`PrepareForSleep(true)` is otherwise advisory.** logind emits it and proceeds
to freeze; the release's sysfs writes racing that is how the fix would silently
not apply on a fast-suspending system. `takeSleepInhibitor` must be called
*before* the signal can arrive (watcher start, and again after each resume), and
the fd is closed immediately after `releaseVolatileState` returns — logind
suspends as soon as the last delay lock closes, so deferring it to the end of the
loop would hold suspend open for `InhibitDelayMaxSec` (5 s by default).
Best-effort throughout: a refused `Inhibit` logs at Debug and returns -1.
A delay after the writes was first added on the theory that the EC answers them
and aborts the suspend, then removed when that control run aborted without the
daemon. **It is back, as the device's sleep release settle, and this time on
measured evidence** — see the next note. It applies only when the hook actually
released something.
