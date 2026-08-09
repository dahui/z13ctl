// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package power holds the TDP and fan-curve rules the drawer needs in order to
// avoid offering the user a state the z13ctl daemon would refuse.
//
// It exists to be testable. The GTK code in internal/gui cannot be unit tested
// without CGO and GTK4 headers, so everything here is pure Go operating on plain
// values — no widgets, no daemon calls. internal/gui holds the widgets and
// delegates every decision to this package.
//
// # Device limits
//
// The numbers live in a Limits value rather than in package constants, because
// z13ctl is being extended to other AMD devices whose chips have different power
// limits and per-profile PPT defaults. DefaultLimits returns the 2025 Flow Z13's
// values, which are correct for the only device supported today.
//
// The daemon does not yet serve its limits over the API — they live in z13ctl's
// internal/cli, which is not exported through the api module, so they have to be
// duplicated here for now. When that API lands the only change is where the
// Limits value comes from: fetch once at startup, Sanitized, falling back to
// DefaultLimits. Nothing else in the drawer moves. See the design brief in
// z13ctl's .claude/plans/device-limits-api.md.
//
// If the two ever disagree the daemon wins: it validates against hardware, and
// these rules only exist so the UI does not present an option that gets rejected.
package power

import (
	"fmt"
	"strings"

	"github.com/dahui/z13ctl/api"
)

// ProfileCustom is the daemon's default custom profile name — the one created
// implicitly by the first custom TDP, fan curve or undervolt setting made while
// a firmware profile is active. Since z13ctl v1.3 it is one of several possible
// custom profiles, so testing a profile name against it no longer answers "do
// custom settings apply"; use api.State.InCustomProfile for that. The stock
// profiles are firmware-managed.
const ProfileCustom = "custom"

// PWM bounds. Unlike the TDP limits these are the hwmon interface's own range,
// not a device characteristic.
const (
	PWMMin = 0
	PWMMax = 255
)

// Fan pwm_enable modes as reported by sysfs and passed through by the daemon's
// get-state. Note these are the raw hwmon values, not the 0=auto/1=custom
// shorthand the api.FanCurveState doc comment suggests.
const (
	FanModeFullSpeed = 0
	FanModeCustom    = 1
	FanModeAuto      = 2
)

// CurvePoints is the number of points in a fan curve. It is fixed at 8 because
// Curve is a fixed-size array; if a future device needs a different count this
// becomes a Limits field and Curve becomes a slice, losing the compile-time
// length guarantee. Worth deciding deliberately rather than by accident.
const CurvePoints = 8

// Limits describes one device's power and thermal envelope — everything the
// drawer needs that varies with the hardware.
//
// Presentation policy is deliberately not in here. BasicSliderMax is a method
// rather than a field because "cap the simple slider a little under the safe max"
// is the drawer's choice; only the safe max itself is a device fact.
type Limits struct {
	Model         string // e.g. "GZ302EA"; for logs and bug reports
	TDPMin        int    // absolute minimum sustained limit
	TDPMaxSafe    int    // above this the daemon requires the force flag
	TDPMaxForced  int    // absolute hardware maximum
	HighTDPMinPWM int    // FloorCurve's bottom, for display text; 0 = no floor
	TempMin       int    // fan curve temperature axis, Celsius
	TempMax       int

	// FloorCurve is the per-point fan floor the daemon enforces while the
	// sustained limit exceeds TDPMaxSafe. It is a floor *curve*, not a scalar:
	// the daemon measures each user point against this curve at the point's own
	// temperature (z13ctl cli.FloorPWMAt), so 50% is enough at idle temperatures
	// while 80°C requires full speed. Empty = the device has no floor.
	// Sanitized keeps HighTDPMinPWM equal to this curve's bottom.
	FloorCurve []api.FanCurvePoint

	// StockProfilePPT holds each stock profile's firmware PPT defaults, used to
	// tell "the firmware's numbers" from "numbers the user chose". Only the three
	// limits the drawer displays are listed; the daemon also tracks APU/Platform
	// sPPT, which it mirrors from PL2 and which no UI shows.
	StockProfilePPT map[string]api.TDPState
}

