# asus-armoury firmware-attributes

**asus-armoury firmware-attributes**: The `asus_armoury` kernel module (mainline
since Linux 6.19) exposes BIOS attributes via the `fw_attributes_class` interface
at `/sys/class/firmware-attributes/asus-armoury/attributes/`. On the 2025 Z13,
`boot_sound` (POST beep toggle, 0/1) and `panel_overdrive` (panel refresh overdrive,
0/1) are available and writable. These are BIOS firmware settings managed by the
kernel — no daemon state persistence needed. The `current_value` files are
`0644 root:root` by default; `setup.go` handles permissions via udev rules
(`SUBSYSTEM=="firmware-attributes", KERNEL=="asus-armoury"`) and direct `applySysfsPerms()`.
Other attributes exist (`charge_mode` is read-only charger type detection). PPT
controls were empty on Z13 on early kernels for want of DMI calibration data;
7.x kernels carry a GZ302EA entry and expose `ppt_pl1_spl`/`ppt_pl2_sppt`/
`ppt_pl3_fppt` (PL1 28–80, PL2 32–92, PL3 45–93; no APU/Platform sPPT), and
voltaire writes power limits through them — see the TDP note. They write the
same WMI IDs as the `asus-nb-wmi` path, and `current_value` is armoury's own
per-power-source value, so it never reflects writes made the other way.
**Every armoury PPT read is a live ACPI call**: `current_value`, `min_value`,
`max_value` and `default_value` all go through `get_current_tunables()`, which
calls `power_supply_is_system_supplied()` to pick the AC or battery table — the
AC adapter's `_PSR`, each time. See the wedged-EC note.
