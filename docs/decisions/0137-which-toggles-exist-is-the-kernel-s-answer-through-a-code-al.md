# Which toggles exist is the kernel's answer through a code allowlist

**Which toggles exist is the kernel's answer through a code allowlist**
(`asus.safeToggles`; Jeff, 2026-10-09). The driver reads each allowlisted
armoury attribute's static metadata on first `List`: `type`,
`possible_values` (→ `ToggleSpec.Values`, served as `toggles[].values`),
the file mode (read-only means a reading, not a switch) and `display_name`.
It never reads `current_value`, which is live WMI. Anything not a writable
0/1 switch is skipped, and the device file supplies wording and can `hidden`
an entry.
The allowlist is the safety boundary, and it lives in code because
attribute names are kernel ABI: armoury on other ASUS models exposes
`gpu_mux_mode`, `dgpu_disable`, `egpu_enable`, `apu_mem`, `cores_*`,
`mini_led_mode`, which can black-screen a machine. "Offer everything the
kernel lists" would have made those a row like any other. `voltaire setup`
grants exactly the allowlist (byte-identical output for the Z13's two). Every
write validates through `ToggleSpec.Accepts`, so there are no `0 || 1`
literals in the handlers or the CLI. With no attribute directory
(asus-armoury not loaded), the declared list stands.
