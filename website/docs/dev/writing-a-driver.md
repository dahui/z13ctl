---
title: Writing a driver
description: Adding a driver for a new kernel interface — the contract, a worked example, registration, hermetic tests, and the hardware check.
---

# Writing a driver

You need a driver when a machine drives a capability through a kernel
interface voltaire does not speak yet. If an existing method already fits
(another ASUS laptop's `asus-nb-wmi` fans, another machine's `power_supply`
battery), you need only a [device file](/dev/device-files).

Read [Architecture](/dev/architecture) first for where drivers sit.
The contract below is also the package comment of `internal/driver`
(`doc.go`), which is the authoritative copy.

## The contract

- **One class, many machines.** Implement one of the interfaces in
  `internal/driver` (`FanController`, `PowerLimiter`, `ProfileController`,
  `Lighting`, `Toggles`, `Battery`, `Undervolter`, `CPUBoost`, `Telemetry`,
  `Buttons`). What differs between machines arrives as constructor arguments
  from the device file, not as a branch on the model.
- **Passive.** No goroutines, no locks held across calls, no policy. The daemon
  serialises hardware access; `internal/safety` holds every safety rule. A
  driver reports facts and performs the writes it is handed.
- **Absent, not failed.** A capability the machine lacks is a `nil` driver,
  never one that errors on every call. A method a driver cannot support returns
  `driver.ErrUnsupported`, which callers treat as "skip", not as a failure.
- **Writes verify.** Read back whatever can be read back, and return an error
  when the value did not take. Kernel interfaces that accept a write and ignore
  it are common (a fan curve dropped by a profile write, a boost switch
  amd-pstate refuses).
- **Know your expensive reads.** A read that is a firmware transaction (an
  ACPI method, an SMU message, waking a device) belongs behind the cheap
  question where the interface offers one (`Undervolter.Present` against
  `ProbeAvailable`, `Lighting.PresentZones` against `Reopen`).
- **Declared means read.** A capability the device file declares must be one
  the driver actually reads, or clients draw a chart flat at zero.
- **Precedence.** A value the kernel can report beats the device file's, which
  beats a built-in default.

## A worked example: `evdev-key`

`internal/drivers/evdevkey` is the smallest real driver and shows the whole
shape. It implements `driver.Buttons`:

```go
type Buttons interface {
	Watch(ctx context.Context, ch chan<- ButtonEvent) error
}
```

### 1. Parameters come from device data

The constructor takes what varies between machines and does nothing else:

```go
// New returns a watcher for the named input device and keycode. Pure: the
// device is located and opened only once Watch runs.
func New(deviceName string, keycode int, kind string) *Buttons {
	return &Buttons{deviceName: deviceName, keycode: evdev.EvCode(keycode), kind: kind}
}
```

Constructors are **pure**. `device.Assemble` runs at daemon start and in every
CLI invocation, and the test suite assembles real device files, so a
constructor that touches hardware would make all of that unsafe.

### 2. Every outside path is a variable

```go
// inputClassDir is the sysfs directory listing input event devices.
// Declared as a var so tests can redirect it; nothing else should assign to it.
var inputClassDir = "/sys/class/input"

// openEventDevice opens an evdev node. Indirected so tests can substitute a fake.
var openEventDevice = func(path string) (eventDevice, error) { return evdev.Open(path) }
```

Larger packages gather their sysfs roots in a `paths.go`. Opening devices,
running programs and mailbox I/O go through function variables the same way,
so a test can replace them.

### 3. Narrow seams can carry a safety rule

