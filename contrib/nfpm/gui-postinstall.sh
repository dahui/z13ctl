#!/bin/sh
udevadm control --reload-rules
udevadm trigger
setcap cap_bpf,cap_perfmon+ep /usr/bin/voltaire-gui 2>/dev/null || true
# Migration from z13gui (pre-2.0): its user unit stays enabled until
# explicitly disabled, racing the renamed one for the overlay.
systemctl --global disable z13gui.service 2>/dev/null || true
systemctl --global enable voltaire-gui.service || true
