package asusz13

// supply_test.go — the system battery and the charger kind come from the
// kernel's own power_supply descriptions (type, scope, online), not from names.

import (
	"os"
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

func TestSystemBatteryIsChosenByScopeNotName(t *testing.T) {
	f := newFakeSysfs(t)
	ps := f.root + "/power_supply"
	// A pack the ACPI convention would not have found, and the fake's BAT0
	// moved aside: only the type/scope rule can pick CMB0.
	if err := os.RemoveAll(f.battery); err != nil {
		t.Fatal(err)
	}
	f.writeFile(t, ps+"/CMB0/type", "Battery")
	f.writeFile(t, ps+"/CMB0/capacity", "61")
	if got := systemBatteryDir(); got != ps+"/CMB0" {
		t.Errorf("systemBatteryDir = %q, want CMB0 (the keyboard pack is scope=Device)", got)
	}

	// With only the keyboard's Device-scope pack there is no system battery.
	if err := os.RemoveAll(ps + "/CMB0"); err != nil {
		t.Fatal(err)
	}
	if got := systemBatteryDir(); got != "" {
		t.Errorf("systemBatteryDir = %q, want none: a peripheral's pack is not the system's", got)
	}
}

func TestChargerFromSupplies(t *testing.T) {
	f := newFakeSysfs(t)
	ucsi := f.root + "/power_supply/ucsi-source-psy-USBC000:001"

	// The fake's USB-C port is online (a PD contract): USB-C, whatever mains says.
	if got := ReadChargerFromSupplies(true, true); got != driver.ChargerUSBC {
		t.Errorf("port online = %q, want usb-c", got)
	}

	f.writeFile(t, ucsi+"/online", "0")
	for _, tt := range []struct {
		onAC, known bool
		want        driver.Charger
	}{
		{true, true, driver.ChargerAdapter},
		{false, true, driver.ChargerNone},
		{false, false, driver.ChargerUnknown},
	} {
		if got := ReadChargerFromSupplies(tt.onAC, tt.known); got != tt.want {
			t.Errorf("onAC=%v known=%v: %q, want %q", tt.onAC, tt.known, got, tt.want)
		}
	}

	// A USB supply of Device scope (a peripheral's charge port) is not an input.
	f.writeFile(t, ucsi+"/online", "1")
	f.writeFile(t, ucsi+"/scope", "Device")
	if got := ReadChargerFromSupplies(true, true); got != driver.ChargerAdapter {
		t.Errorf("device-scope USB online = %q, want adapter", got)
	}
}

// A USB-C-only machine has a battery and no Mains supply: its USB supplies say
// whether it is plugged in. Without a battery the answer stays unknown.
func TestOnACPowerUSBOnly(t *testing.T) {
	f := newFakeSysfs(t)
	if err := os.RemoveAll(f.ac); err != nil {
		t.Fatal(err)
	}
	ucsi := f.root + "/power_supply/ucsi-source-psy-USBC000:001"

	if on, err := OnACPower(); err != nil || !on {
		t.Errorf("USB-C online = %v, %v; want on AC", on, err)
	}
	f.writeFile(t, ucsi+"/online", "0")
	if on, err := OnACPower(); err != nil || on {
		t.Errorf("USB-C offline = %v, %v; want on battery", on, err)
	}
	if err := os.RemoveAll(f.battery); err != nil {
		t.Fatal(err)
	}
	if _, err := OnACPower(); err == nil {
		t.Error("no battery and no Mains: want unknown (an error), not a source")
	}
}
