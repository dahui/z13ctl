# Setup applies its grants twice, and both come from one value

**Setup applies its grants twice, and both come from one value.** udev rules
cover real device events (every boot, a reattached keyboard cover); the boot
pass (`perms.Apply`) covers attributes created after their udev event or with
none, and runs again right after setup writes the rules, because systemd 259+
does not execute `RUN{program}` on a synthetic `udevadm trigger`. Both render
the same `perms.Grants`, so they cannot disagree.

**The boot pass is also what undoes udev's name-reuse race** (seen on the
Z13, 2026-11-26, first boot with per-machine rules). The lightbar
(`0b05:18c6`) is bound by `hid-generic` in the initramfs (interface 0 →
`hidraw5`, 1 → `hidraw6`) and rebound by `asus` after switch-root, which
renumbers it (interface 1 → `hidraw5` at 5.30 s, 0 → `hidraw8` at 5.34 s).
Coldplug at 4.52 s had queued an `add` for interface 0 as `hidraw5`, and udev
applies `GROUP`/`MODE` to the node *by name*, so that event landed on
interface 1's node: `users` on an interface no rule matches. The boot pass at
8.88 s counted it as its one revoke (`14 granted, 43 already granted, 1
revoked`), and the stale-hidraw reset in `perms.Apply` exists for exactly this
shape. The static rules file had the same race with nothing to undo it.
Ordering after `systemd-udev-trigger` only means coldplug was *queued*, and the
`asus` rebind is a modalias load, not `systemd-modules-load`, so the pass could
win the race and leave the stray grant in place. The unit therefore runs
`ExecStartPre=-udevadm settle --timeout=30` first: only this unit waits for the
queue to drain, bounded, and `-` makes a slow queue delay the pass rather than
fail it (`TestUnit`).

**The race also lands outside voltaire's devices, so the reset covers every
hidraw node when it safely can** (the next boot, same day). The keyboard's
interface 0 was `hidraw0` from 2.35 s; at 4.69 s the kernel gave that name to
the ITE8353 sensor hub (I2C, `048d:8353`, in no device file), and the queued
event left it `users` 0660. The reset then covered only devices some device
file names, and USB nodes only, so it stayed. Widening it to *any* hidraw node
carrying the group would strip a grant another rules file made, since
`users` is a group anyone may hand out, so the widening is conditional:
`perms.OnlyOursGrant` scans udev's rule directories for any file but ours
(a same-named copy is one udev shadows) assigning `GROUP="<group>"` or
running `chgrp <group>`, comments excluded. When none does, the group on a
hidraw node can only be ours, and the pass resets every one the grants do not
name, I2C nodes included (`stray hidraw`, logged by device); when one does,
setup prints that file and keeps the known-devices reset
(`TestStrayHIDRawGrants`, both cases, negative controls run). `setup --remove`
applies the same rule.
