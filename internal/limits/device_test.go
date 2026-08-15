// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package limits

import (
	"reflect"
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

// z13Document is the device-get document the daemon serves for the only device
// supported today, written out by hand rather than derived from DefaultLimits
// so that a change to either side has to be made twice deliberately.
func z13Document() *api.DeviceInfo {
	return &api.DeviceInfo{
		ID:    "asus-rog-flow-z13-2025",
		Model: "GZ302",
		Fans: &api.FanInfo{
			Points: 8, TempMin: 35, TempMax: 105, PWMMax: 255,
			Presets: []api.FanPreset{
				{
					Name: "quiet", Label: "Quiet",
					Description: "Fans stopped until 60°C, then a late ramp. Quietest option; lets the package run hot.",
					Curve: []api.FanCurvePoint{
						{Temp: 35, PWM: 0}, {Temp: 50, PWM: 0}, {Temp: 60, PWM: 0}, {Temp: 70, PWM: 60},
						{Temp: 80, PWM: 110}, {Temp: 90, PWM: 170}, {Temp: 95, PWM: 215}, {Temp: 105, PWM: 255},
					},
				},
				{
					Name: "balanced", Label: "Balanced",
					Description: "Silent at idle, ramping from 55°C. A middle ground between Quiet and Turbo.",
					Curve: []api.FanCurvePoint{
						{Temp: 35, PWM: 0}, {Temp: 45, PWM: 0}, {Temp: 55, PWM: 55}, {Temp: 65, PWM: 90},
						{Temp: 75, PWM: 130}, {Temp: 85, PWM: 180}, {Temp: 95, PWM: 225}, {Temp: 105, PWM: 255},
					},
				},
				{
					Name: "turbo", Label: "Turbo",
					Description: "Fans always running, full speed by 85°C. Audible at idle, and the only preset ready for TDP above 75W.",
					Curve: []api.FanCurvePoint{
						{Temp: 35, PWM: 127}, {Temp: 45, PWM: 140}, {Temp: 55, PWM: 165}, {Temp: 65, PWM: 190},
						{Temp: 75, PWM: 235}, {Temp: 85, PWM: 255}, {Temp: 95, PWM: 255}, {Temp: 105, PWM: 255},
					},
				},
			},
		},
		Power: &api.PowerInfo{
			TDPMin: 5, TDPMaxSafe: 75, TDPMaxForced: 93,
			FloorCurve: []api.FanCurvePoint{
				{Temp: 30, PWM: 127}, {Temp: 40, PWM: 127}, {Temp: 50, PWM: 140},
				{Temp: 60, PWM: 165}, {Temp: 65, PWM: 190}, {Temp: 70, PWM: 215},
				{Temp: 75, PWM: 235}, {Temp: 80, PWM: 255},
			},
			StockProfilePPT: map[string]api.TDPState{
				"quiet":       {PL1SPL: 40, PL2SPPT: 55, FPPT: 55},
				"balanced":    {PL1SPL: 52, PL2SPPT: 71, FPPT: 70},
				"performance": {PL1SPL: 70, PL2SPPT: 86, FPPT: 86},
			},
		},
	}
}

// The drawer showed identical numbers before and after the swap, or the swap
// changed behaviour on the one machine anybody is running. This is the whole
// no-op-on-the-Z13 claim in one assertion.
func TestFromDeviceMatchesDefaultLimitsOnTheZ13(t *testing.T) {
	got := FromDevice(z13Document())
	if want := DefaultLimits(); !reflect.DeepEqual(got, want) {
		t.Errorf("FromDevice(z13) disagrees with DefaultLimits\n got: %+v\nwant: %+v", got, want)
	}
}

// A nil document is every failure mode at once: no daemon, a daemon too old for
// device-get, a malformed reply. All of them must land on a complete Limits.
func TestFromDeviceNilFallsBackWhole(t *testing.T) {
	if got, want := FromDevice(nil), DefaultLimits(); !reflect.DeepEqual(got, want) {
		t.Errorf("FromDevice(nil) = %+v, want DefaultLimits %+v", got, want)
	}
}

// A document with no sections at all is what a daemon for a device with no
// power or fan control would send. It must still be usable rather than a
// Limits full of zeros, since a zero TDPMaxSafe demands the force flag for
// every value the user can pick.
func TestFromDeviceEmptyDocumentIsStillUsable(t *testing.T) {
	got := FromDevice(&api.DeviceInfo{ID: "unknown", Model: "Mystery"})
	d := DefaultLimits()
	if got.Model != "Mystery" {
		t.Errorf("Model = %q, want the document's own %q", got.Model, "Mystery")
	}
	if got.TDPMaxSafe != d.TDPMaxSafe || got.TDPMin != d.TDPMin || got.TempMax != d.TempMax {
		t.Errorf("empty sections did not fall back: %+v", got)
	}
	if len(got.StockProfilePPT) == 0 {
		t.Error("StockProfilePPT is empty; IsStockPPT would call every limit user-chosen")
	}
}

// The point of the whole change: a second device's numbers must survive the
// conversion instead of being replaced by the Z13's.
func TestFromDeviceCarriesAnotherDevicesEnvelope(t *testing.T) {
	doc := &api.DeviceInfo{
		Model: "OXP-X2",
		Fans:  &api.FanInfo{Points: 8, TempMin: 40, TempMax: 90, PWMMax: 255},
		Power: &api.PowerInfo{
			TDPMin: 4, TDPMaxSafe: 30, TDPMaxForced: 40,
			StockProfilePPT: map[string]api.TDPState{"balanced": {PL1SPL: 15, PL2SPPT: 20, FPPT: 22}},
		},
	}
	got := FromDevice(doc)

	if got.TDPMin != 4 || got.TDPMaxSafe != 30 || got.TDPMaxForced != 40 {
		t.Errorf("power envelope not carried: %+v", got)
	}
	if got.TempMin != 40 || got.TempMax != 90 {
		t.Errorf("fan axis not carried: TempMin=%d TempMax=%d", got.TempMin, got.TempMax)
	}
	if len(got.FloorCurve) != 0 || got.HighTDPMinPWM != 0 {
		t.Errorf("device declares no floor but got one: curve=%v scalar=%d", got.FloorCurve, got.HighTDPMinPWM)
	}
	if len(got.StockProfilePPT) != 1 {
		t.Errorf("stock table not carried: %+v", got.StockProfilePPT)
	}
	// The derived rules must follow the device, not the Z13.
	if got.BasicSliderMax() >= DefaultLimits().BasicSliderMax() {
		t.Errorf("BasicSliderMax = %d, still reflects the Z13", got.BasicSliderMax())
	}
	if !got.ForceRequired(35) {
		t.Error("35W should require force on a device whose safe max is 30W")
	}
}

// A malformed floor must degrade to a flat floor at the curve's own bottom,
// never to no floor: the daemon still enforces one, so a drawer that dropped it
// would offer curves the daemon refuses — the exact failure this package exists
// to prevent.
func TestFromDeviceMalformedFloorKeepsAFloor(t *testing.T) {
	doc := &api.DeviceInfo{
		Model: "Broken",
		Power: &api.PowerInfo{
			TDPMin: 5, TDPMaxSafe: 75, TDPMaxForced: 93,
			// Temperatures not strictly increasing: sanitizedFloor rejects it whole.
			FloorCurve: []api.FanCurvePoint{{Temp: 30, PWM: 127}, {Temp: 30, PWM: 200}},
		},
	}
	got := FromDevice(doc)
	if len(got.FloorCurve) == 0 {
		t.Fatal("malformed floor was dropped entirely; the daemon still enforces one")
	}
	if got.HighTDPMinPWM != 127 {
		t.Errorf("HighTDPMinPWM = %d, want the declared bottom 127", got.HighTDPMinPWM)
	}
	if got.FanFloorPWM(90) != 127 {
		t.Errorf("FanFloorPWM above the safe max = %d, want the flat 127", got.FanFloorPWM(90))
	}
}

// The daemon's envelope carries five PPT rails; the drawer compares three.
// Keeping the other two would make a fetched Limits unequal to DefaultLimits on
// fields nothing reads, and the two have to stay interchangeable — the drawer
// picks between them purely on whether the daemon answered.
func TestFromDeviceNarrowsStockTableToTheComparedRails(t *testing.T) {
	doc := z13Document()
	for name, v := range doc.Power.StockProfilePPT {
		v.APUSPPT, v.PlatformSPPT = 70, 70
		doc.Power.StockProfilePPT[name] = v
	}

	got := FromDevice(doc)
	if b := got.StockProfilePPT["balanced"]; b.APUSPPT != 0 || b.PlatformSPPT != 0 {
		t.Errorf("balanced = %+v, want APU/Platform dropped", b)
	}
	if !reflect.DeepEqual(got, DefaultLimits()) {
		t.Errorf("extra rails leaked into the conversion:\n got: %+v\nwant: %+v", got, DefaultLimits())
	}
	// The rails that are compared must survive intact.
	if !got.IsStockPPT(api.TDPState{PL1SPL: 52, PL2SPPT: 71, FPPT: 70}) {
		t.Error("balanced's own values no longer read as stock")
	}
}

// The document is cached for the process lifetime, so the conversion must not
// hand the caller a view onto it.
func TestFromDeviceDoesNotAliasTheDocument(t *testing.T) {
	doc := z13Document()
	got := FromDevice(doc)

	doc.Power.FloorCurve[0].PWM = 9
	doc.Power.StockProfilePPT["balanced"] = api.TDPState{PL1SPL: 999}

	if got.FloorCurve[0].PWM != 127 {
		t.Errorf("FloorCurve aliases the document: got %d after mutating it", got.FloorCurve[0].PWM)
	}
	if got.StockProfilePPT["balanced"].PL1SPL != 52 {
		t.Errorf("StockProfilePPT aliases the document: got %d", got.StockProfilePPT["balanced"].PL1SPL)
	}
}