// DefaultLimits returns the 2025 ROG Flow Z13 (GZ302) values. These mirror
// z13ctl's cli.TDPMin / TDPMaxSafe / TDPMaxForced / cli.HighTDPFanCurve and
// cli.StockProfilePPT. Until the daemon serves them over the API, any change on
// the daemon side must be copied here by hand — the floor dropped from a flat
// 80% to this 50%-bottomed ramp in z13ctl v1.3.x and the drawer kept clamping
// at 80% for a release, which is the drift this comment is warning about.
func DefaultLimits() Limits {
	return Limits{
		Model:         "GZ302",
		TDPMin:        5,
		TDPMaxSafe:    75,
		TDPMaxForced:  93,
		HighTDPMinPWM: 127, // FloorCurve's bottom: 50% of PWMMax
		TempMin:       35,
		TempMax:       105,
		// cli.HighTDPFanCurve: 50% at idle temperatures, full speed by 80°C.
		// The ramp is what protects the APU; the bottom is what keeps it quiet.
		FloorCurve: []api.FanCurvePoint{
			{Temp: 30, PWM: 127},
			{Temp: 40, PWM: 127},
			{Temp: 50, PWM: 140},
			{Temp: 60, PWM: 165},
			{Temp: 65, PWM: 190},
			{Temp: 70, PWM: 215},
			{Temp: 75, PWM: 235},
			{Temp: 80, PWM: 255},
		},
		StockProfilePPT: map[string]api.TDPState{
			"quiet":       {PL1SPL: 40, PL2SPPT: 55, FPPT: 55},
			"balanced":    {PL1SPL: 52, PL2SPPT: 71, FPPT: 70},
			"performance": {PL1SPL: 70, PL2SPPT: 86, FPPT: 86},
		},
	}
}

// Sanitized returns l with any unset field replaced by its default.
//
// This is the guard for the day the daemon serves limits over the API: a client
// newer than the daemon receives zero for fields the daemon does not know about,
// and a zero TDPMaxSafe would make every fan curve fail the floor check and every
// TDP request demand the force flag. Falling back per-field degrades gracefully
// instead of catastrophically.
//
// HighTDPMinPWM is deliberately not defaulted — zero is a legitimate value there,
// meaning a device with no fan floor at all.
func (l Limits) Sanitized() Limits {
	d := DefaultLimits()
	if l.Model == "" {
		l.Model = d.Model
	}
	if l.TDPMin <= 0 {
		l.TDPMin = d.TDPMin
	}
	if l.TDPMaxSafe <= 0 {
		l.TDPMaxSafe = d.TDPMaxSafe
	}
	if l.TDPMaxForced <= 0 {
		l.TDPMaxForced = d.TDPMaxForced
	}
	if l.TempMin <= 0 {
		l.TempMin = d.TempMin
	}
	if l.TempMax <= 0 {
		l.TempMax = d.TempMax
	}
	if len(l.StockProfilePPT) == 0 {
		l.StockProfilePPT = d.StockProfilePPT
	}

	// Ordering and width invariants, not just presence. A per-field default fixes
	// a value the daemon never sent; these catch values it sent that cannot be
	// true together, which a zero check cannot see.
	//
	// Each group falls back whole rather than nudging one field, because an
	// inconsistent triple does not say which of its members is the wrong one.
	if l.TDPMin >= l.TDPMaxSafe || l.TDPMaxSafe > l.TDPMaxForced {
		l.TDPMin, l.TDPMaxSafe, l.TDPMaxForced = d.TDPMin, d.TDPMaxSafe, d.TDPMaxForced
	}

	// The temperature axis has to be wide enough for the curve's points to hold
	// strictly increasing temperatures. Below that EnforceCurve cannot satisfy
	// both monotonicity and the bounds, and emits points under TempMin that the
	// daemon rejects; at TempMin == TempMax the editor's coordinate mapping
	// divides by zero and every point lands on a NaN. The invariant was asserted
	// in the tests but never enforced, so it held only for limits compiled in.
	if l.TempMax-l.TempMin < CurvePoints-1 {
		l.TempMin, l.TempMax = d.TempMin, d.TempMax
	}

	// The floor group. The curve is authoritative and the scalar is its bottom;
	// they are repaired together so display text derived from one can never
	// disagree with clamping derived from the other.
	if l.HighTDPMinPWM < PWMMin {
		l.HighTDPMinPWM = PWMMin
	}
	if l.HighTDPMinPWM > PWMMax {
		l.HighTDPMinPWM = PWMMax
	}
	l.FloorCurve = sanitizedFloor(l.FloorCurve, l.HighTDPMinPWM)
	if len(l.FloorCurve) > 0 {
		l.HighTDPMinPWM = l.FloorCurve[0].PWM
	}
	return l
}

