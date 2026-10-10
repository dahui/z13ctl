// Package aurahid implements driver.Lighting over the ASUS Aura HID protocol
// — the "aura-hid" method in device data. It owns the open hidraw handles and
// the parsing of api.LightingState fields into Aura packets; per the driver
// contract it starts no goroutines and takes no locks, so the daemon's device
// lock is what serializes Reopen against every other method.
package aurahid

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/aura"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/hid"
)

// hidFind and hidHas wrap the hid package's discovery entry points purely so
// tests can drive Reopen without real hidraw nodes.
var (
	hidFind = hid.FindDevice
	hidHas  = hid.HasDevice
)

// kbdBacklightMax is the LED class's own ceiling for the keyboard backlight,
// read only to cross-check the protocol's; a var so tests can redirect it.
var kbdBacklightMax = "/sys/class/leds/asus::kbd_backlight/max_brightness"

// Zone is one lighting zone: its wire name and label, the HID device that
// carries it, and the protocol's zone byte.
type Zone struct {
	Name, Label     string
	Vendor, Product uint16
	Byte            uint8
}

// Config is the device data the driver is built from. Modes and Speeds are
// optional subsets of the protocol's, in order; empty means all of it.
type Config struct {
	Zones  []Zone
	Modes  []string
	Speeds []string
}

// Lighting drives Aura RGB zones over hidraw. The zero value is unopened;
// Reopen acquires the device.
type Lighting struct {
	zones     []Zone
	known     []hid.Known
	zoneBytes []uint8
	caps      driver.LightingCaps
	dev       *hid.Device // nil until Reopen succeeds
}

// New returns an unopened lighting driver for c. Pure apart from a static
// LED-class read: the HID device is opened by the first Reopen, so assembly
// touches no hardware. A mode or speed the protocol cannot send is an error —
// the device data is wrong, and offering it would fail at every apply.
func New(c Config) (*Lighting, error) {
	l := &Lighting{zones: append([]Zone(nil), c.Zones...)}
	for _, z := range c.Zones {
		l.known = append(l.known, hid.Known{Name: z.Name, Vendor: z.Vendor, Product: z.Product})
		l.zoneBytes = append(l.zoneBytes, z.Byte)
		l.caps.Zones = append(l.caps.Zones, driver.LightingZone{Name: z.Name, Label: z.Label})
	}
	modes := c.Modes
	if len(modes) == 0 {
		for _, m := range aura.Modes {
			modes = append(modes, m.Name)
		}
	}
	for _, name := range modes {
		m, ok := aura.LookupMode(name)
		if !ok {
			return nil, fmt.Errorf("lighting mode %q is not one the Aura protocol can send", name)
		}
		l.caps.Modes = append(l.caps.Modes, driver.LightingMode{
			Name: m.Name, Label: titleCase(m.Name), Color: m.Color, Color2: m.Color2, Speed: m.Speed})
	}
	speeds := c.Speeds
	if len(speeds) == 0 {
		speeds = aura.SpeedNames
	}
	for _, name := range speeds {
		if _, err := aura.SpeedFromString(name); err != nil {
			return nil, fmt.Errorf("lighting speed: %w", err)
		}
	}
	l.caps.Speeds = append([]string(nil), speeds...)
	l.caps.BrightnessMax = aura.MaxBrightness
	// The protocol decides the levels; the kernel's LED class is a second
	// opinion worth a warning when it disagrees, since one of them is then
	// describing hardware the other is not.
	if data, err := os.ReadFile(kbdBacklightMax); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && n != aura.MaxBrightness {
			slog.Warn("keyboard backlight max_brightness differs from the Aura protocol's levels",
				"kernel", n, "protocol", aura.MaxBrightness)
		}
	}
	return l, nil
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Caps is the zones, modes, speeds and brightness ceiling, from the device
// data and the protocol.
func (l *Lighting) Caps() driver.LightingCaps {
	c := l.caps
	c.Zones = append([]driver.LightingZone(nil), l.caps.Zones...)
	c.Modes = append([]driver.LightingMode(nil), l.caps.Modes...)
	c.Speeds = append([]string(nil), l.caps.Speeds...)
	return c
}

// skippableError marks a condition callers treat as "nothing to write here"
// rather than a failure — no HID device open, or a zone whose hidraw node is
// not present (a detached keyboard). It wraps driver.ErrUnsupported so
// errors.Is can spot it without the sentinel's text polluting the message the
// socket protocol has always used for these cases.
type skippableError struct{ msg string }

func (e skippableError) Error() string { return e.msg }
func (e skippableError) Unwrap() error { return driver.ErrUnsupported }

// view resolves a zone name (or raw hidraw path) to the write target.
func (l *Lighting) view(zone string) (*hid.Device, error) {
	if l.dev == nil {
		return nil, skippableError{msg: "no HID device available"}
	}
	target, err := l.dev.FilteredView(zone)
	if err != nil {
		return nil, skippableError{msg: err.Error()}
	}
	return target, nil
}

// Zones returns the addressable zone names from device data.
func (l *Lighting) Zones() []string {
	out := make([]string, len(l.zones))
	for i, z := range l.zones {
		out[i] = z.Name
	}
	return out
}

