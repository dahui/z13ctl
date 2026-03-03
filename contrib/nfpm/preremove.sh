#!/bin/sh
systemctl --global disable z13ctl.socket z13ctl.service || true
systemctl disable --now z13ctl-perms.service || true
