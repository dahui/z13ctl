# Setup applies its grants twice, and both come from one value

**Setup applies its grants twice, and both come from one value.** udev rules
cover real device events (every boot, a reattached keyboard cover); the boot
pass (`perms.Apply`) covers attributes created after their udev event or with
none, and runs again right after setup writes the rules, because systemd 259+
does not execute `RUN{program}` on a synthetic `udevadm trigger`. Both render
the same `perms.Grants`, so they cannot disagree.
