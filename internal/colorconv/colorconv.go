// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package colorconv converts between the RRGGBB hex strings the z13ctl daemon
// speaks and the HSL components the drawer's colour picker manipulates.
//
// It is a separate package because internal/gui needs CGO and GTK4 headers and
// therefore cannot be unit tested. Colour conversion is arithmetic with a lot of
// branches — the kind of code that is cheap to get subtly wrong and cheap to
// verify — so it lives where tests can reach it.
package colorconv

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Normalize returns hex as the canonical uppercase RRGGBB the daemon expects,
// and reports whether the input was a colour at all.
//
// It accepts a leading "#" and the 3-digit shorthand because those turn up in
// hand-edited config and in state files written by other tools; both are expanded
// rather than rejected. Anything else fails, and callers must not use the string.
//
// Validating here matters because the drawer feeds daemon state straight into the
// picker: an unparseable value used to become pure black silently, and the next
// apply would have written that black back to the hardware.
func Normalize(hex string) (string, bool) {
	s := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(hex), "#")))
	switch len(s) {
	case 3:
		// #RGB shorthand: each digit doubles.
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return "", false
	}
	if _, err := strconv.ParseUint(s, 16, 32); err != nil {
		return "", false
	}
	return s, true
}

// HexToHSL converts a hex colour to HSL: H in [0,360), S and L in [0,100].
//
// ok is false when hex is not a colour, in which case the components are zero and
// the caller should leave its widgets alone rather than display the result —
// (0,0,0) is indistinguishable from a legitimate black.
func HexToHSL(hex string) (h, s, l float64, ok bool) {
	norm, ok := Normalize(hex)
	if !ok {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(norm, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	r := float64((v>>16)&0xFF) / 255
	g := float64((v>>8)&0xFF) / 255
	b := float64(v&0xFF) / 255

	maxC := math.Max(r, math.Max(g, b))
	minC := math.Min(r, math.Min(g, b))
	l = (maxC + minC) / 2

	if maxC == minC {
		return 0, 0, l * 100, true // achromatic: hue and saturation are undefined
	}
	d := maxC - minC
	if l > 0.5 {
		s = d / (2 - maxC - minC)
	} else {
		s = d / (maxC + minC)
	}
	switch maxC {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	case b:
		h = (r-g)/d + 4
	}
	return h * 60, s * 100, l * 100, true
}

// RGB converts a hex colour to the 0..1 component floats Cairo takes, so the fan
// curve chart can be drawn in the active theme's colours instead of hardcoded
// ones. ok is false when hex is not a colour, in which case the caller must fall
// back rather than draw the zero value — which is black, and invisible against a
// dark theme's background.
//
// Unlike Normalize it also accepts the 8-digit #rrggbbaa form and discards the
// alpha, because theme colours are a different input from daemon colours. A
// theme.toml may legitimately use it — theme.IsHexColor accepts 3, 6 and 8 digits
// — whereas the daemon's wire format is strictly RRGGBB, so Normalize must keep
// rejecting it. Missing that distinction made every chart element silently fall
// back to the default palette for such a theme. Alpha is dropped rather than
// honoured because the chart chooses its own per-element opacity.
func RGB(hex string) (r, g, b float64, ok bool) {
	s := strings.TrimSpace(hex)
	if len(strings.TrimPrefix(s, "#")) == 8 {
		s = strings.TrimPrefix(s, "#")[:6]
	}
	norm, ok := Normalize(s)
	if !ok {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(norm, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return float64((v>>16)&0xFF) / 255,
		float64((v>>8)&0xFF) / 255,
		float64(v&0xFF) / 255,
		true
}

// HSLToHex converts HSL components to canonical uppercase RRGGBB.
// H is taken modulo 360; S and L are clamped to [0,100], so slider values can be
// passed straight through.
func HSLToHex(h, s, l float64) string {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	s = clamp01(s / 100)
	l = clamp01(l / 100)
	h /= 360

	if s == 0 {
		v := int(math.Round(l * 255))
		return fmt.Sprintf("%02X%02X%02X", v, v, v)
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return fmt.Sprintf("%02X%02X%02X",
		int(math.Round(hueToRGB(p, q, h+1.0/3.0)*255)),
		int(math.Round(hueToRGB(p, q, h)*255)),
		int(math.Round(hueToRGB(p, q, h-1.0/3.0)*255)),
	)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// hueToRGB maps one hue-shifted channel back to an intensity.
func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}
