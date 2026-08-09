// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package power

import (
	"testing"

	"github.com/dahui/z13ctl/api"
)

// otherDevice is a fictional second device with a deliberately different
// envelope: a much lower power ceiling, a narrower fan temperature range, no fan
// floor at all, and its own stock profile table.
//
// Every rule is exercised against both it and the Z13 so that nothing quietly
// depends on the Z13's numbers. That is the regression the multi-device port
// would otherwise walk into.
func otherDevice() Limits {
	return Limits{
		Model:         "FICTIONAL-1",
		TDPMin:        4,
		TDPMaxSafe:    30,
		TDPMaxForced:  45,
		HighTDPMinPWM: 0, // this device has no high-TDP fan floor
		TempMin:       40,
		TempMax:       90,
		StockProfilePPT: map[string]api.TDPState{
			"quiet":       {PL1SPL: 10, PL2SPPT: 15, FPPT: 15},
			"balanced":    {PL1SPL: 20, PL2SPPT: 28, FPPT: 26},
			"performance": {PL1SPL: 30, PL2SPPT: 40, FPPT: 40},
		},
	}
}

func TestDefaultLimitsAreSelfConsistent(t *testing.T) {
	for _, l := range []Limits{DefaultLimits(), otherDevice()} {
		if l.TDPMin >= l.TDPMaxSafe {
			t.Errorf("%s: TDPMin %d not below TDPMaxSafe %d", l.Model, l.TDPMin, l.TDPMaxSafe)
		}
		if l.TDPMaxSafe > l.TDPMaxForced {
			t.Errorf("%s: TDPMaxSafe %d above TDPMaxForced %d", l.Model, l.TDPMaxSafe, l.TDPMaxForced)
		}
		if l.TempMin >= l.TempMax {
			t.Errorf("%s: TempMin %d not below TempMax %d", l.Model, l.TempMin, l.TempMax)
		}
		// A curve needs one degree per point.
		if got := l.TempMax - l.TempMin; got < CurvePoints {
			t.Errorf("%s: temperature range %d too narrow for %d points", l.Model, got, CurvePoints)
		}
		if b := l.BasicSliderMax(); b <= l.TDPMin || b > l.TDPMaxSafe {
			t.Errorf("%s: BasicSliderMax %d outside (%d, %d]", l.Model, b, l.TDPMin, l.TDPMaxSafe)
		}
	}
}

func TestBasicSliderMaxIsDerivedNotFixed(t *testing.T) {
	// The Z13's historic hardcoded value, preserved by the derivation.
	if got := DefaultLimits().BasicSliderMax(); got != 70 {
		t.Errorf("Z13 BasicSliderMax = %d, want 70", got)
	}
	if got := otherDevice().BasicSliderMax(); got != 25 {
		t.Errorf("fictional BasicSliderMax = %d, want 25", got)
	}
	// A device whose safe max leaves no headroom must not produce an inverted
	// or below-minimum range.
	tiny := Limits{TDPMin: 5, TDPMaxSafe: 6, TDPMaxForced: 10}
	if got := tiny.BasicSliderMax(); got < tiny.TDPMin {
		t.Errorf("tiny BasicSliderMax = %d, below TDPMin %d", got, tiny.TDPMin)
	}
}

func TestSanitizedFillsUnsetFields(t *testing.T) {
	d := DefaultLimits()
	got := Limits{}.Sanitized()

	if got.TDPMin != d.TDPMin || got.TDPMaxSafe != d.TDPMaxSafe || got.TDPMaxForced != d.TDPMaxForced {
		t.Errorf("zero Limits did not inherit TDP defaults: %+v", got)
	}
	if got.TempMin != d.TempMin || got.TempMax != d.TempMax {
		t.Errorf("zero Limits did not inherit temperature defaults: %+v", got)
	}
	if len(got.StockProfilePPT) != len(d.StockProfilePPT) {
		t.Errorf("zero Limits did not inherit the stock profile table")
	}
	if got.Model == "" {
		t.Error("zero Limits did not inherit a model name")
	}
}

func TestSanitizedKeepsProvidedValues(t *testing.T) {
	o := otherDevice()
	got := o.Sanitized()

	if got.TDPMaxSafe != o.TDPMaxSafe || got.TempMax != o.TempMax || got.Model != o.Model {
		t.Errorf("Sanitized overwrote provided values: %+v", got)
	}
	// Zero is a legitimate HighTDPMinPWM — a device with no fan floor. It must
	// not be "helpfully" replaced with the Z13's 204, which would clamp fan
	// curves on hardware that has no such rule.
	if got.HighTDPMinPWM != 0 {
		t.Errorf("HighTDPMinPWM = %d, want 0 preserved (no floor on this device)", got.HighTDPMinPWM)
	}
}

