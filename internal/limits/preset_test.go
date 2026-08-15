// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package limits

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

func eightPoints(pwm int) []api.FanCurvePoint {
	pts := make([]api.FanCurvePoint, CurvePoints)
	for i := range pts {
		pts[i] = api.FanCurvePoint{Temp: 30 + i*10, PWM: pwm}
	}
	return pts
}

// A preset whose length does not fit the editor's fixed-size Curve cannot be
// loaded at all, so it must not be offered. This is the drop rule that earns
// its keep — the others repair, this one cannot.
func TestSanitizedPresetsDropsWhatTheEditorCannotHold(t *testing.T) {
	l := Limits{Presets: []api.FanPreset{
		{Name: "ok", Label: "OK", Curve: eightPoints(100)},
		{Name: "short", Label: "Short", Curve: eightPoints(100)[:5]},
		{Name: "long", Label: "Long", Curve: append(eightPoints(100), api.FanCurvePoint{Temp: 115, PWM: 255})},
		{Name: "", Label: "Nameless", Curve: eightPoints(100)},
	}}.Sanitized()

	if len(l.Presets) != 1 || l.Presets[0].Name != "ok" {
		t.Fatalf("Sanitized kept %d presets (%+v), want only the well-formed one", len(l.Presets), l.Presets)
	}
}

// Ordering and range are the daemon's rules; a preset breaking them would be
// offered by the drawer and refused on send, which is exactly the state this
// package exists to prevent.
func TestSanitizedPresetsDropsCurvesTheDaemonWouldRefuse(t *testing.T) {
	descending := eightPoints(100)
	descending[4].PWM = 40

	sameTemp := eightPoints(100)
	sameTemp[3].Temp = sameTemp[2].Temp

	tooHigh := eightPoints(100)
	tooHigh[7].PWM = PWMMax + 1

	for _, tc := range []struct {
		name  string
		curve []api.FanCurvePoint
	}{
		{"pwm decreases", descending},
		{"temp repeats", sameTemp},
		{"pwm above ceiling", tooHigh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := Limits{Presets: []api.FanPreset{{Name: "bad", Label: "Bad", Curve: tc.curve}}}.Sanitized()
			if len(l.Presets) != 0 {
				t.Errorf("Sanitized kept a preset whose %s", tc.name)
			}
		})
	}
}

// Absence is a real answer here, unlike every other field Sanitized touches: a
// device that declares no presets has none, and filling in another machine's
// curves would offer a fan profile designed for hardware the user is not
// running. The contrast with StockProfilePPT — which *does* fall back — is the
// point of this test.
func TestSanitizedLeavesAbsentPresetsAbsent(t *testing.T) {
	l := Limits{}.Sanitized()
	if len(l.Presets) != 0 {
		t.Errorf("Sanitized invented %d presets for a device that declared none", len(l.Presets))
	}
	if len(l.StockProfilePPT) == 0 {
		t.Error("StockProfilePPT did not fall back; the contrast this test documents no longer holds")
	}
}

// A label is what a button says, so a preset that arrived without one still
// has to render as something. The daemon's device-data validation requires a
// label, so this only covers a client talking to something that does not.
func TestSanitizedPresetsFallBackToTheNameForALabel(t *testing.T) {
	l := Limits{Presets: []api.FanPreset{{Name: "turbo", Curve: eightPoints(200)}}}.Sanitized()
	if len(l.Presets) != 1 || l.Presets[0].Label != "turbo" {
		t.Errorf("got %+v, want the name used as the label", l.Presets)
	}
}

func TestPresetCurveResolvesCaseInsensitively(t *testing.T) {
	l := DefaultLimits()
	c, ok := l.PresetCurve("TURBO")
	if !ok {
		t.Fatal("PresetCurve did not resolve a known preset by a differently-cased name")
	}
	if c[0].PWM != 127 {
		t.Errorf("turbo curve starts at %d, want the device file's 127", c[0].PWM)
	}
	if _, ok := l.PresetCurve("nonesuch"); ok {
		t.Error("PresetCurve resolved a name no preset carries")
	}
}

// PresetCurve must hand back a copy. Curve is an array so this is true by
// construction today; the test is here because turning Curve into a slice
// would silently make the editor's repairs rewrite the device's own preset.
func TestPresetCurveDoesNotAliasTheDeviceData(t *testing.T) {
	l := DefaultLimits()
	c, ok := l.PresetCurve("quiet")
	if !ok {
		t.Fatal("quiet preset missing")
	}
	c[0].PWM = 200
	again, _ := l.PresetCurve("quiet")
	if again[0].PWM != 0 {
		t.Errorf("editing the returned curve changed the preset (now %d, want 0)", again[0].PWM)
	}
}

// PresetMatching requires exact equality on both axes. A curve one drag away
// from a preset is a curve the user drew, and labelling it with the preset's
// name would misreport what is about to be committed.
func TestPresetMatchingIsExact(t *testing.T) {
	l := DefaultLimits()
	turbo, _ := l.PresetCurve("turbo")

	if got := l.PresetMatching(turbo); got != "turbo" {
		t.Errorf("PresetMatching(turbo) = %q, want \"turbo\"", got)
	}

	nudged := turbo
	nudged[3].PWM++
	if got := l.PresetMatching(nudged); got != "" {
		t.Errorf("PresetMatching of a one-PWM-off curve = %q, want no match", got)
	}

	moved := turbo
	moved[3].Temp++
	if got := l.PresetMatching(moved); got != "" {
		t.Errorf("PresetMatching of a one-degree-off curve = %q, want no match", got)
	}

	if got := (Limits{}).PresetMatching(turbo); got != "" {
		t.Errorf("PresetMatching with no presets = %q, want no match", got)
	}
}

// The curve string a preset applies through is the same one --set takes, which
// is the whole "sugar for a curve" claim expressed at this layer.
func TestPresetCurveRendersTheWireFormat(t *testing.T) {
	l := DefaultLimits()
	c, _ := l.PresetCurve("balanced")
	if got, want := c.String(), "35:0,45:0,55:55,65:90,75:130,85:180,95:225,105:255"; got != want {
		t.Errorf("balanced renders as %q, want %q", got, want)
	}
}
