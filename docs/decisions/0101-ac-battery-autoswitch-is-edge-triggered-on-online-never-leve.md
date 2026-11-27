# AC/battery autoswitch is edge-triggered on `online`, never level-triggered, and never reads `platform_profile`

**AC/battery autoswitch is edge-triggered on `online`, never level-triggered,
and never reads `platform_profile`** (`internal/daemon/powersource.go`, issue
#6). That is the whole safety argument: the watcher reacts only to a value
neither voltaire nor PPD nor the desktop can write, so the feedback loop that
would produce a write-fight does not exist. A level-triggered "keep my profile
applied" watcher would both fight PPD over every transition — the thing
`reconcile.go` exists to avoid — and make a manual profile change impossible to
hold. The intended semantics follow directly: a profile chosen by hand sticks
until the source actually changes, and voltaire yields in between (GNOME's
Automatic Power Saver is a *low-battery* trigger, not an unplug trigger, and is
correctly ignored). One observation function, `linux.OnACPower()`, with two
triggers: a UPower `OnBattery` nudge for immediacy and a 2s poll as the
backstop for Gaming Mode / no-UPower setups — so there is one behaviour to
test, not two. An edge is confirmed on the following observation before
applying; that settle window lets PPD's own transition write land first (else
the custom curve is dropped until reconcile notices) and stops a loose USB-C
connector driving a full PPT+fan+SMU write per bounce. A failed apply latches
the source anyway and does not retry. **The startup apply lives in `Run()`, not
the watcher**, which latches its first observation without acting: `Run()`
holds the same-value guard that keeps a redundant `platform_profile` write —
and the WMI fan-controller reset that comes with it — out of every daemon
restart. There is deliberately no hook in `resume.go`: Go timers use
`CLOCK_MONOTONIC` and do not advance across suspend, so the armed timer fires
promptly after resume; duplicating it would race the watcher over `hwMu`.
