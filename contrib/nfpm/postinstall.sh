#!/bin/sh
udevadm control --reload-rules
udevadm trigger
systemctl enable --now z13ctl-perms.service || true
# "enable --now" is a no-op on upgrade: the unit is Type=oneshot with
# RemainAfterExit=yes, so it is already active and new ExecStart lines never run.
# Restart so an upgrade applies added permission grants without a reboot.
systemctl restart z13ctl-perms.service || true
# Before 1.4.4 the lighting rule granted every interface of the keyboard cover,
# its typing interface included. udev leaves a node's permissions alone once no
# rule sets them, so the narrowed rule alone would leave that grant in place
# until a reboot; setup --perms-only takes it back now.
/usr/bin/z13ctl setup --perms-only || true
systemctl --global enable z13ctl.socket z13ctl.service || true
