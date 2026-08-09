// Package aurahid implements driver.Lighting over the ASUS Aura HID protocol
// — the "aura-hid" method in device data. It owns the open hidraw handles and
// the parsing of api.LightingState fields into Aura packets; per the driver
// contract it starts no goroutines and takes no locks, so the daemon's device
// lock is what serializes Reopen against every other method.
package aurahid

import (
	"errors"
	"fmt"

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

// Lighting drives Aura RGB zones over hidraw. The zero value is unopened;
// Reopen acquires the device.
type Lighting struct {
	zones []string
	dev   *hid.Device // nil until Reopen succeeds
}

// New returns an unopened lighting driver for the given zones. Pure: the HID
// device is opened by the first Reopen, so assembly touches no hardware.
func New(zones []string) *Lighting {
	z := make([]string, len(zones))
	copy(z, zones)
	return &Lighting{zones: z}
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
	copy(out, l.zones)
	return out
}

// Present reports whether the detachable keyboard's hidraw node exists in
// sysfs, without opening anything. The keyboard is the hot-pluggable part of
// the Z13's lighting — the lightbar is fixed — so its presence is what the
// hotplug watcher polls for.
func (l *Lighting) Present() bool { return hid.HasDevice("keyboard") }

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
	dev, err := hidFind("")
	if err != nil {
		return err
	}
	for _, zone := range l.zones {
		if !hidHas(zone) {
			continue
		}
		if _, err := dev.FilteredView(zone); err != nil {
			dev.Close()
			return fmt.Errorf("zone %s is present but its hidraw node did not open (udev permissions not applied yet?): %w", zone, err)
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
	mode, err := aura.ModeFromString(ls.Mode)
	if err != nil {
		return err
	}
	speed, err := aura.SpeedFromString(ls.Speed)
	if err != nil {
		return err
	}
	r, g, b, err := aura.ParseColor(ls.Color)
	if err != nil {
		return err
	}
	r2, g2, b2, err := aura.ParseColor(ls.Color2)
	if err != nil {
		return err
	}
	if err := aura.Apply(target, mode, r, g, b, r2, g2, b2, speed, uint8(ls.Brightness)); err != nil {
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
