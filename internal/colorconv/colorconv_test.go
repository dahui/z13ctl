// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package colorconv

import (
	"fmt"
	"math"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{in: "FF6600", want: "FF6600", wantOK: true},
		{in: "ff6600", want: "FF6600", wantOK: true},
		{in: "#FF6600", want: "FF6600", wantOK: true},
		{in: "  #ff6600  ", want: "FF6600", wantOK: true},
		{in: "F60", want: "FF6600", wantOK: true}, // shorthand expands
		{in: "#f60", want: "FF6600", wantOK: true},
		{in: "000000", want: "000000", wantOK: true},
		{in: "FFFFFF", want: "FFFFFF", wantOK: true},

		// Rejected. Each of these used to yield pure black silently.
		{in: "", wantOK: false},
		{in: "#", wantOK: false},
		{in: "GGGGGG", wantOK: false},
		{in: "FF66", wantOK: false},
		{in: "FF66000", wantOK: false},
		{in: "FF 660", wantOK: false},
		{in: "0x1234", wantOK: false},
		{in: "-F0000", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, ok := Normalize(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("Normalize(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The regression this package exists for: an unparseable colour must be
// reportable as invalid, not silently indistinguishable from black. The drawer
// takes this value from daemon state, and a corrupt state file used to blacken the
// swatch and then get written back to the hardware on the next apply.
func TestHexToHSLRejectsGarbageInsteadOfReturningBlack(t *testing.T) {
	for _, bad := range []string{"", "nonsense", "#", "12345", "GGGGGG"} {
		h, s, l, ok := HexToHSL(bad)
		if ok {
			t.Errorf("HexToHSL(%q) ok = true, want false", bad)
		}
		if h != 0 || s != 0 || l != 0 {
			t.Errorf("HexToHSL(%q) = (%v,%v,%v), want zeroes alongside ok=false", bad, h, s, l)
		}
	}

	// Real black is valid and must be distinguishable from the failure above by
	// the ok flag alone.
	if _, _, _, ok := HexToHSL("000000"); !ok {
		t.Error("HexToHSL(000000) ok = false, want true — black is a colour")
	}
}

func TestHexToHSLKnownValues(t *testing.T) {
	tests := []struct {
		hex     string
		h, s, l float64
	}{
		{hex: "000000", h: 0, s: 0, l: 0},
		{hex: "FFFFFF", h: 0, s: 0, l: 100},
		{hex: "808080", h: 0, s: 0, l: 50.196},
		{hex: "FF0000", h: 0, s: 100, l: 50},
		{hex: "00FF00", h: 120, s: 100, l: 50},
		{hex: "0000FF", h: 240, s: 100, l: 50},
		{hex: "FFFF00", h: 60, s: 100, l: 50},
		{hex: "00FFFF", h: 180, s: 100, l: 50},
		{hex: "FF00FF", h: 300, s: 100, l: 50},
	}
	for _, tt := range tests {
		t.Run(tt.hex, func(t *testing.T) {
			h, s, l, ok := HexToHSL(tt.hex)
			if !ok {
				t.Fatalf("HexToHSL(%s) ok = false", tt.hex)
			}
			const tol = 0.01
			if math.Abs(h-tt.h) > tol || math.Abs(s-tt.s) > tol || math.Abs(l-tt.l) > tol {
				t.Errorf("HexToHSL(%s) = (%.3f,%.3f,%.3f), want (%v,%v,%v)", tt.hex, h, s, l, tt.h, tt.s, tt.l)
			}
		})
	}
}

// Hue is meaningless without saturation, so it must come back as a definite 0
// rather than whatever the branch arithmetic happened to leave behind.
func TestHexToHSLGreysHaveZeroHueAndSaturation(t *testing.T) {
	for _, grey := range []string{"000000", "111111", "808080", "CCCCCC", "FFFFFF"} {
		h, s, _, ok := HexToHSL(grey)
		if !ok {
			t.Fatalf("HexToHSL(%s) ok = false", grey)
		}
		if h != 0 || s != 0 {
			t.Errorf("HexToHSL(%s) = h %v s %v, want both 0", grey, h, s)
		}
	}
}

func TestHexToHSLHueIsInRange(t *testing.T) {
	for r := 0; r < 256; r += 17 {
		for g := 0; g < 256; g += 17 {
			for b := 0; b < 256; b += 17 {
				hex := fmt.Sprintf("%02X%02X%02X", r, g, b)
				h, s, l, ok := HexToHSL(hex)
				if !ok {
					t.Fatalf("HexToHSL(%s) ok = false", hex)
				}
				if h < 0 || h >= 360 {
					t.Errorf("HexToHSL(%s) h = %v, outside [0,360)", hex, h)
				}
				if s < 0 || s > 100 {
					t.Errorf("HexToHSL(%s) s = %v, outside [0,100]", hex, s)
				}
				if l < 0 || l > 100 {
					t.Errorf("HexToHSL(%s) l = %v, outside [0,100]", hex, l)
				}
			}
		}
	}
}

// The invariant that matters in the picker: opening the colour view converts hex
// to slider positions, and touching a slider converts back. A lossy round trip
// would drift the colour every time the view is opened.
func TestRoundTripHexToHSLAndBack(t *testing.T) {
	for r := 0; r < 256; r += 17 {
		for g := 0; g < 256; g += 17 {
			for b := 0; b < 256; b += 17 {
				want := fmt.Sprintf("%02X%02X%02X", r, g, b)
				h, s, l, ok := HexToHSL(want)
				if !ok {
					t.Fatalf("HexToHSL(%s) ok = false", want)
				}
				if got := HSLToHex(h, s, l); got != want {
					t.Errorf("round trip %s -> (%.3f,%.3f,%.3f) -> %s", want, h, s, l, got)
				}
			}
		}
	}
}

func TestHSLToHexAlwaysProducesAValidColour(t *testing.T) {
	for h := -720.0; h <= 1080; h += 37 {
		for s := -50.0; s <= 150; s += 25 {
			for l := -50.0; l <= 150; l += 25 {
				got := HSLToHex(h, s, l)
				if norm, ok := Normalize(got); !ok || norm != got {
					t.Errorf("HSLToHex(%v,%v,%v) = %q, not a canonical colour", h, s, l, got)
				}
			}
		}
	}
}

// Slider values arrive unclamped from GTK and hue wraps, so out-of-range input
// must behave rather than produce something like "-1-1-1".
func TestHSLToHexClampsAndWraps(t *testing.T) {
	if got, want := HSLToHex(0, 100, 50), "FF0000"; got != want {
		t.Errorf("HSLToHex(0,100,50) = %s, want %s", got, want)
	}
	if got, want := HSLToHex(360, 100, 50), "FF0000"; got != want {
		t.Errorf("hue 360 = %s, want %s (wraps to 0)", got, want)
	}
	if got, want := HSLToHex(-360, 100, 50), "FF0000"; got != want {
		t.Errorf("hue -360 = %s, want %s (wraps to 0)", got, want)
	}
	if got, want := HSLToHex(720+120, 100, 50), "00FF00"; got != want {
		t.Errorf("hue 840 = %s, want %s (wraps to 120)", got, want)
	}
	if got, want := HSLToHex(0, 999, 50), "FF0000"; got != want {
		t.Errorf("saturation 999 = %s, want %s (clamped to 100)", got, want)
	}
	if got, want := HSLToHex(0, 100, 999), "FFFFFF"; got != want {
		t.Errorf("lightness 999 = %s, want %s (clamped to 100)", got, want)
	}
	if got, want := HSLToHex(0, 100, -999), "000000"; got != want {
		t.Errorf("lightness -999 = %s, want %s (clamped to 0)", got, want)
	}
}

func TestRGB(t *testing.T) {
	tests := []struct {
		hex     string
		r, g, b float64
		ok      bool
	}{
		{"FF0000", 1, 0, 0, true},
		{"00FF00", 0, 1, 0, true},
		{"0000FF", 0, 0, 1, true},
		{"FFFFFF", 1, 1, 1, true},
		{"000000", 0, 0, 0, true},
		// Theme tokens arrive "#rrggbb" from theme.Colors, so the leading hash and
		// lowercase digits both have to work — this is the form the fan curve reads.
		{"#cc0000", 0.8, 0, 0, true},
		{"#f38ba8", 243.0 / 255, 139.0 / 255, 168.0 / 255, true},
		// Shorthand, same as Normalize accepts.
		{"#f00", 1, 0, 0, true},
		// Not colours: the caller must fall back, not draw black.
		{"", 0, 0, 0, false},
		{"nope", 0, 0, 0, false},
		{"#12345", 0, 0, 0, false},
		{"GGGGGG", 0, 0, 0, false},
	}
	const eps = 1e-9
	for _, tt := range tests {
		r, g, b, ok := RGB(tt.hex)
		if ok != tt.ok {
			t.Errorf("RGB(%q) ok = %v, want %v", tt.hex, ok, tt.ok)
			continue
		}
		if math.Abs(r-tt.r) > eps || math.Abs(g-tt.g) > eps || math.Abs(b-tt.b) > eps {
			t.Errorf("RGB(%q) = (%v, %v, %v), want (%v, %v, %v)",
				tt.hex, r, g, b, tt.r, tt.g, tt.b)
		}
	}
}

// TestRGBComponentsInRange is the invariant Cairo depends on: SetSourceRGBA
// silently clamps, so an out-of-range component would be a colour that is wrong
// rather than an error anyone notices.
func TestRGBComponentsInRange(t *testing.T) {
	for v := 0; v <= 0xFFFFFF; v += 7919 { // prime stride, ~2100 samples
		hex := fmt.Sprintf("%06X", v)
		r, g, b, ok := RGB(hex)
		if !ok {
			t.Fatalf("RGB(%q) not ok", hex)
		}
		for name, c := range map[string]float64{"r": r, "g": g, "b": b} {
			if c < 0 || c > 1 {
				t.Fatalf("RGB(%q) %s = %v, outside [0,1]", hex, name, c)
			}
		}
	}
}

// TestRGBMatchesEveryBuiltinThemeToken would be circular if it lived in the theme
// package; here it just checks that the string form theme.Colors uses is one RGB
// accepts, for every token the fan curve might read.
func TestRGBAcceptsThemeTokenForm(t *testing.T) {
	for _, hex := range []string{"#cc0000", "#1a1a1a", "#e0e0e0", "#888888", "#ff4444"} {
		if _, _, _, ok := RGB(hex); !ok {
			t.Errorf("RGB(%q) rejected a theme token", hex)
		}
	}
}

// TestRGBAcceptsEveryFormIsHexColorDoes ties RGB to the theme parser's notion of
// a colour. theme.IsHexColor accepts 3, 6 and 8 digits, so a theme.toml may carry
// an #rrggbbaa value; RGB rejecting it made every fan curve element silently fall
// back to the default palette for that theme.
//
// The alpha is dropped, not honoured: the chart picks its own per-element opacity.
func TestRGBAcceptsEveryFormIsHexColorDoes(t *testing.T) {
	tests := []struct {
		hex     string
		r, g, b float64
	}{
		{"#f00", 1, 0, 0},
		{"#ff0000", 1, 0, 0},
		{"#ff0000aa", 1, 0, 0},
		{"#FF0000AA", 1, 0, 0},
		{"#89b4fa80", 137.0 / 255, 180.0 / 255, 250.0 / 255},
		// Bare, as Normalize also allows.
		{"ff0000aa", 1, 0, 0},
	}
	const eps = 1e-9
	for _, tt := range tests {
		r, g, b, ok := RGB(tt.hex)
		if !ok {
			t.Errorf("RGB(%q) rejected a form theme.IsHexColor accepts", tt.hex)
			continue
		}
		if math.Abs(r-tt.r) > eps || math.Abs(g-tt.g) > eps || math.Abs(b-tt.b) > eps {
			t.Errorf("RGB(%q) = (%v, %v, %v), want (%v, %v, %v)",
				tt.hex, r, g, b, tt.r, tt.g, tt.b)
		}
	}
}

// The daemon's wire format is strictly RRGGBB, so loosening RGB must not have
// loosened Normalize with it — an 8-digit value reaching the hardware would be
// wrong, and silently writing it back is the bug Normalize was added to stop.
func TestNormalizeStillRejectsAlphaForms(t *testing.T) {
	for _, hex := range []string{"#ff0000aa", "ff0000aa", "#FF0000AA", "#f00a"} {
		if got, ok := Normalize(hex); ok {
			t.Errorf("Normalize(%q) = %q, ok — the daemon format has no alpha", hex, got)
		}
	}
}

// TestHSLRoundTrip is the invariant the colour picker rests on: opening it sets
// the sliders from the stored hex, and every slider change converts straight back.
// Any drift would mean a colour that shifts when the user opens the picker and
// nudges a slider back to where it started.
//
// Verified exact across the sampled space rather than approximately, because there
// is no rounding budget to spend — the daemon stores the hex the drawer sends.
func TestHSLRoundTrip(t *testing.T) {
	// Prime stride so the samples do not align with channel boundaries.
	for v := 0; v <= 0xFFFFFF; v += 4993 {
		hex := fmt.Sprintf("%06X", v)
		h, s, l, ok := HexToHSL(hex)
		if !ok {
			t.Fatalf("HexToHSL(%q) not ok", hex)
		}
		if back := HSLToHex(h, s, l); back != hex {
			t.Errorf("round trip drifted: %s -> H%.4f S%.4f L%.4f -> %s", hex, h, s, l, back)
		}
	}
}

// The greys are the interesting case: saturation is undefined there, so hue is
// reported as 0 and has to be reconstructed without shifting the colour.
func TestHSLRoundTripGreys(t *testing.T) {
	for c := 0; c <= 0xFF; c++ {
		hex := fmt.Sprintf("%02X%02X%02X", c, c, c)
		h, s, l, ok := HexToHSL(hex)
		if !ok {
			t.Fatalf("HexToHSL(%q) not ok", hex)
		}
		if s != 0 {
			t.Errorf("%s: saturation = %v, want 0 for a grey", hex, s)
		}
		if back := HSLToHex(h, s, l); back != hex {
			t.Errorf("grey round trip drifted: %s -> %s", hex, back)
		}
	}
}

// Every preset and swatch default a user can actually click.
func TestHSLRoundTripPresets(t *testing.T) {
	for _, hex := range []string{
		"FF0000", "FF6600", "FFFF00", "00FF00",
		"00FFFF", "0000FF", "FF00FF", "FFFFFF", "000000",
	} {
		h, s, l, _ := HexToHSL(hex)
		if back := HSLToHex(h, s, l); back != hex {
			t.Errorf("preset %s round-tripped to %s", hex, back)
		}
	}
}
