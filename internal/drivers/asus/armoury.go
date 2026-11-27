package asus

// armoury.go — asus-armoury firmware attributes read and written outside the
// toggle allowlist: the boot-sound and panel-overdrive helpers the compat
// commands and dry run use, and charge_mode, the charger kind only the firmware
// can name.

import (
	"github.com/dahui/voltaire/v2/internal/driver"
)

// ReadCharger reports which power input is supplying the machine, or
// driver.ChargerUnknown when the firmware does not say.
//
// The Z13 takes power two ways — the proprietary high-wattage DC adapter shared
// with the Zephyrus line, and USB-C Power Delivery — and the *Mains* supply
// reads online for both. That is correct (mains power is attached either way)
// and is exactly why OnACPower cannot answer this question: asus-armoury's
// charge_mode is the only thing on the machine that distinguishes them.
//
// The vocabulary was established by observation rather than inference, with the
// charger physically swapped: on the DC adapter the attribute reads 1 with both
// ucsi-source-psy ports offline; on USB-C it reads 2 with a ucsi port online and
// its usb_type showing PD active. 0 is documented by possible_values and is the
// only remaining case, but has not been observed — a machine with no charger
// attached is on battery, where nobody was watching this attribute.
func ReadCharger() driver.Charger {
	v, err := readIntFile(sysFirmwareAttrDir + "/charge_mode/current_value")
	if err != nil {
		return driver.ChargerUnknown
	}
	switch v {
	case 0:
		return driver.ChargerNone
	case 1:
		return driver.ChargerAdapter
	case 2:
		return driver.ChargerUSBC
	}
	// A value this build does not know is unknown, not a guess. Firmware may
	// grow a kind before voltaire does, and naming it wrongly is worse than
	// saying nothing.
	return driver.ChargerUnknown
}

// ChargerFromArmoury is ReadCharger in the battery driver's ChargerFunc shape:
// asus-armoury's charge_mode answers on its own, so what power_supply said
// about mains is not needed.
func ChargerFromArmoury(_, _ bool) driver.Charger { return ReadCharger() }
