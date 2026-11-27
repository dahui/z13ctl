# Lighting is described by the driver, and every layer reads that description

**Lighting is described by the driver, and every layer reads that
description** (`driver.Lighting.Caps()`; 2026-10-09). The zone table (name,
label, USB id, zone byte) is the device file's; the modes with the inputs each
takes (`aura.Modes`), the speeds, and the 0–3 brightness scale
(`aura.MaxBrightness`) are the protocol's, with the device file free to list a
subset of modes and speeds. Daemon validation (`lightingRequestError`, which
also refuses an unknown zone), the CLI's parsing, help and confirmation text,
`device-get` (`lighting.labels/modes/speeds/brightness_max`) and the GUI's
tabs, cards, effect buttons, speed row and slider all come from it, so no
layer restates a mode list or a 0–3. `kbd_backlight`'s `max_brightness` is
read only as a cross-check (a warning on disagreement). The CLI's no-daemon
lighting path goes through the assembled driver like the daemon's, not
through raw `hid`/`aura` calls. The init and power quirks stay in `aura` with
no flag: only the Z13's variant is known, so a flag would have no second
branch to test.
