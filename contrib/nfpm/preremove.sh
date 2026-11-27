#!/bin/sh
# Only a real removal undoes anything. On an upgrade rpm runs the OLD package's
# %preun after the new package's %post (with $1 = 1), and dpkg calls prerm with
# "upgrade": without this guard every upgrade ended with the units the new
# package had just enabled disabled again. pacman calls this on removal only.
case "$1" in
	1 | 2 | upgrade | failed-upgrade | deconfigure) exit 0 ;;
esac

# Removes the boot unit and the rules file setup wrote (only if they carry its
# marker) and takes voltaire's group back from everything it was granted.
/usr/bin/voltaire setup --remove || true

systemctl --global disable voltaire.socket voltaire.service || true
systemctl --global disable voltaire-gui.service || true