// sanitizedFloor returns a well-formed copy of floor: PWMs inside the hwmon
// range, temperatures strictly increasing, PWMs non-decreasing. A malformed
// curve does not say which of its points is the wrong one, so it falls back
// whole — to a flat floor at the scalar minimum when one is declared, else to
// no floor at all. An empty floor with a positive scalar is a description from
// before per-point floors existed (or a daemon serving only the scalar); the
// flat synthesis keeps the editor holding that line rather than dropping the
// constraint. Zero scalar with no curve stays "no floor" — zero is legitimate.
func sanitizedFloor(floor []api.FanCurvePoint, minPWM int) []api.FanCurvePoint {
	flat := func() []api.FanCurvePoint {
		if minPWM <= PWMMin {
			return nil
		}
		// A single point is flat everywhere under FloorPWMAt's semantics.
		return []api.FanCurvePoint{{Temp: 0, PWM: minPWM}}
	}
	if len(floor) == 0 {
		return flat()
	}
	out := make([]api.FanCurvePoint, len(floor)) // copy: never mutate the caller's slice
	copy(out, floor)
	for i := range out {
		if out[i].PWM < PWMMin {
			out[i].PWM = PWMMin
		}
		if out[i].PWM > PWMMax {
			out[i].PWM = PWMMax
		}
		if i > 0 && (out[i].Temp <= out[i-1].Temp || out[i].PWM < out[i-1].PWM) {
			return flat()
		}
	}
	return out
}

// BasicSliderMax is the ceiling of the drawer's single-slider basic view.
//
// Presentation policy, but derived rather than fixed: it only means anything as
// "a little under the safe max". A hardcoded 70 would be nonsense on a device
// whose safe sustained limit is 54.
func (l Limits) BasicSliderMax() int {
	const headroom = 5
	if m := l.TDPMaxSafe - headroom; m > l.TDPMin {
		return m
	}
	return l.TDPMin
}

// ForceRequired reports whether a TDP request needs the force flag, which the
// daemon demands for a sustained limit above TDPMaxSafe.
func (l Limits) ForceRequired(pl1 int) bool {
	return pl1 > l.TDPMaxSafe
}

// ActiveFloor returns the floor curve in force for the applied sustained limit:
// nil while pl1 is at or below TDPMaxSafe, or when the device declares no floor.
//
// While the floor is in force the daemon rejects any curve containing a point
// below the floor at that point's temperature, and refuses a fan reset outright
// — firmware auto has no floor at all, so releasing the fans there would remove
// the very protection the power limit requires. Resetting the TDP is the way
// back out.
func (l Limits) ActiveFloor(pl1 int) []api.FanCurvePoint {
	if pl1 <= l.TDPMaxSafe || len(l.FloorCurve) == 0 {
		return nil
	}
	return l.FloorCurve
}

// FanFloorPWM returns the lowest fan PWM the daemon will accept anywhere on the
// curve given the applied sustained limit — the active floor's bottom — or
// PWMMin when unconstrained. Per-point clamping goes through ActiveFloor and
// FloorPWMAt; this scalar remains for "is a floor in force" gates and for
// display text about the floor's minimum.
func (l Limits) FanFloorPWM(pl1 int) int {
	if f := l.ActiveFloor(pl1); len(f) > 0 {
		return f[0].PWM
	}
	return PWMMin
}

// FloorPWMAt evaluates a floor curve at a temperature, mirroring the daemon's
// semantics exactly (z13ctl cli.FloorPWMAt): below the first point it returns
// that point's PWM — a floor does not taper off at low temperature — above the
// last point it returns the last PWM, and between points it interpolates
// linearly. An empty floor is PWMMin everywhere. The mirroring is the point:
// what this accepts and what the daemon accepts must be the same set of curves.
func FloorPWMAt(floor []api.FanCurvePoint, temp int) int {
	if len(floor) == 0 {
		return PWMMin
	}
	if temp <= floor[0].Temp {
		return floor[0].PWM
	}
	for i := 1; i < len(floor); i++ {
		if temp > floor[i].Temp {
			continue
		}
		lo, hi := floor[i-1], floor[i]
		span := hi.Temp - lo.Temp
		if span <= 0 {
			return hi.PWM
		}
		return lo.PWM + (hi.PWM-lo.PWM)*(temp-lo.Temp)/span
	}
	return floor[len(floor)-1].PWM
}

