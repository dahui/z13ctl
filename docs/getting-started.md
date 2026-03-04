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

```sh
# Check current CPU fan curve
z13ctl fancurve --get --fan cpu

# Set a custom CPU fan curve (8 temp:pwm pairs)
z13ctl fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102" --fan cpu

# Reset both fans to firmware auto mode
z13ctl fancurve --reset
```

---

## TDP control

```sh
# Check current TDP/PPT values
z13ctl tdp --get

# Set all PPT limits to 50W
z13ctl tdp --set 50

# Set with individual PL overrides
z13ctl tdp --set 45 --pl2 55 --pl3 60

# Force high TDP (above 75W, fans forced to full speed)
z13ctl tdp --set 85 --force

# Reset to firmware defaults
z13ctl tdp --reset
```

---

## Custom profile

The `custom` profile recalls previously saved fan curves and TDP settings from
the daemon's state. At least one custom fan curve or TDP value must have been
set before switching to `custom`.

```sh
# Set up a custom configuration
z13ctl fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102" --fan cpu
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
- [Daemon](daemon.md) — set up the daemon for state persistence and boot
  restore
