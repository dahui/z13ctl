# The AUR package is setup-driven, like the others

**The AUR package is setup-driven, like the others.** `voltaire-bin.install`
runs `/usr/bin/voltaire setup` on install and upgrade and `setup --remove` in
`pre_remove`; the PKGBUILD ships no rules file and no perms unit.
