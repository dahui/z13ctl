package cmd

// lighting.go — the CLI's view of the device's lighting: its capabilities,
// the zone bytes and HID identities from device data, and the no-daemon path
// through the assembled driver. Nothing here restates a mode list, a speed
// list or a brightness range; all of it is the driver's Caps.

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/hid"
)

// lightingHW returns the assembled device's lighting driver, or an error when
// the device has none.
func lightingHW() (driver.Lighting, error) {
	hw, err := hardware()
	if err != nil {
		return nil, err
	}
	if hw.Lighting == nil {
		return nil, errors.New("this device has no lighting control")
	}
	return hw.Lighting, nil
}

// lightingCaps is the device's lighting capabilities; zero when it has none.
func lightingCaps() driver.LightingCaps {
	l, err := lightingHW()
	if err != nil {
		return driver.LightingCaps{}
	}
	return l.Caps()
}

// lightingZoneBytes is each zone's protocol byte, from device data, for the
// dry run's packet listing.
func lightingZoneBytes() []uint8 {
	if _, err := hardware(); err != nil || hwConfig.Lighting == nil {
		return nil
	}
	out := make([]uint8, 0, len(hwConfig.Lighting.Zones))
	for _, z := range hwConfig.Lighting.Zones {
		out = append(out, uint8(z.Zone))
	}
	return out
}

// lightingKnown is the zone table as hid's known-device list, for list.
func lightingKnown() []hid.Known {
	if _, err := hardware(); err != nil || hwConfig.Lighting == nil {
		return nil
	}
	var out []hid.Known
	for _, z := range hwConfig.Lighting.Zones {
		if v, p, err := z.USBIDs(); err == nil {
			out = append(out, hid.Known{Name: z.Name, Vendor: v, Product: p})
		}
	}
	return out
}

// withLighting opens the lighting driver, runs fn, and closes it: the
// no-daemon path, so the CLI writes through the same driver the daemon does.
func withLighting(fn func(driver.Lighting) error) error {
	l, err := lightingHW()
	if err != nil {
		return err
	}
	if err := l.Reopen(); err != nil {
		return err
	}
	if c, ok := l.(io.Closer); ok {
		defer c.Close() //nolint:errcheck // best-effort close of a hidraw handle
	}
	return fn(l)
}

// checkLightingZone refuses a --device that is neither a zone nor a hidraw
// path, naming the zones the device has.
func checkLightingZone(c driver.LightingCaps, zone string) error {
	if zone == "" || strings.HasPrefix(zone, "/") || c.HasZone(zone) {
		return nil
	}
	names := make([]string, len(c.Zones))
	for i, z := range c.Zones {
		names[i] = z.Name
	}
	return fmt.Errorf("--device %q is not a lighting zone on this device (zones: %s, or a /dev/hidrawN path)",
		zone, strings.Join(names, ", "))
}

// parseLightingBrightness parses a brightness name or number against the
// device's ceiling. The names are positions on its scale — off is 0, high
// the ceiling, low 1 and medium the middle — so they mean the same thing on
// any number of levels.
func parseLightingBrightness(s string, maxLevel int) (int, error) {
	if maxLevel <= 0 {
		return 0, errors.New("this device has no brightness control")
	}
	switch strings.ToLower(s) {
	case "off":
		return 0, nil
	case "low":
		return 1, nil
	case "medium", "med":
		return (maxLevel + 1) / 2, nil
	case "high", "max":
		return maxLevel, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > maxLevel {
		return 0, fmt.Errorf("brightness must be off/low/medium/high (or 0–%d), got %q", maxLevel, s)
	}
	return n, nil
}

// modeNamesText lists the device's modes for help and errors.
func modeNamesText(c driver.LightingCaps) string {
	names := make([]string, len(c.Modes))
	for i, m := range c.Modes {
		names[i] = m.Name
	}
	return strings.Join(names, "|")
}
