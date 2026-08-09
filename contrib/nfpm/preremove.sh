#!/bin/sh
systemctl --global disable voltaire.socket voltaire.service || true
systemctl disable --now voltaire-perms.service || true
