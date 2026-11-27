#!/bin/sh
udevadm control --reload-rules || true
udevadm trigger || true
systemctl daemon-reload || true
