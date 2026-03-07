# Quick Start

These examples assume you have [installed z13ctl](installation.md) and run
`sudo z13ctl setup`. The daemon does not need to be running — commands fall
back to direct hardware access automatically. See [Daemon](daemon.md) for why
you probably want the daemon running anyway.

---

## Lighting

```sh
# Solid cyan at full brightness (keyboard + lightbar)
z13ctl apply --color cyan --brightness high

# Breathing red, slow pulse
z13ctl apply --mode breathe --color red --speed slow

# Breathing between two colors
z13ctl apply --mode breathe --color hotpink --color2 blue

# Rainbow wave across both zones
z13ctl apply --mode rainbow --speed normal

# Spectrum cycle, fast
z13ctl apply --mode cycle --speed fast

# Strobe white
z13ctl apply --mode strobe --color white

# Turn off all lighting
z13ctl off
```

---

## Brightness

```sh
# Adjust brightness without changing mode or color
z13ctl brightness high
z13ctl brightness medium
z13ctl brightness low
z13ctl brightness off
```

---

## Performance profile

```sh
# Check current profile
z13ctl profile --get

# Switch profiles
z13ctl profile --set performance
z13ctl profile --set balanced
z13ctl profile --set quiet
```

---

## Battery charge limit

```sh
# Check current limit
z13ctl batterylimit --get

# Cap charging at 80% (recommended for mostly-plugged-in use)
z13ctl batterylimit --set 80

# Remove the limit (charge to 100%)
z13ctl batterylimit --set 100
```

---

## Fan curves

Both physical fans cool the same APU, so the same curve is always applied to
both fans simultaneously.

```sh
# Check current fan curves
z13ctl fancurve --get

# Set a custom fan curve using PWM values (8 temp:speed pairs, both fans)
z13ctl fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"

# Or use percentages (0–100%)
z13ctl fancurve --set "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"

# Reset both fans to firmware auto mode
z13ctl fancurve --reset
```

---

## TDP control

TDP (Thermal Design Power) controls how much power the APU can draw. There are
three limits that form a hierarchy: PL1 is the sustained (continuous) limit,
PL2 allows short-term bursts above PL1 for several seconds, and PL3 allows
instantaneous spikes for milliseconds. Setting all three to the same value gives
a flat power cap; setting PL2 and PL3 higher allows bursty workloads to
temporarily exceed PL1.

Stock profiles (quiet/balanced/performance) manage TDP automatically. Custom TDP
values override this and switch to the `custom` profile.

```sh
# Check current TDP/PPT values
z13ctl tdp --get

# Set all PPT limits to 50W
z13ctl tdp --set 50

# Set with individual PL overrides
z13ctl tdp --set 45 --pl2 55 --pl3 60

# Force high TDP (above 75W, fans set to 80% minimum)
z13ctl tdp --set 85 --force

# Reset to balanced profile (firmware manages PPT)
z13ctl tdp --reset
```

---

## Undervolting (Curve Optimizer)

Undervolting reduces CPU voltage via AMD Curve Optimizer (CO), lowering
temperatures and power draw without reducing performance. Requires the
`ryzen_smu` kernel module (optional — see [Installation](installation.md)).

CO values are volatile — they reset on reboot and sleep. The daemon reapplies
them automatically on startup and resume when the custom profile is active.

```sh
# Check current CO value
z13ctl undervolt --get

# Set CPU CO to -20
z13ctl undervolt --set -20

# Reset to stock voltage
z13ctl undervolt --reset
```

Safety limit (matching G-Helper defaults): CPU 0 to -40.

---

## Custom profile

The `custom` profile recalls previously saved fan curves, TDP, and undervolt
settings from the daemon's state. At least one custom fan curve, TDP value, or
undervolt offset must have been previously set.

```sh
# Set up a custom configuration
z13ctl fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"
z13ctl tdp --set 50

# Recall it later with custom profile
z13ctl profile --set custom

# Switch back to a stock profile (resets fan curves and TDP)
z13ctl profile --set balanced
```

---

## Per-device control

Use `--device` to target only the keyboard or lightbar:

```sh
# Keyboard to red, lightbar to blue
z13ctl apply --color red --device keyboard
z13ctl apply --color blue --device lightbar

# Turn off just the lightbar
z13ctl off --device lightbar
```

---

## Preview without applying changes

`--dry-run` shows exactly what packets or writes would be sent, without
touching any hardware:

```sh
z13ctl --dry-run apply --mode rainbow --speed fast
z13ctl --dry-run setup
```

---

## See all named colors

```sh
z13ctl apply --list-colors
```

This prints a live swatch table in your terminal. Any 6-digit hex value
(`RRGGBB`, without `#`) is also accepted wherever a color name is.

---

## Next steps

- [Commands](commands.md) — every flag and option for every command
- [Daemon](daemon.md) — set up the daemon for state persistence, boot
  restore, and sleep/resume recovery
