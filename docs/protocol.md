# Aura HID Protocol Reference

This document describes the HID protocol used by the 2025 ASUS ROG Flow Z13
for RGB lighting control. It is intended for developers who want to understand
or reimplement this functionality.

The protocol was reverse-engineered from
[g-helper](https://github.com/seerge/g-helper) (MIT license), specifically
`app/USB/AsusHid.cs` (device I/O) and `app/USB/Aura.cs` (packet construction).

---

## Devices

The 2025 ROG Flow Z13 exposes two separate HID devices for lighting:

| Device | USB Product ID | Role | Controls |
|--------|---------------|------|----------|
| Keyboard | `0b05:1a30` | `keyboard` | Key backlight |
| Lightbar | `0b05:18c6` | `lightbar` | Edge light strip |

Both devices use the same Aura protocol and accept the same packet format.
Commands are sent independently to each device; there is no broadcast shortcut
that addresses both at once. Each device responds only to the zone byte that
matches its own zone (keyboard = zone 0, lightbar = zone 1) and silently
ignores commands for the other zone.

---

## The hidraw Interface

Linux exposes raw HID devices at `/dev/hidraw0`, `/dev/hidraw1`, etc.
Writing to these files sends an output report directly to the device,
bypassing any input subsystem drivers.

**Access**: The files require read/write permission. By default this means
root. A udev rule can grant a specific group access without requiring
privilege escalation for each command:

```
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0b05", ATTRS{idProduct}=="18c6", \
    GROUP="users", MODE="0660"
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0b05", ATTRS{idProduct}=="1a30", \
    GROUP="users", MODE="0660"
```

**Write size**: The Aura protocol uses 64-byte output reports. Every write
must be exactly 64 bytes. Shorter packets must be zero-padded to fill the
buffer before writing.

**Report ID**: The first byte of every write is the HID report ID. For Aura,
this is always `0x5d`. The hidraw interface includes the report ID in the
write buffer (unlike some HID APIs that strip it). This means byte 0 of every
packet you write is `0x5d`, and the actual command byte is at offset 1.

---

## Discovering Devices

The kernel creates sysfs entries for every hidraw device under
`/sys/class/hidraw/`. Each device has an associated `uevent` file that
contains the HID_ID, which encodes the bus type, vendor, and product IDs
in a fixed format.

**Path pattern**: `/sys/class/hidraw/hidraw*/device/uevent`

**HID_ID format**: `HID_ID=BUSTYPE:VENDORID:PRODUCTID` (all hex, zero-padded to 8 digits)

Example for the lightbar:
```
HID_ID=0003:00000B05:000018C6
```

- Bus `0003` = USB HID
- Vendor `00000B05` = ASUS (0x0b05)
- Product `000018C6` = N-KEY device (lightbar, 0x18c6)

To convert a sysfs uevent path to the corresponding `/dev/hidrawN` path,
extract the `hidrawN` component from position 4 of the path split by `/`:

```
/sys/class/hidraw/hidraw3/device/uevent  ->  /dev/hidraw3
```

### Confirming Aura Support

Not every hidraw node with a matching USB ID actually supports the Aura
report. Multiple HID interfaces are present on each USB device, and only one
of them carries Report ID `0x5d`. You can confirm support by reading the HID
report descriptor via ioctl and scanning for the two-byte sequence
`[0x85, 0x5d]`:

- `0x85` is the HID short-form item tag for "Report ID"
- `0x5d` is the Aura report ID value

```c
// Get descriptor size
int size;
ioctl(fd, HIDIOCGRDESCSIZE, &size);  // ioctl 0x80044801

// Read descriptor
struct {
    uint32_t size;
    uint8_t  value[4096];
} desc;
desc.size = size;
ioctl(fd, HIDIOCGRDESC, &desc);      // ioctl 0x90044802

// Scan for Report ID 0x5d
for (int i = 0; i < size - 1; i++) {
    if (desc.value[i] == 0x85 && desc.value[i+1] == 0x5d) {
        // This device supports Aura report 0x5d
    }
}
```

ioctl numbers (Linux, 64-bit):

- `HIDIOCGRDESCSIZE` = `0x80044801`
- `HIDIOCGRDESC` = `0x90044802`

---

## Packet Format

All Aura packets follow this structure:

```
Offset  Len  Field
------  ---  -----
0       1    Report ID (always 0x5d)
1       1    Command byte
2+      N    Command-specific payload
(end)   ...  Zero padding to reach 64 bytes total
```

The full 64-byte buffer is always written to the hidraw device, regardless
of how many payload bytes the command actually uses. Any unused bytes must
be set to zero.

---

## Protocol Flow

A complete lighting update requires this sequence of packets:

```
Init()           -- 4 packets: wake device, identify, configure, activate lightbar
SetPower(on)     -- 1 packet:  enable power to all lighting zones
SetBrightness()  -- 1 packet:  set brightness level (0-3)
SetMode(zone 0)  -- 1 packet:  set color/mode for keyboard
Commit()         -- 2 packets: latch the zone 0 change
SetMode(zone 1)  -- 1 packet:  set color/mode for lightbar
Commit()         -- 2 packets: latch the zone 1 change
```

Total: 11 packets per full update. Init and SetPower/SetBrightness are sent
once per session. SetMode and Commit must be sent once for each zone you want
to update.

---

## Commands

### Init

Sent once before any other command to wake the device and activate the
lightbar. All four packets must be sent in order.

**Packet 1 — Wake**

```
Offset  Byte  Meaning
------  ----  -------
0       5d    Report ID
1       b9    Init command
2-63    00    Zero padding
```

**Packet 2 — ASUS Identification String**

```
Offset  Bytes                            Meaning
------  -----                            -------
0-14    5d 41 53 55 53 20 54 65          ASCII: ]ASUS Tech.Inc.
        63 68 2e 49 6e 63 2e
15-63   00                               Zero padding
```

The string `]ASUS Tech.Inc.` is sent as raw ASCII. The first character `]`
has ASCII value `0x5d`, which is also the report ID — so the first byte of
this packet doubles as both the `]` character and the report ID. Do not
prepend an additional `0x5d` byte.

**Packet 3 — Mode Config Header**

```
Offset  Byte  Meaning
------  ----  -------
0       5d    Report ID
1       05    Command
2       20    Config byte
3       31    Config byte
4       00    Config byte
5       1a    Config byte
6-63    00    Zero padding
```

**Packet 4 — Z13 Dynamic Lighting Init**

```
Offset  Byte  Meaning
------  ----  -------
0       5d    Report ID
1       c0    Command
2       03    Subcommand
3       01    Enable flag
4-63    00    Zero padding
```

This packet is required for the 2025 ROG Flow Z13 lightbar. Without it, the
lightbar will not respond to SetMode commands. It is safe to send to the
keyboard device as well; it is simply ignored there.

### SetPower

Enables or disables power to all lighting zones simultaneously.

```
Offset  Byte  Meaning
------  ----  -------
0       5d    Report ID
1       bd    Command
2       01    Subcommand
3       keyb  Keyboard power flags
4       bar   Lightbar power flags
5       lid   Lid power flags
6       rear  Rear power flags
7       ff    Terminator (always 0xff)
8-63    00    Zero padding
```

**Power-on values:**

| Field | Value | Notes |
|-------|-------|-------|
| keyb | `ff` | All power states enabled |
| bar | `1f` | Bits 0-4 set: Awake, Boot, Awake(dup), Sleep, Shutdown |
| lid | `ff` | All power states enabled |
| rear | `ff` | All power states enabled |

**Power-off values:** all four fields set to `00`. The terminator byte `ff`
at offset 7 is present regardless of power state.

The `bar` byte uses a bitmask to select which power states the lightbar is
active in:

| Bit | Power state |
|-----|-------------|
| 0 | Awake |
| 1 | Boot |
| 2 | Awake (duplicate) |
| 3 | Sleep |
| 4 | Shutdown |

`0x1f` (binary `00011111`) enables all five states.

### SetBrightness

Sets the keyboard backlight brightness level.

```
Offset  Byte   Meaning
------  ----   -------
0       5d     Report ID
1       ba     Command
2       c5     Config byte
3       c4     Config byte
4       level  Brightness level (0x00-0x03)
5-63    00     Zero padding
```

**Level values:**

| Level | Byte | Meaning |
|-------|------|---------|
| Off | `00` | Backlight disabled |
| Low | `01` | Minimum brightness |
| Medium | `02` | Mid brightness |
| High | `03` | Maximum brightness |

Values above `0x03` should be clamped to `0x03` before sending.

### SetMode

Sets the color and animation mode for a single zone. Must always be followed
by a [Commit](#commit) sequence.

```
Offset  Byte      Meaning
------  ----      -------
0       5d        Report ID
1       b3        Command
2       zone      Zone number (00 = keyboard, 01 = lightbar)
3       mode      Animation mode
4       r         Primary color, red channel (0x00-0xff)
5       g         Primary color, green channel
6       b         Primary color, blue channel
7       speed     Animation speed
8       dir       Direction (0x00 = default)
9       randFlag  Color selection flag
10      r2        Secondary color, red channel (breathe mode only)
11      g2        Secondary color, green channel
12      b2        Secondary color, blue channel
13-63   00        Zero padding
```

**Mode values:**

| Name | Byte | Description |
|------|------|-------------|
| `static` | `00` | Solid color, no animation |
| `breathe` | `01` | Pulse between two colors |
| `cycle` | `02` | Cycle through spectrum |
| `rainbow` | `03` | Rainbow wave |
| *(star)* | `04` | Random pixels blink — not supported on Z13 2025 |
| *(rain)* | `05` | Dripping color — not supported on Z13 2025 |
| `strobe` | `0a` | Rapid flash |
| *(comet)* | `0b` | Trailing comet — not supported on Z13 2025 |
| *(flash)* | `0c` | Flash burst — not supported on Z13 2025 |

**Speed values:**

| Name | Byte |
|------|------|
| Slow | `e1` |
| Normal | `eb` |
| Fast | `f5` |

**randFlag values and when to use them:**

| Value | Meaning | When to use |
|-------|---------|-------------|
| `00` | Use primary color (r, g, b) | Any mode with a non-zero color |
| `01` | Dual-color mode | `breathe` with a non-zero primary color |
| `ff` | Device selects color | When r, g, and b are all zero |

The logic for choosing randFlag:

```
if r == 0 and g == 0 and b == 0:
    randFlag = 0xff   # all-zero color signals "random"
elif mode == breathe:
    randFlag = 0x01   # enables use of the r2/g2/b2 secondary color
else:
    randFlag = 0x00   # use r/g/b directly
```

For modes other than `breathe`, the r2/g2/b2 fields are unused and should
be set to zero.

### Commit

Two packets that must be sent after every SetMode to latch the pending state.
Without them, the mode change is accepted by the device but not applied to the
lighting output.

**MESSAGE_SET** (send first):

```
Offset  Byte  Meaning
------  ----  -------
0       5d    Report ID
1       b5    MESSAGE_SET command
2-63    00    Zero padding
```

**MESSAGE_APPLY** (send second):

```
Offset  Byte  Meaning
------  ----  -------
0       5d    Report ID
1       b4    MESSAGE_APPLY command
2-63    00    Zero padding
```

Always send MESSAGE_SET (`b5`) before MESSAGE_APPLY (`b4`). The order matters.

---

## Zones

The Z13 has two lighting zones that must be addressed separately:

| Zone | Byte | USB Product | Physical location |
|------|------|-------------|-------------------|
| Keyboard | `00` | `0b05:1a30` | Key backlight |
| Lightbar | `01` | `0b05:18c6` | Edge light strip |

Each physical USB device responds only to its own zone byte. If you send a
SetMode with zone `00` (keyboard) to the lightbar hidraw node, the packet
is silently ignored. The same applies in reverse.

In practice, you can send all packets to both devices and let each device
filter by zone — this matches how g-helper operates. Alternatively, look
up each device separately and send each zone's packets only to the matching
device.

---

## Annotated Packet Examples

### Static red on both zones, full brightness

The complete packet sequence for setting both the keyboard and lightbar to
solid red at maximum brightness:

```
// Init packet 1: wake device
5d b9 00 00 00 00 00 00 00 00 00 00 00 00 00 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00

// Init packet 2: ASUS identification string "]ASUS Tech.Inc."
5d 41 53 55 53 20 54 65 63 68 2e 49 6e 63 2e 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00

// Init packet 3: mode config header
5d 05 20 31 00 1a 00 00 00 00 00 00 00 00 00 00
00 ... (zero-padded to 64 bytes)

// Init packet 4: Z13 dynamic lighting (required for lightbar)
5d c0 03 01 00 00 00 00 00 00 00 00 00 00 00 00
00 ... (zero-padded to 64 bytes)

// SetPower: all zones on
// [id] [cmd] [sub] [keyb] [bar] [lid] [rear] [term]
5d     bd     01    ff     1f    ff    ff     ff    00 ...

// SetBrightness: level 3 (high)
// [id] [cmd] [c5] [c4] [level]
5d     ba     c5   c4   03     00 ...

// SetMode: zone 0 (keyboard), static, red (#FF0000), normal speed
// [id] [cmd] [zone] [mode] [r ] [g ] [b ] [spd] [dir] [flag] [r2] [g2] [b2]
5d     b3     00     00     ff   00   00   eb    00    00     00   00   00  00 ...

// MESSAGE_SET (latch zone 0)
5d b5 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 ... (64 bytes)

// MESSAGE_APPLY (apply zone 0)
5d b4 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 ... (64 bytes)

// SetMode: zone 1 (lightbar), static, red (#FF0000), normal speed
// [id] [cmd] [zone] [mode] [r ] [g ] [b ] [spd] [dir] [flag] [r2] [g2] [b2]
5d     b3     01     00     ff   00   00   eb    00    00     00   00   00  00 ...

// MESSAGE_SET (latch zone 1)
5d b5 00 00 ... (64 bytes)

// MESSAGE_APPLY (apply zone 1)
5d b4 00 00 ... (64 bytes)
```

### Breathe between cyan and blue, slow speed

```
// SetMode: zone 0, breathe, cyan (#00FFFF) primary, blue (#0000FF) secondary
// [id] [cmd] [zone] [mode] [r ] [g ] [b ] [spd] [dir] [flag] [r2] [g2] [b2]
5d     b3     00     01     00   ff   ff   e1    00    01     00   00   ff  00 ...
//                   ^breathe    ^---cyan---  ^slow       ^dual    ^---blue---

// randFlag = 0x01 because mode is breathe and primary color is non-zero.
// The device animates between the two colors.
```

### Cycle mode with device-chosen colors

```
// SetMode: zone 0, cycle, black primary (device picks color), normal speed
// [id] [cmd] [zone] [mode] [r ] [g ] [b ] [spd] [dir] [flag] [r2] [g2] [b2]
5d     b3     00     02     00   00   00   eb    00    ff     00   00   00  00 ...
//                   ^cycle      ^all zero   ^normal   ^random

// randFlag = 0xff because r, g, and b are all 0x00.
// The device cycles through its own color sequence.
```