func TestForceRequired(t *testing.T) {
	for _, l := range []Limits{DefaultLimits(), otherDevice()} {
		if l.ForceRequired(l.TDPMaxSafe) {
			t.Errorf("%s: ForceRequired(%d) = true, want false at the threshold", l.Model, l.TDPMaxSafe)
		}
		if !l.ForceRequired(l.TDPMaxSafe + 1) {
			t.Errorf("%s: ForceRequired(%d) = false, want true above it", l.Model, l.TDPMaxSafe+1)
		}
	}
}

func TestFanFloorPWM(t *testing.T) {
	z := DefaultLimits()
	for _, tt := range []struct{ pl1, want int }{
		{pl1: 0, want: PWMMin},
		{pl1: 50, want: PWMMin},
		{pl1: z.TDPMaxSafe, want: PWMMin},              // at the threshold, unconstrained
		{pl1: z.TDPMaxSafe + 1, want: z.HighTDPMinPWM}, // one over, floor applies
		{pl1: z.TDPMaxForced, want: z.HighTDPMinPWM},
	} {
		if got := z.FanFloorPWM(tt.pl1); got != tt.want {
			t.Errorf("Z13 FanFloorPWM(%d) = %d, want %d", tt.pl1, got, tt.want)
		}
	}

	// A device declaring no floor must never clamp, even far above its safe max.
	o := otherDevice()
	if got := o.FanFloorPWM(o.TDPMaxForced); got != PWMMin {
		t.Errorf("fictional FanFloorPWM(%d) = %d, want %d (device has no floor)", o.TDPMaxForced, got, PWMMin)
	}
}