`eventDevice` is the subset of the evdev device the watcher needs, and it
deliberately has **no `Grab` method**. An exclusive grab on the Z13's button
node also takes the tablet-mode switch away from the desktop (issue #10), so
reintroducing it is a compile error rather than a code review catch.

### 4. Register the method

Factories are wired in `internal/drivers/register/register.go`, keyed by the
method name a device file uses:

```go
device.RegisterButtons("evdev-key", func(c device.ButtonConfig) (driver.Buttons, error) {
	return evdevkey.New(c.Device, c.Keycode, c.Kind), nil
})
```

This file is the only place drivers from different packages meet. If your
driver needs something another package owns (a reading, a path to watch), take
it as a constructor argument and pass it in here; do not import the other
driver package. The battery's charger and the profile controller's extra
policy paths are the existing examples.

A new capability *kind*, rather than a new method for an existing one, also
needs a block in `internal/device/config.go`, a field on `device.Device`, a
`Register*` function, and a section in the `device-get` document. Write the
block's keys into [Device files](/dev/device-files): a test fails
until every schema key is documented there.

### 5. Name the method by its interface

Name a method after the kernel interface it speaks (`platform-profile`,
`power-supply`, `aura-hid`), not after a machine. A second machine using the
same interface then reuses it as is.

## Testing without hardware

**Never let a test reach a real sysfs write.** The pattern every driver package
follows:

1. **`paths.go`.** Every sysfs root the package touches is a package variable.
2. **`main_test.go`.** A `TestMain` points every *writable* root at a path that
   does not exist before any test runs, so a test that forgets its fake fails
   with "no such file" instead of writing the developer's machine:

   ```go
   // internal/drivers/asus/main_test.go
   func TestMain(m *testing.M) {
   	const nowhere = "/nonexistent/voltaire-test-sysfs"
   	for _, v := range []*string{&sysHwmonDir, &sysFirmwareAttrDir, &pptBasePath} {
   		*v = nowhere + *v
   	}
   	os.Exit(m.Run())
   }
   ```

3. **`sysfs_fake_test.go`.** A `newFakeSysfs(t)` builds a temp-dir tree with the
   files the driver expects, swaps the variables to point into it, and restores
   them on cleanup. Interfaces plain files cannot imitate get a small emulator:
   the ryzen_smu mailbox's write-then-read-response protocol is `fakeSMU`.
4. **Exec seams.** Anything that runs a program (`powerprofilesctl`) goes
   through a function variable the fake replaces with a recorder.

Then test behaviour through the fake: the write lands in the right file, the
readback mismatch is an error, the missing attribute is `ErrUnsupported` or a
nil capability.

Run the **negative control** for any guard you add: break the thing it guards
and confirm the test fails. A guard that passes with the safeguard deleted
protects nothing, and several in this repository were caught that way.

## Testing on hardware

Unit tests cannot see firmware behaviour. The Z13's power-limit reset on a fan
release and its early wake from suspend were both invisible to them. A driver
that changes power, fan or sleep handling needs a run on the machine:

- Build with `make build-hwtest`. It points the SMU mailbox path at nowhere, so
  nothing can send a Curve Optimizer write, and checks the binary's symbol
  table to prove the override landed.
- Stop the installed daemon (`systemctl --user stop voltaire.service
  voltaire.socket`), back up `~/.local/state/voltaire/state.json`, and run
  `./voltaire-hwtest daemon --no-button` in the foreground with its log in a
  file.
- Measure power with RAPL under sustained load (`yes` ×32), never from a power
  limit's own readback, which does not move when firmware resets it.
- Restore the state file and restart the installed units afterwards.

The [Z13 hardware smoke test](/dev/smoke-z13) is the full checklist
for one machine; a new device wants its own.

## Checklist

- [ ] The driver implements one `internal/driver` interface; constructor
      parameters come from the device file.
- [ ] Constructors are pure; no goroutines; no locks held across calls.
- [ ] Every sysfs root is a variable in `paths.go`; exec and device opens go
      through function variables.
- [ ] `TestMain` points every writable root nowhere.
- [ ] Writes are read back; a mismatch is an error.
- [ ] Expensive reads sit behind the cheap question.
- [ ] The method is registered in `internal/drivers/register` and named after
      its kernel interface.
- [ ] Any new device-file key is documented in Device files (`make test` checks).
- [ ] Files the driver writes get a grant: a case for its method in
      `perms.For` (`internal/perms/grants.go`), and the device goldens
      regenerated and reviewed (`go test ./internal/perms -run TestGoldens -update`).
- [ ] `make test`, `make lint` and `make build-all` pass; power, fan and sleep
      changes have had a `make build-hwtest` run on the machine.
