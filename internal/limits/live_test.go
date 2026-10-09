// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package limits

// live_test.go — the ranges a client must take from the device rather than
// from the Z13: undervolt and charge limit from the document, and the power
// limits from get-state's live tdp_limits.

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

func TestFromDeviceCarriesUndervoltAndBatteryRanges(t *testing.T) {
	l := FromDevice(&api.DeviceInfo{
		Model:     "OTHER",
		Undervolt: &api.UndervoltInfo{Min: -30, Max: 0},
		Battery:   &api.BatteryInfo{ChargeLimit: true, ChargeLimitMin: 60, ChargeLimitMax: 95},
	})
	if l.UVMin != -30 || l.UVMax != 0 {
		t.Errorf("undervolt range = %d..%d, want the device's -30..0", l.UVMin, l.UVMax)
	}
	if l.BatteryMin != 60 || l.BatteryMax != 95 {
		t.Errorf("charge limit range = %d..%d, want the device's 60..95", l.BatteryMin, l.BatteryMax)
	}
}

// A daemon older than the range fields serves charge_limit with no bounds, and
// one with no undervolt capability serves no section: both read as the range
// every pre-2.0 daemon accepted, never as 0..0.
func TestFromDeviceRangesAbsentFallBack(t *testing.T) {
	l := FromDevice(&api.DeviceInfo{Battery: &api.BatteryInfo{ChargeLimit: true}})
	d := DefaultLimits()
	if l.BatteryMin != d.BatteryMin || l.BatteryMax != d.BatteryMax {
		t.Errorf("charge limit range = %d..%d, want the default %d..%d", l.BatteryMin, l.BatteryMax, d.BatteryMin, d.BatteryMax)
	}
	if l.UVMin != d.UVMin || l.UVMax != d.UVMax {
		t.Errorf("undervolt range = %d..%d, want the default %d..%d", l.UVMin, l.UVMax, d.UVMin, d.UVMax)
	}
}

func TestSanitizedRepairsImpossibleRanges(t *testing.T) {
	d := DefaultLimits()
	for _, tt := range []struct {
		name string
		mut  func(*Limits)
	}{
		{"undervolt inverted", func(l *Limits) { l.UVMin, l.UVMax = 0, -20 }},
		{"undervolt above zero", func(l *Limits) { l.UVMin, l.UVMax = -10, 5 }},
		{"battery inverted", func(l *Limits) { l.BatteryMin, l.BatteryMax = 90, 50 }},
		{"battery past 100", func(l *Limits) { l.BatteryMin, l.BatteryMax = 40, 120 }},
		{"battery at zero", func(l *Limits) { l.BatteryMin, l.BatteryMax = 0, 80 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := DefaultLimits()
			tt.mut(&l)
			l = l.Sanitized()
			if l.UVMin != d.UVMin || l.UVMax != d.UVMax || l.BatteryMin != d.BatteryMin || l.BatteryMax != d.BatteryMax {
				t.Errorf("got uv %d..%d battery %d..%d, want the defaults", l.UVMin, l.UVMax, l.BatteryMin, l.BatteryMax)
			}
		})
	}
}

// The case this exists for, measured on 2026-10-09: a drawer started while the
// daemon was on asus-nb-wmi (the device file's 5–93) and then the armoury
// grant landed. The live ranges must replace the startup ones.
func TestWithTDPLimitsAdoptsTheKernelsRanges(t *testing.T) {
	armoury := &api.TDPLimits{
		Backend: "asus-armoury",
		PL1:     api.TDPRange{Min: 28, Max: 80},
		PL2:     api.TDPRange{Min: 32, Max: 92},
		PL3:     api.TDPRange{Min: 45, Max: 93},
		SafeMax: 75,
	}
	l := DefaultLimits().WithTDPLimits(armoury)
	if l.TDPMin != 28 || l.TDPMaxForced != 80 || l.TDPMaxSafe != 75 {
		t.Errorf("PL1 = %d..%d safe %d, want 28..80 safe 75", l.TDPMin, l.TDPMaxForced, l.TDPMaxSafe)
	}
	if lo, hi := l.PL2Range(); lo != 32 || hi != 92 {
		t.Errorf("PL2 = %d..%d, want 32..92", lo, hi)
	}
	if lo, hi := l.PL3Range(); lo != 45 || hi != 93 {
		t.Errorf("PL3 = %d..%d, want 45..93", lo, hi)
	}
	if l.Equal(DefaultLimits()) {
		t.Error("Equal reports no change after the ranges moved")
	}
	if !l.Equal(l.WithTDPLimits(armoury)) {
		t.Error("re-applying the same ranges should be a no-op by Equal")
	}
}

func TestWithTDPLimitsNilKeepsWhatItHas(t *testing.T) {
	l := DefaultLimits()
	if !l.WithTDPLimits(nil).Equal(l) {
		t.Error(`a nil tdp_limits changed the limits; it means "cannot say", not "none"`)
	}
}

// A reply that cannot be true — a safe maximum above the forced one — must not
// reach the widgets; Sanitized's whole-group fallback applies to live ranges
// exactly as to the document's.
func TestWithTDPLimitsRepairsAnImpossibleReply(t *testing.T) {
	l := DefaultLimits().WithTDPLimits(&api.TDPLimits{
		PL1: api.TDPRange{Min: 28, Max: 60}, SafeMax: 75,
	})
	d := DefaultLimits()
	if l.TDPMin != d.TDPMin || l.TDPMaxSafe != d.TDPMaxSafe || l.TDPMaxForced != d.TDPMaxForced {
		t.Errorf("PL1 = %d..%d safe %d, want the defaults after an inconsistent reply", l.TDPMin, l.TDPMaxForced, l.TDPMaxSafe)
	}
}
