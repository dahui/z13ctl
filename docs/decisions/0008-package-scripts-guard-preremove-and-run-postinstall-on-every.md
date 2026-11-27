# Package scripts: guard preremove, and run postinstall on every upgrade path

**Package scripts: guard preremove, and run postinstall on every upgrade
path.** rpm runs the *old* package's `%preun` after the new `%post`, with
`$1 = 1`, and dpkg calls prerm with `upgrade`: the old preremove, with no
guard, disabled every unit the new package had just enabled, on every rpm
upgrade. preremove now returns on `1|2|upgrade|failed-upgrade|deconfigure`
before `setup --remove`. pacman runs only `post_upgrade` on an upgrade, so
nfpm's archlinux `postupgrade` and rpm's `posttrans` both run postinstall.sh,
which is idempotent. The perms unit is a `RemainAfterExit` oneshot, so an
upgrade must restart it, not merely enable it: setup runs `reenable` (it
replaces an enable link to a unit a package used to ship under `/usr/lib`)
and `restart`. `TestPackagesRunSetup`.
