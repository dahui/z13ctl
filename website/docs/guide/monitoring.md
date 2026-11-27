---
title: Monitoring
description: Temperatures, fans, power, battery, load and more, live and over the last hour, in the GUI and on the command line.
---

# Monitoring

The voltaire daemon reads your machine's sensors once a second and keeps the
last hour of readings. You can watch them in the full window's Dashboard, at a
glance in the drawer's header, or in a terminal with `voltaire status`.

## In the GUI

### The drawer header

The drawer's header always shows the power source (Battery, AC, or the input
in use on a machine that can tell its chargers apart, such as USB-C), the CPU
temperature and the fan speed.

### The Dashboard

Open the [full window](/guide/gui#full-window) and the **Dashboard** tab
shows one card per kind of reading, each with a live value and a history
chart:

| Card | Shows |
|------|-------|
| **Temp** | CPU and GPU temperature |
| **Fan** | Each fan's speed, in RPM |
| **Power** | CPU package, GPU and NPU power draw, in watts |
| **Battery** | State of charge, in percent |
| **Load** | CPU, GPU and NPU utilization |
| **Clocks** | Average CPU core clock, GPU clock and memory clock |
| **Memory** | Memory in use, and the GPU's dedicated memory (VRAM) |
| **Net** | Network throughput, down and up |

A reading your hardware does not provide gets no card or line at all, rather
than a line stuck at zero. Lines on one card share a scale, so their heights
compare directly.

The **battery card's header** says what the battery is doing. While it charges
or discharges, it shows the charge level, the rate in watts and an estimate,
for example `64% · 28.0 W · 30 m to limit` or `81% · 12.3 W · 3 h 5 m to
empty`. While charging with a charge limit set, the estimate is to the limit,
since that is where charging stops. At rest on the charger above the limit it
reads `AC · not charging`, which is normal.

### History windows

The buttons on the Dashboard's heading pick how far back the charts look:
**1m**, **5m**, **15m**, **30m** or **1h**. The live values beside each card's
name update every second whatever the window.

Things to know when reading the charts:

- **Gaps are real.** A suspend, or a reading the daemon could not take, leaves
  a break in the line instead of a straight ramp across it.
- **The history starts when the daemon starts.** After a restart or reboot an
  hour-long chart fills in from the right as the machine runs.
- **The scale stays put** while values move within the normal range, and grows
  only when a reading goes outside it.
- **The NPU reads 0 W and 0%** while it is asleep, which is most of the time
  unless something is running on it. voltaire never wakes it to ask.
- **Network throughput** counts physical interfaces (Wi-Fi, Ethernet), not VPN
  tunnels or bridges, so traffic is not counted twice.

## On the command line

`voltaire status` prints a one-off summary: CPU temperature, fans, profile,
power source, power limits, undervolt and battery.

```sh
voltaire status
```

Add `--watch` to keep it updating in place until you press Ctrl-C, and
`--interval` to change how often (the default is one second):

```sh
voltaire status --watch
voltaire status --watch --interval 5s
```

`status` works without the daemon, reading the hardware directly. See
[`status`](/reference/commands#status) for every line it prints.

## Device notes

| Device | Notes |
|--------|-------|
| [ASUS ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | Two fans, shown as Fan 1 and Fan 2 |
| [ASUS ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | Two fans, shown as Fan 1 and Fan 2 |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | One fan, read only: voltaire shows its speed but leaves it on the firmware's control. No NPU readings while the kernel runs with the parameters suspend needs on this machine |

## Things to know

- **The Power card needs setup.** The CPU package power counter is readable
  only after `sudo voltaire setup` has granted access to it (read-only). If the
  card is missing its package line, run setup again.
- **If the Dashboard says the daemon does not serve telemetry history**, the
  daemon is older than the GUI. Restart it after upgrading:
  `systemctl --user restart voltaire`.
