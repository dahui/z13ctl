#!/bin/sh
udevadm control --reload-rules
udevadm trigger
systemctl enable --now z13ctl-perms.service || true
systemctl --global enable z13ctl.socket z13ctl.service || true
