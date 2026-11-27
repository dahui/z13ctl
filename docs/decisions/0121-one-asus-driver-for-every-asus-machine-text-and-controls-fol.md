# One ASUS driver for every ASUS machine; text and controls follow the device

**One ASUS driver for every ASUS machine; text and controls follow the
device** (Jeff, 2026-11-15; `.claude/plans/device-genericity.md`). Three
changes, made so the ROG Ally X and Xbox Ally X can be device files rather
than code. (1) The Z13 package `asusz13` was split by vendor/subsystem into
`asus`, `amd` and `linux` — move, don't rewrite: method names unchanged, all
141 tests kept, four assertions changed. No driver package imports another;
`register` injects the couplings (the armoury charger into the battery,
`throttle_thermal_policy` into the profile watcher, AMD's sources into the
telemetry composer), and fan RPM moved to the daemon (`liveSample`) so it has
one reader. The Z13's 5 W asus-nb-wmi boot cache became device data
(`power.boot_cache_pl1`) rather than a driver constant. (2) `device-get`
gained the words a client needs to stay vendor-neutral: `name`,
`power.limits[{id,label,description}]` (kernel presence ∩ the TOML's
`[[power.limits]]`, wording TOML → driver → client), `undervolt.{label,
short_label,unit,description}` (TOML → `driver.UndervoltDescriber`, AMD's
"CPU Curve Optimizer"/"CO"), and `lighting.brightness_names`. All
additive; an older daemon gets the client's built-in words. (3) The GUI and
CLI read them: the editor builds POWER/FAN/UNDERVOLT cards per
`profileui.SectionsFor` and one slider per served limit; the Profiles tab
needs profiles plus something to tune; Settings always shows (it holds
VOLTAIRE and QUICKBAR) while its FIRMWARE card and the button preference go
on a device without them; hints, the undervolt note, the header line and the
"no custom profiles" gesture are all `profileui`/`buttonpref` rules; the CLI
lists "Supported devices" from the device files' names and prints only the
device's limits in `tdp --get`/`status`. On the Z13 the focus dump stayed
byte-identical on all six lines and every CLI output but help is
byte-identical; the one visible change is "PL2 (SPPT)/PL3 (FPPT)" becoming
"PL2 (sPPT)/PL3 (fPPT)", the kernel's spelling and the CLI's. The developer
guide (`website/.../dev/`: architecture, device-files, writing-a-driver,
plugins — the last marked *planned*) is guarded by
`internal/drivers/register/docs_test.go`: the device-file page's example must
parse and assemble, and every schema key must appear on the page.
**Deferred, recorded in the plan:** setup grants sourced from the drivers
(with M5's `[[perms]]`), per-device dry-run data, a 1080p `uiscale`
reference width, refusing `--pl2/--pl3` where absent, the `CacheStale` TDPMin
fallback, the Aura init packet as a device flag, `examples/plugins/`.
