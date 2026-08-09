#!/bin/sh
udevadm control --reload-rules
udevadm trigger
systemctl enable --now voltaire-perms.service || true
# "enable --now" is a no-op on upgrade: the unit is Type=oneshot with
# RemainAfterExit=yes, so it is already active and new ExecStart lines never run.
# Restart so an upgrade applies added permission grants without a reboot.
systemctl restart voltaire-perms.service || true
# Migration from z13ctl (pre-2.0): the old perms unit is also
# RemainAfterExit=yes, so anything short of an explicit "disable --now" leaves
# it active and enabled, holding stale grants. The old user units are replaced
# by voltaire.socket/.service serving both socket paths.
systemctl disable --now z13ctl-perms.service 2>/dev/null || true
systemctl --global disable z13ctl.socket z13ctl.service 2>/dev/null || true
systemctl --global enable voltaire.socket voltaire.service || true
