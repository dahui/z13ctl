// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package uiscale computes the drawer's UI scale factor for the gamescope
// backend, where GTK cannot be asked to scale for us.
//
// GDK_SCALE is unusable here: GTK would scale its buffer and gamescope's own
// scaler would then scale that again. So the drawer scales its CSS instead, and
// this package decides by how much.
//
// It is a separate package because internal/gui/gamescope imports cgo (Xlib), and
// a cgo package cannot be unit tested without the C toolchain and headers — while
// this is pure arithmetic with clamping and an env-var override, exactly the sort
// of thing worth pinning down.
package uiscale

import (
	"math"
	"strconv"
)

const (
	// ReferenceWidth is the output width at which scale is 1.0: 2560/1.5, which
	// makes the drawer occupy the same fraction of the screen as KDE at 150% on
	// the Z13's native panel.
	ReferenceWidth = 1707.0

	// Min is the smallest usable scale: below it touch targets fall under the
	// minimum comfortable size.
	Min = 1.0
	// Max is the largest usable scale: beyond it the drawer stops fitting on
	// screen, and an unreachable dismiss control cannot be recovered from.
	Max = 3.0
)

// For returns the UI scale for an output of the given pixel width.
//
// envOverride is the raw Z13GUI_SCALE value ("" when unset). When it parses as a
// positive number it replaces the computed scale — but is still clamped to
// [Min, Max]. That is deliberate: the override exists to correct a bad
// auto-detection, not to allow an unusable UI, and a typo like "30" instead of
// "3.0" should not produce a drawer nothing can dismiss.
//
// A malformed or non-positive override is ignored in favour of auto-detection,
// since silently falling back beats starting with an unusable interface.
//
// A non-positive width means the monitor geometry was not available; Min is the
// safe answer.
func For(outputWidth int, envOverride string) float64 {
	if v, ok := parseOverride(envOverride); ok {
		return clamp(v)
	}
	if outputWidth <= 0 {
		return Min
	}
	return clamp(float64(outputWidth) / ReferenceWidth)
}

// OverrideIsUsable reports whether envOverride would be honoured by For. Callers
// use it to log that a supplied value was ignored, which is otherwise invisible.
func OverrideIsUsable(envOverride string) bool {
	_, ok := parseOverride(envOverride)
	return ok
}

// OverrideWasClamped reports whether a usable override was outside [Min, Max] and
// therefore did not take effect as written — worth telling the user, since they
// asked for a specific number and got a different one.
func OverrideWasClamped(envOverride string) bool {
	v, ok := parseOverride(envOverride)
	return ok && v != clamp(v)
}

func parseOverride(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	// NaN needs an explicit test: ParseFloat("NaN") succeeds, and every
	// comparison against NaN is false, so a `v <= 0` guard lets it through and
	// clamp() then passes it along untouched. +Inf is fine — clamp catches it.
	if err != nil || math.IsNaN(v) || v <= 0 {
		return 0, false
	}
	return v, true
}

func clamp(v float64) float64 {
	if v < Min {
		return Min
	}
	if v > Max {
		return Max
	}
	return v
}
