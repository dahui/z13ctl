# No permission artifact is packaged; every package runs `voltaire setup`

**No permission artifact is packaged; every package runs `voltaire setup`**
(Jeff, 2026-11-26). The packages used to ship static copies of the rules and
perms unit, held to setup's output by drift tests, because the files were one
set for every machine. That shipped every device's grants to every machine,
and the drift between copies had already cost .rpm users all PPT and
fan-curve access once (issue #12). Now setup writes both for the machine it
runs on, from `/usr/bin/voltaire` explicitly (so a stale `/usr/local/bin`
copy never becomes the boot unit's binary), and `setup --remove` undoes it
(only files carrying `perms.Marker`, and only grants still carrying the
group). `TestNoPermissionArtifactIsPackaged` keeps a copy from coming back.
Image-based builds (bootc) would run setup against the build host's DMI and
grant nothing: a known gap.
