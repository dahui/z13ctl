#!/bin/sh
udevadm control --reload-rules || true
udevadm trigger || true