// The ramp mirrors z13ctl's cli.HighTDPFanCurve by hand. Until the daemon
// serves limits over the API, a change there MUST be copied here — this pin is
// the only drift guard. The 204-era flat floor lingered in the drawer for a
// release after the daemon moved to this ramp; that is the regression this
// test exists to name.
func TestDefaultFloorCurveMirrorsTheDaemon(t *testing.T) {
	want := []api.FanCurvePoint{
		{Temp: 30, PWM: 127}, {Temp: 40, PWM: 127}, {Temp: 50, PWM: 140}, {Temp: 60, PWM: 165},
		{Temp: 65, PWM: 190}, {Temp: 70, PWM: 215}, {Temp: 75, PWM: 235}, {Temp: 80, PWM: 255},
	}
	got := DefaultLimits().FloorCurve
	if len(got) != len(want) {
		t.Fatalf("FloorCurve has %d points, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("FloorCurve[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if b := DefaultLimits().HighTDPMinPWM; b != want[0].PWM {
		t.Errorf("HighTDPMinPWM = %d, want the ramp bottom %d", b, want[0].PWM)
	}
}

// FloorPWMAt must measure exactly as the daemon's cli.FloorPWMAt does — flat
// below the first knee, interpolated between knees, pinned past the last —
// because the set of curves the editor allows has to equal the set the daemon
// accepts.
func TestFloorPWMAt(t *testing.T) {
	floor := DefaultLimits().FloorCurve
	tests := []struct{ temp, want int }{
		{-10, 127}, // a floor does not taper off at low temperature
		{30, 127},
		{35, 127}, // between two equal knees
		{45, 133}, // 127 + (140-127)*5/10, integer math
		{50, 140},
		{55, 152}, // 140 + (165-140)*5/10
		{75, 235},
		{80, 255},
		{100, 255}, // past the last knee stays at full speed
	}
	for _, tt := range tests {
		if got := FloorPWMAt(floor, tt.temp); got != tt.want {
			t.Errorf("FloorPWMAt(%d) = %d, want %d", tt.temp, got, tt.want)
		}
	}
	if got := FloorPWMAt(nil, 90); got != PWMMin {
		t.Errorf("FloorPWMAt(nil, 90) = %d, want %d (no floor)", got, PWMMin)
	}
}

func TestActiveFloor(t *testing.T) {
	z := DefaultLimits()
	if f := z.ActiveFloor(z.TDPMaxSafe); f != nil {
		t.Errorf("ActiveFloor(%d) = %v, want nil at the threshold", z.TDPMaxSafe, f)
	}
	if f := z.ActiveFloor(z.TDPMaxSafe + 1); len(f) == 0 {
		t.Errorf("ActiveFloor(%d) = nil, want the floor curve above the threshold", z.TDPMaxSafe+1)
	}
	o := otherDevice()
	if f := o.ActiveFloor(o.TDPMaxForced); f != nil {
		t.Errorf("fictional ActiveFloor(%d) = %v, want nil (device has no floor)", o.TDPMaxForced, f)
	}
}

func TestSanitizedFloorCurve(t *testing.T) {
	t.Run("scalar-only description synthesizes a flat floor", func(t *testing.T) {
		// A daemon (or old built-in table) that knows only the scalar minimum
		// must still constrain the editor, just without the ramp.
		got := Limits{HighTDPMinPWM: 204}.Sanitized()
		if len(got.FloorCurve) == 0 {
			t.Fatal("no floor synthesized from the scalar")
		}
		for _, temp := range []int{0, 50, 200} {
			if pwm := FloorPWMAt(got.FloorCurve, temp); pwm != 204 {
				t.Errorf("synthesized floor at %d°C = %d, want flat 204", temp, pwm)
			}
		}
	})
	t.Run("no floor stays no floor", func(t *testing.T) {
		got := Limits{}.Sanitized()
		if len(got.FloorCurve) != 0 || got.HighTDPMinPWM != 0 {
			t.Errorf("floorless device grew a floor: curve %v, min %d", got.FloorCurve, got.HighTDPMinPWM)
		}
	})
	t.Run("scalar follows the curve bottom", func(t *testing.T) {
		in := Limits{HighTDPMinPWM: 90, FloorCurve: []api.FanCurvePoint{{Temp: 30, PWM: 100}, {Temp: 80, PWM: 255}}}
		if got := in.Sanitized(); got.HighTDPMinPWM != 100 {
			t.Errorf("HighTDPMinPWM = %d, want 100 (the curve's bottom is authoritative)", got.HighTDPMinPWM)
		}
	})
	t.Run("malformed curve falls back to a flat scalar floor", func(t *testing.T) {
		// Temperatures out of order: the curve does not say which point is
		// wrong, so the whole group falls back rather than one point moving.
		in := Limits{HighTDPMinPWM: 127, FloorCurve: []api.FanCurvePoint{{Temp: 50, PWM: 200}, {Temp: 40, PWM: 255}}}
		got := in.Sanitized()
		for _, temp := range []int{0, 60, 100} {
			if pwm := FloorPWMAt(got.FloorCurve, temp); pwm != 127 {
				t.Errorf("fallback floor at %d°C = %d, want flat 127", temp, pwm)
			}
		}
	})
	t.Run("out-of-range floor PWMs are clamped", func(t *testing.T) {
		in := Limits{FloorCurve: []api.FanCurvePoint{{Temp: 30, PWM: -5}, {Temp: 80, PWM: 999}}}
		got := in.Sanitized()
		if got.FloorCurve[0].PWM != PWMMin || got.FloorCurve[1].PWM != PWMMax {
			t.Errorf("clamped floor = %v, want PWMs [%d %d]", got.FloorCurve, PWMMin, PWMMax)
		}
	})
	t.Run("does not mutate the caller's slice", func(t *testing.T) {
		curve := []api.FanCurvePoint{{Temp: 30, PWM: 999}}
		Limits{FloorCurve: curve}.Sanitized()
		if curve[0].PWM != 999 {
			t.Error("Sanitized mutated the input FloorCurve")
		}
	})
	t.Run("idempotent", func(t *testing.T) {
		for _, in := range []Limits{{}, {HighTDPMinPWM: 204}, DefaultLimits()} {
			once := in.Sanitized()
			twice := once.Sanitized()
			if once.HighTDPMinPWM != twice.HighTDPMinPWM || len(once.FloorCurve) != len(twice.FloorCurve) {
				t.Errorf("not idempotent: %+v -> %+v", once, twice)
			}
		}
	})
}

func TestIsStockPPT(t *testing.T) {
	for _, l := range []Limits{DefaultLimits(), otherDevice()} {
		for name, stock := range l.StockProfilePPT {
			if !l.IsStockPPT(stock) {
				t.Errorf("%s: IsStockPPT(%s defaults %+v) = false, want true", l.Model, name, stock)
			}
			// The daemon mirrors APU/Platform sPPT from PL2, so a real reading
			// carries values the table does not list. They must not affect it.
			withMirrored := stock
			withMirrored.APUSPPT = stock.PL2SPPT
			withMirrored.PlatformSPPT = stock.PL2SPPT
			if !l.IsStockPPT(withMirrored) {
				t.Errorf("%s: IsStockPPT(%s with mirrored APU fields) = false, want true", l.Model, name)
			}
		}
	}

	z := DefaultLimits()
	for _, tdp := range []api.TDPState{
		{PL1SPL: 80, PL2SPPT: 80, FPPT: 80},
		{PL1SPL: 52, PL2SPPT: 71, FPPT: 69}, // one watt off balanced
		{PL1SPL: 45, PL2SPPT: 45, FPPT: 45},
		{},
	} {
		if z.IsStockPPT(tdp) {
			t.Errorf("IsStockPPT(%+v) = true, want false", tdp)
		}
	}

	// Another device's stock values are not this device's stock values.
	if z.IsStockPPT(otherDevice().StockProfilePPT["balanced"]) {
		t.Error("Z13 accepted the fictional device's balanced defaults as stock")
	}
}

func TestNeedsAdvanced(t *testing.T) {
	z := DefaultLimits()

	tests := []struct {
		name     string
		isCustom bool
		tdp      api.TDPState
		want     bool
	}{
		{
			// The regression this function exists for: PL1 above the basic
			// slider's ceiling used to clamp to 70 and report "70 W" while the
			// hardware ran at 80 W, and a save would then send 70.
			name:     "sustained above basic ceiling",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 80, PL2SPPT: 80, FPPT: 80},
			want:     true,
		},
		{
			name:     "exactly at the basic ceiling is representable",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 70, PL2SPPT: 70, FPPT: 70},
			want:     false,
		},
		{
			name:     "one over the basic ceiling is not",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 71, PL2SPPT: 71, FPPT: 71},
			want:     true,
		},
		{
			// Basic mode applies one value to all three, so differing limits
			// cannot be shown even when every value is inside the basic range.
			// Deliberately not 52/71/70 — that is the balanced stock triple and
			// is covered below as a state the user did not choose.
			name:     "limits differ below the ceiling",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 50, PL2SPPT: 65, FPPT: 60},
			want:     true,
		},
		{
			name:     "only PL2 differs",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 50, PL2SPPT: 60, FPPT: 50},
			want:     true,
		},
		{
			name:     "only PL3 differs",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 50, PL2SPPT: 50, FPPT: 60},
			want:     true,
		},
		{
			// What a basic save round-trips as: the daemon defaults the blank
			// PL fields to the single value, so this must stay in basic.
			name:     "equal triple from a basic save",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 45, PL2SPPT: 45, FPPT: 45},
			want:     false,
		},
		{
			// APUSPPT/PlatformSPPT are daemon bookkeeping mirrored from PL2 and
			// are not shown in the drawer, so they must not force advanced.
			name:     "mirrored APU fields are ignored",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 50, PL2SPPT: 50, FPPT: 50, APUSPPT: 70, PlatformSPPT: 70},
			want:     false,
		},
		{
			name:     "zero value is representable",
			isCustom: true,
			tdp:      api.TDPState{},
			want:     false,
		},

		// Stock profiles report the firmware's own per-profile PPT defaults,
		// which differ between limits by design. Those are a starting point for
		// editing, not settings the user chose, so they must never force the
		// advanced view — otherwise every stock profile opens it. isCustom is
		// false on every stock profile (api.State.InCustomProfile).
		{
			name:     "stock balanced defaults do not force advanced",
			isCustom: false,
			tdp:      api.TDPState{PL1SPL: 52, PL2SPPT: 71, FPPT: 70},
			want:     false,
		},
		{
			name:     "stock quiet defaults do not force advanced",
			isCustom: false,
			tdp:      api.TDPState{PL1SPL: 40, PL2SPPT: 55, FPPT: 55},
			want:     false,
		},
		{
			name:     "stock performance defaults do not force advanced",
			isCustom: false,
			tdp:      api.TDPState{PL1SPL: 70, PL2SPPT: 86, FPPT: 86},
			want:     false,
		},
		{
			// Even a reading above the ceiling stays basic on a stock profile:
			// it is the firmware's value, not a saved custom one.
			name:     "stock profile above the ceiling still stays basic",
			isCustom: false,
			tdp:      api.TDPState{PL1SPL: 80, PL2SPPT: 90, FPPT: 90},
			want:     false,
		},
		{
			// An equal triple above the ceiling would trip both value checks;
			// only the isCustom gate keeps it basic.
			name:     "not custom stays basic even above the ceiling",
			isCustom: false,
			tdp:      api.TDPState{PL1SPL: 80, PL2SPPT: 80, FPPT: 80},
			want:     false,
		},

		// Saving a fan curve or undervolt flips the daemon to a custom profile
		// by itself, while the power limits stay at the firmware's values. The
		// isCustom check alone would then fire on numbers the user never chose.
		{
			name:     "custom fan curve on top of stock balanced limits",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 52, PL2SPPT: 71, FPPT: 70},
			want:     false,
		},
		{
			name:     "custom fan curve on top of stock quiet limits",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 40, PL2SPPT: 55, FPPT: 55},
			want:     false,
		},
		{
			name:     "custom fan curve on top of stock performance limits",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 70, PL2SPPT: 86, FPPT: 86},
			want:     false,
		},
		{
			// One watt off the stock table is a deliberate edit, not firmware.
			name:     "one watt off stock balanced is a user choice",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 53, PL2SPPT: 71, FPPT: 70},
			want:     true,
		},
		{
			// The case the ceiling rule exists for must survive the stock check.
			name:     "advanced burst limits within the basic ceiling",
			isCustom: true,
			tdp:      api.TDPState{PL1SPL: 65, PL2SPPT: 85, FPPT: 90},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := z.NeedsAdvanced(tt.isCustom, tt.tdp); got != tt.want {
				t.Errorf("NeedsAdvanced(%v, %+v) = %v, want %v", tt.isCustom, tt.tdp, got, tt.want)
			}
		})
	}
}