// IsStockPPT reports whether a TDP reading matches some stock profile's firmware
// defaults exactly, meaning the user has not diverged from what the firmware
// would set on its own.
//
// An exact match on a genuinely user-chosen triple is possible but harmless: it
// only means the drawer offers the basic view for values the basic view would
// reproduce unchanged.
func (l Limits) IsStockPPT(t api.TDPState) bool {
	for _, s := range l.StockProfilePPT {
		if t.PL1SPL == s.PL1SPL && t.PL2SPPT == s.PL2SPPT && t.FPPT == s.FPPT {
			return true
		}
	}
	return false
}

// NeedsAdvanced reports whether a TDP state can only be shown accurately in the
// drawer's advanced view.
//
// Basic mode is a single slider that applies one value to all three power limits
// and stops at BasicSliderMax, so it cannot represent either a sustained limit
// above that ceiling or a state where the three limits differ. Showing such a
// state in basic mode would clamp the slider and misreport the hardware — and
// worse, a subsequent save would send the clamped value and quietly lower the
// limit.
//
// Only settings the user actually chose count, which takes two checks rather than
// one. On a stock profile the daemon reports that profile's own PPT defaults,
// whose limits legitimately differ — balanced is 52/71/70 — so isCustom must be
// true (the caller passes api.State.InCustomProfile(), which covers named custom
// profiles as well as the default "custom"). But saving a fan curve or an
// undervolt is enough to flip the daemon to a custom profile on its own, leaving
// the power limits at the firmware's values, so the reading must also differ
// from the stock defaults.
//
// A basic save round-trips as PL1 == PL2 == FPPT, because the daemon defaults the
// blank PL fields to the single value, so an equal triple never trips this.
func (l Limits) NeedsAdvanced(isCustom bool, t api.TDPState) bool {
	if !isCustom || l.IsStockPPT(t) {
		return false
	}
	if t.PL1SPL > l.BasicSliderMax() {
		return true
	}
	return t.PL1SPL != t.PL2SPPT || t.PL2SPPT != t.FPPT
}

// FanCurveIsCustom reports whether a fan curve reported by the daemon is actually
// in force, and therefore worth displaying.
//
// The curve registers keep the last written points even after the fans are
// released to firmware auto, so the points alone cannot tell you anything:
// switching to a stock profile resets the mode to FanModeAuto but leaves the old
// custom points perfectly readable. Drawing them then shows the user a curve the
// firmware is not following.
func FanCurveIsCustom(fc *api.FanCurveState) bool {
	return fc != nil && fc.Mode == FanModeCustom && len(fc.Points) == CurvePoints
}

// Curve is an 8-point fan curve, ordered by ascending temperature.
type Curve [CurvePoints]api.FanCurvePoint

// DefaultCurve returns the curve shown before the daemon reports one, fitted to
// this device's temperature range.
//
// The shape is hand-tuned for the Z13 and is returned unchanged there. On a
// device with a narrower range EnforceCurve pulls it into bounds; the result is
// no longer hand-tuned, but it is valid, which is what matters for a placeholder.
func (l Limits) DefaultCurve() Curve {
	c := Curve{
		{Temp: 35, PWM: 0},
		{Temp: 45, PWM: 25},
		{Temp: 50, PWM: 50},
		{Temp: 60, PWM: 80},
		{Temp: 70, PWM: 120},
		{Temp: 80, PWM: 170},
		{Temp: 90, PWM: 220},
		{Temp: 100, PWM: 255},
	}
	l.EnforceCurve(&c, 0, nil)
	return c
}

// String renders the curve in the daemon's "temp:pwm,temp:pwm,..." wire format.
func (c Curve) String() string {
	parts := make([]string, 0, len(c))
	for _, p := range c {
		parts = append(parts, fmt.Sprintf("%d:%d", p.Temp, p.PWM))
	}
	return strings.Join(parts, ",")
}

