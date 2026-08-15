// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/safety"
)

// TestZ13PresetsParseAsUserCurves is the guard behind the whole design: a
// preset is applied by handing its points to the ordinary fancurve set, so a
// preset the curve parser would reject is one the CLI would offer and the
// daemon would refuse.
//
// It runs each preset through cli.ParseFanCurve — the same validator a typed
// curve goes through — rather than re-checking the rules Validate already
// checked, because two copies of "what a legal curve is" is exactly how the
// preset path could come to accept something the write path does not.
func TestZ13PresetsParseAsUserCurves(t *testing.T) {
	c := z13Config(t)
	presets := c.Fans.Shape().Presets
	if len(presets) == 0 {
		t.Fatal("the Z13 declares no fan presets; this test and the [[fans.presets]] block disagree")
	}
	for _, p := range presets {
		if got, want := len(p.Curve), c.Fans.Points; got != want {
			t.Errorf("preset %q has %d points, want %d", p.Name, got, want)
		}
		if _, err := cli.ParseFanCurve(c.Fans.Shape(), api.FormatFanCurve(p.Curve)); err != nil {
			t.Errorf("preset %q is rejected by the curve parser: %v", p.Name, err)
		}
	}
}

// TestZ13PresetNamesAreDistinctFromProfileNames keeps two vocabularies apart.
// "quiet" and "balanced" are firmware *profile* names as well as preset names,
// which is fine — they are different commands — but a preset must never be
// reachable as a profile or the reverse, and nothing else would notice if a
// future preset were named to collide with a reserved word in some third
// namespace. What this actually pins is the harmless case: the overlap is
// deliberate, and the names still resolve independently.
func TestZ13PresetNamesAreDistinctFromProfileNames(t *testing.T) {
	c := z13Config(t)
	for _, p := range c.Fans.Shape().Presets {
		got, ok := c.Fans.FanPreset(p.Name)
		if !ok || got.Name != p.Name {
			t.Errorf("preset %q does not resolve by its own name", p.Name)
		}
		// Lookup folds case, as profile lookup does.
		if _, ok := c.Fans.FanPreset(strings.ToUpper(p.Name)); !ok {
			t.Errorf("preset %q does not resolve case-insensitively", p.Name)
		}
	}
	if _, ok := c.Fans.FanPreset("no-such-preset"); ok {
		t.Error("an unknown preset name resolved")
	}
}

// TestZ13TurboIsTheHighTDPReadyPreset pins the claim the device file makes in
// prose, in the terms the daemon actually enforces it: at a sustained limit
// above tdp_max_safe, the edit-time check accepts turbo unchanged and the fan
// floor leaves it exactly as drawn.
//
// The other two are expected to fail that check — they are below the floor at
// idle temperatures by design, since keeping the fans stopped is the point —
// so this asserts the difference rather than only the happy case. If a future
// edit made every preset clear the floor, the comment in the TOML would be
// wrong and this test says so.
func TestZ13TurboIsTheHighTDPReadyPreset(t *testing.T) {
	c := z13Config(t)
	env := c.Power.Envelope()
	high := env.TDPMaxForced // well above tdp_max_safe

	for _, p := range c.Fans.Shape().Presets {
		err := safety.CheckCurveAgainstTDP(env, p.Curve, high)
		adjusted := safety.FloorAdjustsCurve(env, high, p.Curve)
		switch p.Name {
		case "turbo":
			if err != nil {
				t.Errorf("turbo is refused at %dW sustained, but the device file calls it the high-TDP-ready preset: %v", high, err)
			}
			if adjusted {
				t.Errorf("turbo is altered by the fan floor at %dW; the device file promises it is applied as drawn", high)
			}
		default:
			if err == nil {
				t.Errorf("preset %q now clears the fan floor at %dW — the device file says only turbo does", p.Name, high)
			}
		}
	}
}

// TestZ13QuietPresetsKeepTheFansStopped is the one hardware fact these curves
// are actually constrained by, and the reason the fork's Quiet feature was
// deleted: the firmware's zero-RPM idle survives only while the curve commands
// 0 at idle temperatures. A "quiet" preset with a non-zero bottom would be
// louder at idle than doing nothing at all, which is the opposite of what its
// name promises.
func TestZ13QuietPresetsKeepTheFansStopped(t *testing.T) {
	c := z13Config(t)
	for _, name := range []string{"quiet", "balanced"} {
		p, ok := c.Fans.FanPreset(name)
		if !ok {
			t.Fatalf("preset %q is missing", name)
		}
		if p.Curve[0].PWM != 0 {
			t.Errorf("preset %q starts at PWM %d; a non-zero bottom defeats zero-RPM idle and makes it louder than firmware auto",
				name, p.Curve[0].PWM)
		}
	}
}

// TestPresetValidationRejectsMalformedData covers what Validate must catch, on
// the rule that device data is compiled in and so a bad preset is a build
// defect rather than a runtime error.
func TestPresetValidationRejectsMalformedData(t *testing.T) {
	base := func() Config {
		return Config{
			Device: Meta{ID: "x", Model: "X", Match: []Match{{Vendor: "v", ProductPrefix: "p"}}},
			Fans:   &FansConfig{Method: "m", Points: 4, TempMin: 30, TempMax: 90},
		}
	}
	good := [][]int{{30, 0}, {50, 40}, {70, 120}, {90, 255}}

	cases := []struct {
		name   string
		preset FanPresetConfig
		want   string
	}{
		{"no name", FanPresetConfig{Label: "L", Curve: good}, "needs both name and label"},
		{"no label", FanPresetConfig{Name: "n", Curve: good}, "needs both name and label"},
		{"wrong point count", FanPresetConfig{Name: "n", Label: "L", Curve: good[:3]}, "has 3 points"},
		{"not a pair", FanPresetConfig{Name: "n", Label: "L", Curve: [][]int{{30}, {50, 40}, {70, 120}, {90, 255}}}, "must be a [temp, pwm] pair"},
		{"pwm out of range", FanPresetConfig{Name: "n", Label: "L", Curve: [][]int{{30, 0}, {50, 40}, {70, 120}, {90, 300}}}, "PWM 300 outside"},
		{"temp not increasing", FanPresetConfig{Name: "n", Label: "L", Curve: [][]int{{30, 0}, {30, 40}, {70, 120}, {90, 255}}}, "must strictly increase"},
		{"pwm decreasing", FanPresetConfig{Name: "n", Label: "L", Curve: [][]int{{30, 0}, {50, 40}, {70, 20}, {90, 255}}}, "must not decrease"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			c.Fans.Presets = []FanPresetConfig{tc.preset}
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a %s preset", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate error = %q, want it to mention %q", err, tc.want)
			}
		})
	}

	// A duplicate name is its own case: lookup folds case, so two presets
	// differing only in case would leave one permanently unreachable.
	t.Run("duplicate name", func(t *testing.T) {
		c := base()
		c.Fans.Presets = []FanPresetConfig{
			{Name: "n", Label: "L", Curve: good},
			{Name: "N", Label: "L2", Curve: good},
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "duplicate name") {
			t.Errorf("Validate error = %v, want a duplicate-name rejection", err)
		}
	})

	// The happy path, so the rejections above are known to be about the defect
	// and not about the scaffolding.
	t.Run("well formed", func(t *testing.T) {
		c := base()
		c.Fans.Presets = []FanPresetConfig{{Name: "n", Label: "L", Curve: good}}
		if err := c.Validate(); err != nil {
			t.Errorf("Validate rejected a well-formed preset: %v", err)
		}
	})
}
