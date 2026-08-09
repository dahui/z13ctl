// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package uiscale

import (
	"fmt"
	"math"
	"testing"
)

func TestForAutoDetection(t *testing.T) {
	tests := []struct {
		width int
		want  float64
	}{
		{width: int(ReferenceWidth), want: 1.0},    // reference panel
		{width: 2560, want: 2560 / ReferenceWidth}, // Z13 native
		{width: 1920, want: 1920 / ReferenceWidth},
		{width: 1280, want: Min}, // small panel clamps up
		{width: 800, want: Min},
		{width: 7680, want: Max}, // 8K clamps down
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.width), func(t *testing.T) {
			got := For(tt.width, "")
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("For(%d, \"\") = %v, want %v", tt.width, got, tt.want)
			}
		})
	}
}

// Monitor geometry is not always available at realize time; a zero width must not
// produce a zero or negative scale, which would collapse the drawer.
func TestForHandlesUnknownGeometry(t *testing.T) {
	for _, w := range []int{0, -1, -4096} {
		if got := For(w, ""); got != Min {
			t.Errorf("For(%d, \"\") = %v, want %v", w, got, Min)
		}
	}
}

func TestForHonoursTheOverride(t *testing.T) {
	// The override replaces auto-detection entirely, including going below what
	// the panel width would give.
	if got := For(2560, "1.25"); got != 1.25 {
		t.Errorf("For(2560, \"1.25\") = %v, want 1.25", got)
	}
	if got := For(1280, "2.5"); got != 2.5 {
		t.Errorf("For(1280, \"2.5\") = %v, want 2.5", got)
	}
}

// Pins the intent that was previously implicit: an override is still clamped. A
// typo like "30" for "3.0" must not produce a drawer too large to dismiss.
func TestOverrideIsStillClamped(t *testing.T) {
	if got := For(2560, "30"); got != Max {
		t.Errorf("For(2560, \"30\") = %v, want %v (clamped)", got, Max)
	}
	if got := For(2560, "0.1"); got != Min {
		t.Errorf("For(2560, \"0.1\") = %v, want %v (clamped)", got, Min)
	}
	if !OverrideWasClamped("30") {
		t.Error("OverrideWasClamped(\"30\") = false, want true so the user can be told")
	}
	if OverrideWasClamped("2.0") {
		t.Error("OverrideWasClamped(\"2.0\") = true, want false")
	}
	if OverrideWasClamped("") {
		t.Error("OverrideWasClamped(\"\") = true, want false")
	}
}

// A malformed override falls back to auto-detection rather than to some fixed
// value — starting with a wrong-but-usable UI beats an unusable one.
func TestMalformedOverrideFallsBackToAutoDetection(t *testing.T) {
	auto := For(2560, "")
	for _, bad := range []string{"abc", "1.0.0", "", " ", "1,5", "NaN-ish", "-2", "0"} {
		if got := For(2560, bad); got != auto {
			t.Errorf("For(2560, %q) = %v, want the auto-detected %v", bad, got, auto)
		}
		if OverrideIsUsable(bad) {
			t.Errorf("OverrideIsUsable(%q) = true, want false", bad)
		}
	}
	for _, good := range []string{"1.5", "2", "0.5", "  "} {
		_ = good // "  " is covered above; listed here to document the boundary
	}
	if !OverrideIsUsable("1.5") {
		t.Error("OverrideIsUsable(\"1.5\") = false, want true")
	}
}

// Whatever the inputs, the result must be a usable scale. Anything outside the
// bounds means touch targets too small to hit or a drawer that does not fit.
func TestForAlwaysReturnsAUsableScale(t *testing.T) {
	widths := []int{-1, 0, 1, 640, 1280, 1707, 1920, 2560, 3840, 7680, 1 << 20}
	overrides := []string{"", "abc", "0", "-1", "0.001", "1", "1.5", "3", "3.001", "1000", "1e9", "NaN", "Inf"}
	for _, w := range widths {
		for _, o := range overrides {
			got := For(w, o)
			if math.IsNaN(got) || math.IsInf(got, 0) {
				t.Fatalf("For(%d, %q) = %v, not a real number", w, o, got)
			}
			if got < Min || got > Max {
				t.Errorf("For(%d, %q) = %v, outside [%v,%v]", w, o, got, Min, Max)
			}
		}
	}
}

func TestReferenceWidthGivesUnityScale(t *testing.T) {
	if got := For(int(ReferenceWidth), ""); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("For(ReferenceWidth) = %v, want 1.0 — the constant defines the unity point", got)
	}
}
