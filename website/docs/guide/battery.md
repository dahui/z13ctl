---
title: Battery
description: The charge limit, a one-time full charge, and reading the battery's state.
---

# Battery

A lithium battery ages fastest when it sits full and warm. If your machine
spends most of its time plugged in, a **charge limit** stops charging below
100 % and can add years to the battery's life. When you need the whole battery
for a day, **full charge once** lifts the limit until the battery is full and
then puts it back.

## Charge limit {#charge-limit}

The charge limit is the level at which the battery stops charging. Around
80 % is a common choice for a machine that is mostly plugged in. Setting the
top of the range (100 %) removes the limit.

| Device | Charge limit |
|---|---|
| [ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | 40–100 % |
| [ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | 40–100 %, on the desktop only (Steam sets it in Gaming Mode) |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | Not available: the kernel's limit does not hold on this board yet |

Once set, the limit is enforced by the hardware whether or not voltaire is
running. The daemon remembers it and sets it again each time it starts, so it
survives a reboot.

### In the GUI

Drag the slider in the drawer's **Battery limit** section, or the **Charge
limit** row in the full window Dashboard's POWER card. The value is shown
beside the slider.

### On the command line

```sh
voltaire batterylimit --get
voltaire batterylimit --set 80
voltaire batterylimit --set 100       # no limit
```

A value outside your device's range is refused with the range it accepts.

## Full charge once {#full-charge-once}

Full charge once charges the battery to 100 % this one time, then returns to
your charge limit. Use it before a trip, then forget about it: the limit comes
back on its own when the battery is full.

- Unplugging part-way doesn't end it. The next time you plug in, the charge
  carries on to full.
- Moving the charge-limit slider, or setting a limit with `batterylimit --set`,
  cancels it and puts the limit back at once.
- It needs a limit to lift: with no limit set, or with the battery already
  full, it is refused.
- It needs the daemon, which watches for the battery to fill.

### In the GUI

On the full window's **Dashboard**, the **Full charge** switch sits under the
charge-limit slider in the POWER card; a note beside it says what it is doing.
The charge-limit slider keeps showing your limit, which is where the battery
will return.

The drawer can show it too, as the **Full charge once** section. It is off by
default; turn it on in Settings → Quickbar (see
[Choosing drawer sections](/guide/gui-configuration#drawer-sections)).

### On the command line

```sh
voltaire batterylimit --full-once           # start
voltaire batterylimit --full-once=false     # cancel and restore the limit now
```

While it runs, `batterylimit --get` prints the maximum, which is what the
hardware is holding, and notes the limit it will return to.

## Reading the battery's state

The battery card on the full window's **Dashboard** says what the battery is
doing in its header, and charts the charge level over time.

| Header | Meaning |
|---|---|
| `64% · 28.0 W · 30 m to limit` | Charging, with the rate and the time to your limit (or "to full" with no limit) |
| `81% · 12.3 W · 3 h 5 m to empty` | On battery, with the rate and the time left |
| `80% · AC · not charging` | Plugged in and held at the limit. This is normal with a charge limit. |
| `100% · AC · full` | Plugged in and full |

On a machine that can tell its power inputs apart, such as the Z13, the
header also says whether you are on **USB-C** or the **Adapter**. The time
estimate follows the current load, as every battery indicator does, and is
left out when the rate is too low to give an honest one.

On the command line, `voltaire status` shows the charge level and the limit:

```
Battery: 74% (limit: 80%)
```

See [Monitoring](/guide/monitoring) for the Dashboard's other charts.

## Device notes

- **ROG Flow Z13:** charges over USB-C or the adapter; the header names the
  one in use.
- **ROG Xbox Ally X:** in Steam Gaming Mode, Steam sets the charge limit, so
  voltaire hides its control there and the command line refuses with the
  reason. Set the limit from the desktop. See
  [What Steam controls](/guide/gaming-mode#steam-controls).
- **OneXPlayer X2 Mini Pro:** no charge limit until a kernel fix ships; the
  [device page](/devices/onexplayer-x2-mini-pro) explains. The battery's level,
  rate and time estimate work.

## Things to know

- A battery held at its limit on mains moves no energy, so the rate reads
  zero. That is a real reading, which is why the header says
  "not charging" instead of showing 0 W.
