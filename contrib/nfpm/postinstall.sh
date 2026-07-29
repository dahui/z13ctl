#!/bin/sh
udevadm control --reload-rules
udevadm trigger
systemctl enable --now z13ctl-perms.service || true
# "enable --now" is a no-op on upgrade: the unit is Type=oneshot with
# RemainAfterExit=yes, so it is already active and new ExecStart lines never run.
# Restart so an upgrade applies added permission grants without a reboot.
systemctl restart z13ctl-perms.service || true
systemctl --global enable z13ctl.socket z13ctl.service || true
