#!/bin/sh
udevadm control --reload-rules
udevadm trigger

systemctl enable --now voltaire-perms.service || true
# "enable --now" is a no-op on upgrade: the unit is Type=oneshot with
# RemainAfterExit=yes, so it is already active and new ExecStart lines never run.
# Restart so an upgrade applies added permission grants without a reboot.
systemctl restart voltaire-perms.service || true

# CAP_BPF + CAP_PERFMON let the gamescope overlay block controller input at the
# kernel level instead of freezing Steam. Optional — the SIGSTOP fallback works
# without them.
setcap cap_bpf,cap_perfmon+ep /usr/bin/voltaire-gui 2>/dev/null || true

# Migration from z13ctl/z13gui (pre-2.0). The old perms unit is also
# RemainAfterExit=yes, so anything short of an explicit "disable --now" leaves
# it active and enabled, holding stale grants. The old user units stay enabled
# until explicitly disabled, and z13gui.service would race the renamed one for
# the overlay. The old sockets keep being served either way: voltaire.socket
# carries the pre-rename listen path too.
systemctl disable --now z13ctl-perms.service 2>/dev/null || true
systemctl --global disable z13ctl.socket z13ctl.service z13gui.service 2>/dev/null || true

systemctl --global enable voltaire.socket voltaire.service || true
systemctl --global enable voltaire-gui.service || true