// The ceiling that matters is the device's own, not the Z13's.
func TestNeedsAdvancedUsesTheDevicesCeiling(t *testing.T) {
	o := otherDevice() // BasicSliderMax 25

	// Well inside the Z13's basic range, but above this device's.
	tdp := api.TDPState{PL1SPL: 40, PL2SPPT: 40, FPPT: 40}
	if !o.NeedsAdvanced(true, tdp) {
		t.Errorf("fictional device: NeedsAdvanced(%+v) = false, want true (over its %dW ceiling)",
			tdp, o.BasicSliderMax())
	}
	if DefaultLimits().NeedsAdvanced(true, tdp) {
		t.Errorf("Z13: NeedsAdvanced(%+v) = true, want false (inside its %dW ceiling)",
			tdp, DefaultLimits().BasicSliderMax())
	}

	// And this device's own stock values must not force advanced on it.
	if o.NeedsAdvanced(true, o.StockProfilePPT["balanced"]) {
		t.Error("fictional device forced advanced for its own stock balanced values")
	}
}

func TestFanCurveIsCustom(t *testing.T) {
	eight := make([]api.FanCurvePoint, CurvePoints)

	tests := []struct {
		name string
		fc   *api.FanCurveState
		want bool
	}{
		{name: "nil state", fc: nil, want: false},
		{
			name: "custom mode with a full curve",
			fc:   &api.FanCurveState{Mode: FanModeCustom, Points: eight},
			want: true,
		},
		{
			// The regression: switching to a stock profile releases the fans to
			// firmware auto, but the curve registers still read back the old
			// custom points. Displaying them shows a curve the fans do not follow.
			name: "auto mode still reports stale points",
			fc:   &api.FanCurveState{Mode: FanModeAuto, Points: eight},
			want: false,
		},
		{
			name: "full-speed mode",
			fc:   &api.FanCurveState{Mode: FanModeFullSpeed, Points: eight},
			want: false,
		},
		{
			name: "custom mode but a short curve is unusable",
			fc:   &api.FanCurveState{Mode: FanModeCustom, Points: eight[:3]},
			want: false,
		},
		{
			name: "custom mode with no points",
			fc:   &api.FanCurveState{Mode: FanModeCustom},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FanCurveIsCustom(tt.fc); got != tt.want {
				t.Errorf("FanCurveIsCustom() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultCurveIsValid(t *testing.T) {
	for _, l := range []Limits{DefaultLimits(), otherDevice()} {
		assertCurveValid(t, l, l.DefaultCurve(), nil)
	}
}

// The Z13's hand-tuned shape must survive being fitted to the Z13's own range.
func TestDefaultCurveUnchangedOnTheZ13(t *testing.T) {
	want := "35:0,45:25,50:50,60:80,70:120,80:170,90:220,100:255"
	if got := DefaultLimits().DefaultCurve().String(); got != want {
		t.Errorf("Z13 DefaultCurve() = %q, want %q", got, want)
	}
}

// A curve built for a wider temperature range must come back valid rather than
// collapsing every out-of-range point onto the maximum.
func TestEnforceCurve_NormalizesACurveFromAnotherDevice(t *testing.T) {
	narrow := otherDevice()
	c := DefaultLimits().DefaultCurve() // 35–100°C, outside narrow's 40–90
	narrow.EnforceCurve(&c, 0, nil)
	assertCurveValid(t, narrow, c, nil)
}

func TestCurveString(t *testing.T) {
	c := Curve{
		{Temp: 35, PWM: 0}, {Temp: 45, PWM: 25}, {Temp: 50, PWM: 50}, {Temp: 60, PWM: 80},
		{Temp: 70, PWM: 120}, {Temp: 80, PWM: 170}, {Temp: 90, PWM: 220}, {Temp: 100, PWM: 255},
	}
	want := "35:0,45:25,50:50,60:80,70:120,80:170,90:220,100:255"
	if got := c.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// Dragging a point off the left edge must not squash the points behind it onto
// TempMin. Point 3 has three points below it, each needing its own degree, so
// the lowest it can sit is TempMin+3.
func TestEnforceCurve_TemperaturesStrictlyIncrease(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	c[3].Temp = 20
	l.EnforceCurve(&c, 3, nil)

	if want := l.TempMin + 3; c[3].Temp != want {
		t.Errorf("dragged point temp = %d, want %d (leaves room for points 0-2)", c[3].Temp, want)
	}
	assertCurveValid(t, l, c, nil)
}

// The mirror of the above: a point dragged off the right edge must leave room
// for the points after it.
func TestEnforceCurve_LeavesRoomAboveDraggedPoint(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	c[4].Temp = 999
	l.EnforceCurve(&c, 4, nil)

	if want := l.TempMax - (CurvePoints - 1 - 4); c[4].Temp != want {
		t.Errorf("dragged point temp = %d, want %d (leaves room for points 5-7)", c[4].Temp, want)
	}
	assertCurveValid(t, l, c, nil)
}

func TestEnforceCurve_PWMNeverDecreases(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	c[1].PWM = 240
	l.EnforceCurve(&c, 1, nil)

	if c[1].PWM != 240 {
		t.Errorf("dragged point PWM = %d, want 240 preserved", c[1].PWM)
	}
	assertCurveValid(t, l, c, nil)
}

func TestEnforceCurve_DraggedPointWinsOverNeighbours(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	c[5].PWM = 30
	l.EnforceCurve(&c, 5, nil)

	if c[5].PWM != 30 {
		t.Errorf("dragged point PWM = %d, want 30 preserved", c[5].PWM)
	}
	assertCurveValid(t, l, c, nil)
}

func TestEnforceCurve_ClampsOutOfRange(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	c[7].Temp = 500
	c[7].PWM = 9000
	l.EnforceCurve(&c, 7, nil)

	if c[7].Temp != l.TempMax {
		t.Errorf("temp = %d, want clamped to %d", c[7].Temp, l.TempMax)
	}
	if c[7].PWM != PWMMax {
		t.Errorf("PWM = %d, want clamped to %d", c[7].PWM, PWMMax)
	}
	assertCurveValid(t, l, c, nil)
}

// The floor is the whole reason the drawer cannot let a drag go low: the daemon
// rejects the entire curve if any point is under the floor *at that point's
// temperature* while PL1 is high. Per-point matters in both directions: a flat
// 50% clamp would allow 50% at 90°C (the daemon refuses it — the ramp is at
// full speed there), and a flat 80% clamp would forbid the quiet 50% idle the
// floor deliberately allows.
func TestEnforceCurve_HighTDPFloorLiftsEveryPoint(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve() // most of its points sit below the ramp
	l.EnforceCurve(&c, 0, l.FloorCurve)

	for i, p := range c {
		if floorMin := FloorPWMAt(l.FloorCurve, p.Temp); p.PWM < floorMin {
			t.Errorf("point %d (%d°C) PWM = %d, below floor %d", i, p.Temp, p.PWM, floorMin)
		}
	}
	// The two ends pin the ramp shape: idle stays quiet, hot is full speed.
	if c[0].PWM != l.HighTDPMinPWM {
		t.Errorf("coolest point PWM = %d, want lifted only to the %d bottom", c[0].PWM, l.HighTDPMinPWM)
	}
	if c[7].PWM != PWMMax {
		t.Errorf("hottest point PWM = %d, want %d (ramp is full speed past 80°C)", c[7].PWM, PWMMax)
	}
	assertCurveValid(t, l, c, l.FloorCurve)
}

func TestEnforceCurve_DragBelowFloorIsLifted(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	l.EnforceCurve(&c, 0, l.FloorCurve) // start from a floored curve

	c[2].PWM = 10 // user drags a point to the bottom
	l.EnforceCurve(&c, 2, l.FloorCurve)

	if floorMin := FloorPWMAt(l.FloorCurve, c[2].Temp); c[2].PWM < floorMin {
		t.Errorf("dragged point (%d°C) PWM = %d, want lifted to at least %d", c[2].Temp, c[2].PWM, floorMin)
	}
	assertCurveValid(t, l, c, l.FloorCurve)
}

// Dragging a point into a hotter region must pick up that region's higher
// floor, not carry the cooler region's minimum along with it.
func TestEnforceCurve_DragToHotterTempRaisesTheFloor(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	l.EnforceCurve(&c, 0, l.FloorCurve)

	c[6].Temp = 95 // well past the ramp's 80°C full-speed knee
	c[6].PWM = 130 // fine at idle temperatures, far below the floor at 95°C
	l.EnforceCurve(&c, 6, l.FloorCurve)

	if c[6].PWM != PWMMax {
		t.Errorf("point at %d°C PWM = %d, want %d (floor is full speed there)", c[6].Temp, c[6].PWM, PWMMax)
	}
	assertCurveValid(t, l, c, l.FloorCurve)
}

func TestEnforceCurve_Idempotent(t *testing.T) {
	l := DefaultLimits()
	for _, floor := range [][]api.FanCurvePoint{nil, l.FloorCurve} {
		c := l.DefaultCurve()
		c[4].PWM = 12
		c[4].Temp = 33
		l.EnforceCurve(&c, 4, floor)
		once := c
		l.EnforceCurve(&c, 4, floor)
		if c != once {
			t.Errorf("floor %v: second pass changed the curve: %v then %v", floor, once, c)
		}
	}
}

func TestEnforceCurve_OutOfRangeIndexIsIgnored(t *testing.T) {
	l := DefaultLimits()
	c := l.DefaultCurve()
	before := c
	l.EnforceCurve(&c, -1, nil)
	l.EnforceCurve(&c, CurvePoints, nil)
	if c != before {
		t.Error("out-of-range index modified the curve")
	}
}

// Every index dragged to every extreme, on both devices, at every floor. This is
// the sweep that caught the original squash bug, and it is what proves the
// constraint logic is not tuned to the Z13's temperature range.
func TestEnforceCurve_AllIndicesAtBothExtremesOnEveryDevice(t *testing.T) {
	for _, l := range []Limits{DefaultLimits(), otherDevice()} {
		for _, floor := range [][]api.FanCurvePoint{nil, l.FloorCurve} {
			for idx := 0; idx < CurvePoints; idx++ {
				for _, temp := range []int{-100, 0, 20, 500} {
					c := l.DefaultCurve()
					c[idx].Temp = temp
					l.EnforceCurve(&c, idx, floor)
					assertCurveValid(t, l, c, floor)
				}
			}
		}
	}
}

// assertCurveValid checks every invariant the daemon and firmware require. The
// floor check is per point at that point's temperature — the same measurement
// the daemon makes.
func assertCurveValid(t *testing.T, l Limits, c Curve, floor []api.FanCurvePoint) {
	t.Helper()
	for i, p := range c {
		if p.Temp < l.TempMin || p.Temp > l.TempMax {
			t.Errorf("%s: point %d temp = %d, outside [%d,%d]", l.Model, i, p.Temp, l.TempMin, l.TempMax)
		}
		if floorMin := FloorPWMAt(floor, p.Temp); p.PWM < floorMin || p.PWM > PWMMax {
			t.Errorf("%s: point %d (%d°C) PWM = %d, outside [%d,%d]", l.Model, i, p.Temp, p.PWM, floorMin, PWMMax)
		}
		if i > 0 {
			if c[i].Temp <= c[i-1].Temp {
				t.Errorf("%s: temps not strictly increasing at %d: %d then %d", l.Model, i, c[i-1].Temp, c[i].Temp)
			}
			if c[i].PWM < c[i-1].PWM {
				t.Errorf("%s: PWM decreased at %d: %d then %d", l.Model, i, c[i-1].PWM, c[i].PWM)
			}
		}
	}
}

// assertLimitsUsable states what the rest of the drawer assumes about a Limits
// value: the TDP bounds are ordered, the temperature axis is wide enough for a
// curve, and the fan floor is a real PWM value. Anything violating these produces
// either a curve the daemon rejects or NaN coordinates in the editor.
func assertLimitsUsable(t *testing.T, l Limits) {
	t.Helper()
	if l.TDPMin >= l.TDPMaxSafe {
		t.Errorf("TDPMin %d not below TDPMaxSafe %d", l.TDPMin, l.TDPMaxSafe)
	}
	if l.TDPMaxSafe > l.TDPMaxForced {
		t.Errorf("TDPMaxSafe %d above TDPMaxForced %d", l.TDPMaxSafe, l.TDPMaxForced)
	}
	if got := l.TempMax - l.TempMin; got < CurvePoints-1 {
		t.Errorf("temperature range %d too narrow for %d points", got, CurvePoints)
	}
	if l.HighTDPMinPWM < PWMMin || l.HighTDPMinPWM > PWMMax {
		t.Errorf("HighTDPMinPWM %d outside [%d,%d]", l.HighTDPMinPWM, PWMMin, PWMMax)
	}
	if len(l.FloorCurve) > 0 {
		if l.FloorCurve[0].PWM != l.HighTDPMinPWM {
			t.Errorf("HighTDPMinPWM %d disagrees with FloorCurve bottom %d", l.HighTDPMinPWM, l.FloorCurve[0].PWM)
		}
		for i := 1; i < len(l.FloorCurve); i++ {
			if l.FloorCurve[i].Temp <= l.FloorCurve[i-1].Temp || l.FloorCurve[i].PWM < l.FloorCurve[i-1].PWM {
				t.Errorf("FloorCurve not monotone at %d: %+v", i, l.FloorCurve)
			}
		}
	}
	if l.BasicSliderMax() > l.TDPMaxForced {
		t.Errorf("BasicSliderMax %d above TDPMaxForced %d", l.BasicSliderMax(), l.TDPMaxForced)
	}
	if len(l.StockProfilePPT) == 0 {
		t.Error("StockProfilePPT is empty")
	}
}

// TestSanitizedRepairsInconsistentLimits covers the values a daemon-served
// device description could carry that a zero check cannot catch. This is the path
// multi-device support opens: today Limits is a compiled-in constant, tomorrow it
// arrives over a socket from a daemon of unknown version.
func TestSanitizedRepairsInconsistentLimits(t *testing.T) {
	d := DefaultLimits()

	tests := []struct {
		name string
		in   Limits
	}{
		{"zero value", Limits{}},
		{"temp axis inverted", Limits{TempMin: 90, TempMax: 40}},
		{"temp axis collapsed", Limits{TempMin: 50, TempMax: 50}},
		{"temp axis too narrow for the curve", Limits{TempMin: 50, TempMax: 55}},
		{"temp axis exactly one degree short", Limits{TempMin: 40, TempMax: 40 + CurvePoints - 2}},
		{"safe max below the minimum", Limits{TDPMin: 60, TDPMaxSafe: 30, TDPMaxForced: 90}},
		{"safe max above the forced max", Limits{TDPMin: 5, TDPMaxSafe: 95, TDPMaxForced: 90}},
		{"min equals safe max", Limits{TDPMin: 50, TDPMaxSafe: 50, TDPMaxForced: 90}},
		{"fan floor above the hwmon maximum", Limits{HighTDPMinPWM: 999}},
		{"fan floor negative", Limits{HighTDPMinPWM: -5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Sanitized()
			assertLimitsUsable(t, got)
			// Whatever it repaired, the placeholder curve must still be valid — the
			// editor draws it before the daemon reports anything.
			assertCurveValid(t, got, got.DefaultCurve(), nil)
		})
	}

	// A device with no fan floor keeps that: zero is legitimate there, and the
	// repair above must not mistake it for a missing value.
	t.Run("zero fan floor is preserved", func(t *testing.T) {
		if got := (Limits{HighTDPMinPWM: 0}).Sanitized(); got.HighTDPMinPWM != 0 {
			t.Errorf("HighTDPMinPWM = %d, want 0 preserved", got.HighTDPMinPWM)
		}
	})

	// A consistent description must pass through untouched, so the repair cannot
	// quietly overwrite a legitimate device with the Z13's numbers.
	t.Run("consistent limits are untouched", func(t *testing.T) {
		for _, l := range []Limits{d, otherDevice()} {
			got := l.Sanitized()
			if got.TDPMin != l.TDPMin || got.TDPMaxSafe != l.TDPMaxSafe ||
				got.TDPMaxForced != l.TDPMaxForced ||
				got.TempMin != l.TempMin || got.TempMax != l.TempMax ||
				got.HighTDPMinPWM != l.HighTDPMinPWM {
				t.Errorf("%s: Sanitized altered a consistent value: %+v -> %+v", l.Model, l, got)
			}
		}
	})
}

// TestSanitizedIsIdempotent — a repaired value must not repair further, or the
// result would depend on how many times it had been through.
func TestSanitizedIsIdempotent(t *testing.T) {
	for _, in := range []Limits{{}, {TempMin: 50, TempMax: 50}, {TDPMin: 60, TDPMaxSafe: 30}} {
		once := in.Sanitized()
		twice := once.Sanitized()
		if once.TDPMin != twice.TDPMin || once.TDPMaxSafe != twice.TDPMaxSafe ||
			once.TDPMaxForced != twice.TDPMaxForced ||
			once.TempMin != twice.TempMin || once.TempMax != twice.TempMax ||
			once.HighTDPMinPWM != twice.HighTDPMinPWM {
			t.Errorf("not idempotent: %+v -> %+v -> %+v", in, once, twice)
		}
	}
}

// TestEnforceCurveSurvivesASanitizedNarrowAxis is the failure this repair
// prevents, stated end to end: take the narrowest axis Sanitized will accept,
// push a point to each extreme, and require a valid curve every time.
func TestEnforceCurveOnNarrowestAcceptedAxis(t *testing.T) {
	l := Limits{TempMin: 40, TempMax: 40 + CurvePoints - 1}.Sanitized()
	assertLimitsUsable(t, l)

	for idx := 0; idx < CurvePoints; idx++ {
		for _, temp := range []int{-100, 0, l.TempMin, l.TempMax, 10000} {
			c := l.DefaultCurve()
			c[idx].Temp = temp
			l.EnforceCurve(&c, idx, nil)
			assertCurveValid(t, l, c, nil)
		}
	}
}
