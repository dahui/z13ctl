#!/bin/sh
udevadm control --reload-rules
udevadm trigger
setcap cap_bpf,cap_perfmon+ep /usr/bin/z13gui 2>/dev/null || true
systemctl --global enable z13gui.service || true
