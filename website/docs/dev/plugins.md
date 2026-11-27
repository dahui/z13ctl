---
title: Plugins (planned)
description: The design for out-of-process drivers — discovery, the manifest, lifecycle, the socket protocol, permissions and safety. Not yet loaded by the daemon.
---

# Plugins (planned)

::: warning Planned: not yet loaded by the daemon
This page is the design for voltaire's external plugin tier, scheduled
alongside the first non-ASUS device. Nothing below is implemented: the daemon
does not discover, start or talk to plugins today. The one piece already on the
wire is each firmware toggle's `source` field in `device-get` (`"core"` today,
`"plugin:<id>"` once plugins exist). Write a [compiled-in driver](/dev/writing-a-driver)
for anything you need now. The design may still change before it ships.
:::

## Why plugins

Compiled-in drivers cover hardware whose kernel interface voltaire speaks.
Plugins are for the rest: a vendor tool that is the only way to reach a
controller, an out-of-tree kernel module with its own userspace protocol, or a
community-maintained driver that should not wait on a voltaire release. A
plugin is a separate program the daemon starts and talks to over a Unix socket.
To the rest of voltaire it looks like one more driver.

## Discovery

The daemon looks for one directory per plugin, each holding a `plugin.toml`:

- `$XDG_CONFIG_HOME/voltaire/plugins/<id>/plugin.toml` (installed by the user)
- `/usr/share/voltaire/plugins/<id>/plugin.toml` (installed by a package)

A user plugin with the same id replaces a packaged one.

## The manifest

```toml
# plugin.toml
id       = "example-fans"         # unique; matches the directory name
name     = "Example fan controller"
version  = "0.1.0"
protocol = 1                      # the plugin protocol major version

# How to start it. The daemon appends --socket <path>.
exec = ["/usr/lib/voltaire/plugins/example-fans/example-fans"]

# Which driver classes it implements.
provides = ["fans", "telemetry"]

# Toggles the plugin adds to the Settings page and the feature commands.
[[features]]
id          = "quiet_mode"
label       = "Quiet mode"
description = "Cap fan speed at 40%."
kind        = "bool"

# Files the plugin needs write access to, folded into `voltaire setup`.
[[perms]]
path = "/sys/class/hwmon/hwmon*/pwm1"
mode = "g+w"
```

A plugin that provides `power` must also declare `[power.envelope]` with the
same keys as a device file's `[power]` block (`tdp_min`, `tdp_max_safe`,
`tdp_max_forced`, `floor_curve`). Registration is refused without it, because
the safety engine has nothing to enforce otherwise.

## Registration

A plugin fills a capability one of two ways:

- **From a device file.** A capability block names it: `method = "plugin:example-fans"`.
  This is the normal case. The device file still owns identity, matching and
  every number.
- **From the manifest.** A capability the device file leaves out is filled by
  a plugin that `provides` it. This covers adding to a machine voltaire already
  supports without editing its file.

Inside the daemon a plugin-backed capability is an adaptor implementing the
same `internal/driver` interface as a compiled-in driver, so handlers, the
safety engine and clients cannot tell the difference. A plugin's `[[features]]`
appear in `device-get`'s `toggles` with `"source":"plugin:<id>"` and are driven
through the generic `feature` and `feature-get` commands.

## Lifecycle

1. The daemon, running as the user, starts each plugin with
   `--socket $XDG_RUNTIME_DIR/voltaire/plugins/<id>.sock` and dials it,
   checking with `SO_PEERCRED` that the peer is the same user.
2. A `hello` handshake echoes the manifest's id and protocol version; a
   mismatch stops the plugin.
3. A plugin that exits is restarted with capped backoff.
4. On shutdown the daemon sends `shutdown`, then `SIGTERM`. Before stopping a
   plugin that drives fans, the daemon hands the fans back to firmware control,
   as its own release paths do.

## Protocol

The same newline-delimited JSON as the daemon's own socket: one request line,
one response line, every response carrying `ok`. The daemon sends operations
and the plugin answers. The plugin sends nothing unprompted except
notifications.

```json
{"op":"hello","protocol":1}
{"ok":true,"id":"example-fans","protocol":1}

{"op":"fan-apply","points":[{"temp":40,"pwm":0},{"temp":80,"pwm":255}]}
{"ok":true}

{"op":"telemetry-sample"}
{"ok":true,"sample":{"temperature":61,"rpm":[3100]}}

{"op":"feature-set","id":"quiet_mode","value":1}
{"ok":false,"error":"quiet_mode: controller not responding"}
```

Operations mirror the driver interfaces (`fan-apply`, `fan-release`,
`fan-read-rpm`, `power-apply`, `feature-get`, `feature-set`,
`telemetry-sample`, …). Fields are add-only within a protocol major: ignore
what you do not understand, and never repurpose a field.

## Permissions

Plugins run as the user, like the daemon. Hardware access comes from the same
udev grants voltaire uses for itself: each manifest's `[[perms]]` entries are
folded into the grants `voltaire setup` derives for the machine
(`internal/perms`), and so into the rules file and the boot pass. voltaire never
escalates. A plugin that genuinely needs root ships its own privileged
component (a kernel module, a system service) and talks to it.

## Safety

A plugin power limiter sits below `safety.Engine` like any compiled-in driver:
the daemon writes the plugin's declared fan floor before any sustained limit
above its safe maximum, and refuses the limit if the floor does not take. A
plugin cannot reach a capability it did not register, and gets no access
through voltaire to another class's hardware.

## Skeleton

What a minimal fan plugin will look like, in Go. Any language that can read and
write lines on a Unix socket works.

```go
// A sketch against the planned protocol — not a working plugin today.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"net"
	"os"
)

type request struct {
	Op     string            `json:"op"`
	Points []json.RawMessage `json:"points,omitempty"`
}

func main() {
	sock := flag.String("socket", "", "socket the daemon dials")
	flag.Parse()
	os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	if err != nil {
		os.Exit(1)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go serve(conn)
	}
}

func serve(conn net.Conn) {
	defer conn.Close()
	in, out := bufio.NewScanner(conn), json.NewEncoder(conn)
	for in.Scan() {
		var req request
		if json.Unmarshal(in.Bytes(), &req) != nil {
			out.Encode(map[string]any{"ok": false, "error": "malformed request"})
			continue
		}
		switch req.Op {
		case "hello":
			out.Encode(map[string]any{"ok": true, "id": "example-fans", "protocol": 1})
		case "fan-apply":
			// Write the curve, read it back, and fail if it did not take:
			// the same verify-after-write rule compiled-in drivers follow.
			out.Encode(map[string]any{"ok": true})
		case "shutdown":
			out.Encode(map[string]any{"ok": true})
			return
		default:
			out.Encode(map[string]any{"ok": false, "error": "unknown op " + req.Op})
		}
	}
}
```