// EnforceCurve repairs the curve after point idx has been moved, so that it
// always satisfies what the firmware and daemon require:
//
//   - temperatures strictly increase
//   - PWM never decreases
//   - every point sits within [minPWM, PWMMax] and [TempMin, TempMax]
//
// floor comes from ActiveFloor: nil when unconstrained, the device's floor
// curve while a high sustained limit is applied. Each point's PWM is held at or
// above the floor at that point's own temperature (FloorPWMAt) — the daemon
// measures curves the same way, so a drag the editor allows is a curve the
// daemon accepts. Passing the floor in rather than deriving it here keeps the
// rule in one place and makes the clamping directly testable with and without
// a floor.
//
// The moved point is clamped first so it wins over its neighbours, then the
// change cascades outward in both directions, then a final pass re-clamps
// everything — cascading can push a neighbour past a bound.
//
// The moved point's temperature is clamped into a range that leaves room for the
// points on either side: each of the idx points below it needs at least one
// degree, as does each of the points above. Clamping it to the raw TempMin
// instead would push its left-hand neighbours below the minimum, and the final
// clamp would then pile them all onto TempMin — producing duplicate temperatures
// that are not strictly increasing.
func (l Limits) EnforceCurve(c *Curve, idx int, floor []api.FanCurvePoint) {
	if idx < 0 || idx >= len(c) {
		return
	}
	clamp := func(p *api.FanCurvePoint) {
		if p.Temp < l.TempMin {
			p.Temp = l.TempMin
		}
		if p.Temp > l.TempMax {
			p.Temp = l.TempMax
		}
		// Temperature first: the floor depends on where the point ends up.
		if floorMin := FloorPWMAt(floor, p.Temp); p.PWM < floorMin {
			p.PWM = floorMin
		}
		if p.PWM > PWMMax {
			p.PWM = PWMMax
		}
	}

	clamp(&c[idx])
	if lo := l.TempMin + idx; c[idx].Temp < lo {
		c[idx].Temp = lo
	}
	if hi := l.TempMax - (len(c) - 1 - idx); c[idx].Temp > hi {
		c[idx].Temp = hi
	}

	// Temperatures must strictly increase.
	for i := idx + 1; i < len(c); i++ {
		if c[i].Temp <= c[i-1].Temp {
			c[i].Temp = c[i-1].Temp + 1
		}
	}
	for i := idx - 1; i >= 0; i-- {
		if c[i].Temp >= c[i+1].Temp {
			c[i].Temp = c[i+1].Temp - 1
		}
	}

	// PWM must not decrease.
	for i := idx + 1; i < len(c); i++ {
		if c[i].PWM < c[i-1].PWM {
			c[i].PWM = c[i-1].PWM
		}
	}
	for i := idx - 1; i >= 0; i-- {
		if c[i].PWM > c[i+1].PWM {
			c[i].PWM = c[i+1].PWM
		}
	}

	for i := range c {
		clamp(&c[i])
	}

	// That clamp can collapse several points onto the same bound — a curve
	// carried over from a device with a wider temperature range, for instance,
	// where every point above the new maximum lands on it. Re-spread so the
	// strictly-increasing invariant holds for any input, not just for curves that
	// were already valid. Clamping PWM needs no equivalent: a monotone clamp
	// preserves ordering.
	for i := 1; i < len(c); i++ {
		if c[i].Temp <= c[i-1].Temp {
			c[i].Temp = c[i-1].Temp + 1
		}
	}
	// The forward spread can push the tail past the maximum; pull it back from
	// the end. There is always room because TempMax-TempMin is at least
	// CurvePoints-1 for any sane device.
	if last := len(c) - 1; c[last].Temp > l.TempMax {
		c[last].Temp = l.TempMax
	}
	for i := len(c) - 2; i >= 0; i-- {
		if c[i].Temp >= c[i+1].Temp {
			c[i].Temp = c[i+1].Temp - 1
		}
	}

	// The re-spread moved temperatures, and the floor depends on temperature, so
	// re-assert it at the final positions. Raising to a floor that never
	// decreases with temperature cannot break the non-decreasing PWM order.
	for i := range c {
		if floorMin := FloorPWMAt(floor, c[i].Temp); c[i].PWM < floorMin {
			c[i].PWM = floorMin
		}
	}
}