// PresentZones returns the zones whose hidraw node exists in sysfs, in
// declaration order, without opening anything. The hotplug watcher reopens
// when a zone appears that was not there before — the Z13's keyboard cover
// returning — so no zone has to be declared removable, and a zone whose
// hardware never appears (a SKU without it) simply never triggers anything.
func (l *Lighting) PresentZones() []string {
	var out []string
	for _, z := range l.zones {
		if hidHas(z.Name, l.known) {
			out = append(out, z.Name)
		}
	}
	return out
}

// Reopen re-discovers and reopens the Aura HID device, replacing (and
// closing) any stale handle. Called under the daemon's device lock.
//
// A zone that is present in sysfs but missing from the opened set is a
// *failed* reopen, not a smaller device. On reattach the keyboard's sysfs
// entry appears before udev has applied permissions to the new /dev node, and
// FindDevice silently drops nodes it cannot open — so without this check a
// reattach-window Reopen returned a lightbar-only device as success, the
// restore skipped the keyboard as "not present", and the hotplug watcher
// latched and stopped retrying: the keyboard stayed dark until the next
// physical detach. The watcher's whole retry path rides on this error.
//
// On failure the old handle is kept, so the zones it does hold (the fixed
// lightbar) keep working through the retry window. A zone absent from sysfs
// is fine — that is a detached cover, and the watcher calls again when it
// returns.
func (l *Lighting) Reopen() error {
	dev, err := hidFind("", l.known)
	if err != nil {
		return err
	}
	for _, z := range l.zones {
		if !hidHas(z.Name, l.known) {
			continue
		}
		if _, err := dev.FilteredView(z.Name); err != nil {
			dev.Close()
			return fmt.Errorf("zone %s is present but its hidraw node did not open (udev permissions not applied yet?): %w", z.Name, err)
		}
	}
	if l.dev != nil {
		l.dev.Close()
	}
	l.dev = dev
	return nil
}

// Close releases the HID device. Deliberately not part of driver.Lighting —
// the daemon reaches it through an io.Closer assertion at shutdown.
func (l *Lighting) Close() error {
	if l.dev != nil {
		l.dev.Close()
		l.dev = nil
	}
	return nil
}

// Apply writes a lighting state to one zone; an empty zone means all. A
// disabled state turns the zone off. Mode, speed and colors are parsed here,
// so an unappliable state errors before any packet is written.
func (l *Lighting) Apply(zone string, ls api.LightingState) error {
	target, err := l.view(zone)
	if err != nil {
		return err
	}
	if !ls.Enabled {
		return aura.TurnOff(target)
	}
	if _, ok := l.caps.Mode(ls.Mode); !ok {
		return fmt.Errorf("unknown mode %q (valid: %s)", ls.Mode, strings.Join(l.modeNames(), " "))
	}
	mode, err := aura.ModeFromString(ls.Mode)
	if err != nil {
		return err
	}
	if !l.caps.HasSpeed(ls.Speed) {
		return fmt.Errorf("unknown speed %q (valid: %s)", ls.Speed, strings.Join(l.caps.Speeds, " "))
	}
	speed, err := aura.SpeedFromString(ls.Speed)
	if err != nil {
		return err
	}
	if ls.Brightness < 0 || ls.Brightness > l.caps.BrightnessMax {
		return fmt.Errorf("brightness %d out of range 0–%d", ls.Brightness, l.caps.BrightnessMax)
	}
	r, g, b, err := aura.ParseColor(ls.Color)
	if err != nil {
		return err
	}
	r2, g2, b2, err := aura.ParseColor(ls.Color2)
	if err != nil {
		return err
	}
	if err := aura.Apply(target, l.zoneBytes, mode, r, g, b, r2, g2, b2, speed, uint8(ls.Brightness)); err != nil {
		return errors.New("apply: " + err.Error())
	}
	return nil
}

// Off turns one zone off; an empty zone means all.
func (l *Lighting) Off(zone string) error {
	target, err := l.view(zone)
	if err != nil {
		return err
	}
	if err := aura.TurnOff(target); err != nil {
		return errors.New("off: " + err.Error())
	}
	return nil
}

// SetBrightness changes only the brightness of one zone, leaving the running
// effect untouched. Level 0 powers the zone down; any other level powers it
// up. The step labels on the errors are the ones the socket protocol has
// always carried for this operation.
func (l *Lighting) SetBrightness(zone string, level int) error {
	if level < 0 || level > l.caps.BrightnessMax {
		return fmt.Errorf("brightness %d out of range 0–%d", level, l.caps.BrightnessMax)
	}
	target, err := l.view(zone)
	if err != nil {
		return err
	}
	if err := aura.Init(target); err != nil {
		return errors.New("init: " + err.Error())
	}
	if err := aura.SetPower(target, level > 0); err != nil {
		return errors.New("setpower: " + err.Error())
	}
	if err := aura.SetBrightness(target, uint8(level)); err != nil {
		return errors.New("brightness: " + err.Error())
	}
	return nil
}

func (l *Lighting) modeNames() []string {
	out := make([]string, len(l.caps.Modes))
	for i, m := range l.caps.Modes {
		out[i] = m.Name
	}
	return out
}

// Known returns the zone table as hid's known-device list, for callers that
// enumerate hidraw nodes themselves (the CLI's list command).
func (l *Lighting) Known() []hid.Known { return append([]hid.Known(nil), l.known...) }
