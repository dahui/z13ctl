---
title: Lighting
description: "RGB lighting: effects, colors, speed, brightness and zones, in the GUI and on the command line."
---

# Lighting

voltaire sets your machine's RGB lighting: the effect, one or two colors, the
animation speed and the brightness. Where a machine has more than one lit
part, each **zone** can be set together or on its own. The daemon remembers
what you chose and puts it back after a reboot, a suspend, or (on the Z13)
reattaching the keyboard.

| Effect | What it does | Colors | Speed |
|---|---|---|---|
| Static | One solid color | 1 | — |
| Breathe | Fades between two colors | 2 | yes |
| Cycle | Runs through the whole spectrum | — | yes |
| Rainbow | A rainbow wave across the zones | — | yes |
| Strobe | Rapid flashing | 1 | yes |

**Speed** is slow, normal or fast. **Brightness** is off, low, medium or high.

| Device | Zones | Effects |
|---|---|---|
| [ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | Keyboard, Lightbar | All of the above |
| [ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | Joystick rings | Static, breathe, cycle, strobe |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | Not supported yet | — |

## In the GUI

The drawer's **RGB lighting** section has, from top to bottom:

- **Zone tabs** (on the Z13, Keyboard and Lightbar) to choose which zone you
  are setting.
- **Mode**, the effect, including **Off**.
- **Color 1** and **Color 2**: eight preset swatches, the current color, and
  **Custom…** for the color picker. Color 2 appears only for effects that use
  it.
- **Speed** and **Brightness**, shown for the effects that have them.

On the full window's **Dashboard**, the RGB band at the bottom shows one card
per zone, side by side, with the same controls. Every change applies at once.

### The color picker

**Custom…** opens a color picker over the page you are on. Drag the Hue,
Saturation and Lightness sliders; each is painted with what it will do, and
the lighting follows live as you drag. The bar along the bottom and the hex
code show the result, and the eight presets are repeated inside so you can
jump to one. Click outside, press Escape, or press B on a controller to close
it.

## On the command line

```sh
voltaire apply --color cyan --brightness high
voltaire apply --mode breathe --color red --color2 blue --speed slow
voltaire apply --mode rainbow --speed normal
voltaire apply --mode cycle --speed fast
voltaire apply --mode strobe --color white

voltaire brightness medium            # change brightness, keep the effect
voltaire off                          # all zones off
```

Colors are names (`voltaire apply --list-colors` prints them all with
swatches) or a six-digit hex value such as `ff8800`, without `#`.

To set one zone, add `--device` with the zone's name. On the Z13:

```sh
voltaire apply --color red --device keyboard
voltaire apply --color blue --device lightbar
voltaire off --device lightbar
```

The Ally has a single zone, so `--device` is not needed there. A wrong zone
name is refused with the list of zones your machine has.

`voltaire list` shows the lighting devices voltaire can see and whether each
one speaks the lighting protocol, which is useful if a zone does nothing.
`voltaire --dry-run apply …` shows what would be sent without changing
anything.

## Device notes

- **ROG Flow Z13:** the keyboard and the lightbar are separate devices. The
  keyboard goes dark when you detach it and comes back unlit; the daemon
  notices it return and restores your effect within a few seconds.
- **ROG Xbox Ally X:** the rings around the two sticks are one zone. If
  InputPlumber manages the controller, which it does on most handheld
  distributions, the lighting shows as unavailable until you run one setup
  command; see [the Ally's joystick lighting](/guide/buttons-and-controllers#ally-lighting).
- **OneXPlayer X2 Mini Pro:** the joystick rings can't be controlled yet,
  because of a gap in the kernel driver. The
  [device page](/devices/onexplayer-x2-mini-pro) explains.

## Things to know

- **Lighting comes back after sleep.** The hardware turns its lighting off
  when the machine sleeps (and voltaire turns the Z13's lightbar off, since it
  would otherwise stay lit). The daemon restores your lighting on resume,
  whichever profile is running. Without the daemon, run your `apply` command
  again.
- **`--color 000000` is not black.** An all-zero color tells the hardware to
  pick a color itself. To turn the lighting off, use `voltaire off`,
  `--brightness off` or the **Off** effect.
- **Use zone names, not device paths.** `--device` also takes a
  `/dev/hidrawN` path, but those numbers can change between boots, so a
  setting made that way is applied but not remembered.
