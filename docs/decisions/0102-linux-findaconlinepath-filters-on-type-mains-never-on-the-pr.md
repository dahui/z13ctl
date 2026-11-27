# `linux.FindACOnlinePath` filters on `type == "Mains"`, never on the presence of an `online` file

**`linux.FindACOnlinePath` filters on `type == "Mains"`, never on the presence of
an `online` file.** On the Z13 the detachable keyboard registers as
`hid-*-battery-N` (type `Battery`) and the two USB-C ports as
`ucsi-source-psy-*` (type `USB`), and all of them expose `online` — a `*/online`
glob reports mains power whenever the cover is attached. `OnACPower` returns an
*error* when no Mains supply exists (VM, desktop, driver not yet bound); callers
must treat that as unknown and do nothing, never as "on battery". The one
exception is gated on a system battery: a machine with a battery and no Mains
supply is USB-C-powered, and a `System`-scope USB supply `online` answers
(`usbPowered`; 2026-10-09). The system battery is likewise chosen by the
kernel's own description — `type == Battery` and `scope != Device`
(`systemBatteryDir`) — rather than the `BAT*` name, which is the fallback only
when no supply describes itself.
