#!/bin/sh
# Runs as the deb/rpm/pacman post-install script, as rpm's posttrans (after an
# upgrade's old %preun), and as pacman's post_upgrade. Idempotent: running it
# twice changes nothing the first run did not.

# The packaged gamepad rules (uaccess for any controller) need the reload.
udevadm control --reload-rules || true
udevadm trigger || true

# setup grants voltaire's group what this machine's device file needs, writes
# /etc/udev/rules.d/99-voltaire.rules and /etc/systemd/system/voltaire-perms.service
# for this machine only, and cleans up what a pre-2.0 "z13ctl setup" installed.
# It is called by its full path so a stale source-install copy in /usr/local/bin
# never becomes the boot unit's binary. On a machine no device file describes it
# grants nothing and says so; the package still installs.
/usr/bin/voltaire setup || echo "voltaire: setup did not complete; run 'sudo voltaire setup' to grant device access."

# CAP_BPF + CAP_PERFMON let the gamescope overlay block controller input at the
# kernel level instead of freezing Steam. Optional — the SIGSTOP fallback works
# without them.
setcap cap_bpf,cap_perfmon+ep /usr/bin/voltaire-gui 2>/dev/null || true

# Migration from z13ctl/z13gui (pre-2.0). The old perms unit is
# RemainAfterExit=yes, so anything short of an explicit "disable --now" leaves
# it active and enabled, holding stale grants. The old user units stay enabled
# until explicitly disabled, and z13gui.service would race the renamed one for
# the overlay. The old sockets keep being served either way: voltaire.socket
# carries the pre-rename listen path too.
systemctl disable --now z13ctl-perms.service 2>/dev/null || true
systemctl --global disable z13ctl.socket z13ctl.service z13gui.service 2>/dev/null || true

systemctl --global enable voltaire.socket voltaire.service || true
systemctl --global enable voltaire-gui.service || true
