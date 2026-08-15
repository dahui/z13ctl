// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package limits

// device.go — turning the daemon's device-get document into a Limits.
//
// This is the seam the package was built for: the drawer asks the daemon what
// the machine can do instead of compiling one laptop's numbers in. Everything
// here is a pure value conversion, so the policy that decides what a partial
// or absent answer means is testable without a daemon, a socket, or GTK.

import "github.com/dahui/voltaire/api/v2"

// FromDevice converts the daemon's device-get document into the drawer's
// Limits, already Sanitized.
//
// A nil document yields DefaultLimits. That single rule covers every way the
// answer can be missing — no daemon running, a daemon too old to know
// device-get, a malformed reply — so no call site has to decide for itself
// what "no answer" means, and the drawer always has a complete Limits to build
// widgets from. Sanitized then repairs a document that answered but answered
// partially, which is the pre-2.0-client case in reverse: a *newer* daemon
// omitting a field this build expects reads as zero, and a zero TDPMaxSafe
// would demand the force flag for every TDP the user could pick.
//
// Capability *absence* is deliberately not handled here. A nil Power or Fans
// section means the device has no such capability and its controls should be
// hidden entirely; this function fills in defaults instead, because Limits
// describes bounds and has no way to say "this control does not exist". That
// is the remaining half of the capability work and it needs a device that
// actually lacks a capability to be worth anything — today's single device has
// all of them, so filling in defaults is exactly what the drawer did before.
func FromDevice(info *api.DeviceInfo) Limits {
	if info == nil {
		return DefaultLimits()
	}

	l := Limits{Model: info.Model}

	if p := info.Power; p != nil {
		l.TDPMin, l.TDPMaxSafe, l.TDPMaxForced = p.TDPMin, p.TDPMaxSafe, p.TDPMaxForced
		l.FloorCurve = append([]api.FanCurvePoint(nil), p.FloorCurve...)

		// Seed the scalar from the curve's bottom before Sanitized runs.
		// sanitizedFloor degrades a malformed curve to "a flat floor at the
		// scalar" when one is declared and to "no floor at all" when it is
		// zero. Leaving the scalar unset would take the second branch — and
		// silently dropping a floor the daemon still enforces is precisely the
		// failure this package exists to prevent, since the drawer would then
		// offer curves the daemon refuses. A suspect floor is worth keeping;
		// no floor is not.
		if len(l.FloorCurve) > 0 {
			l.HighTDPMinPWM = l.FloorCurve[0].PWM
		}

		// Copied, not aliased: Sanitized may hand this map straight through to
		// the caller, and the document is cached for the process lifetime.
		//
		// Narrowed to the three rails the drawer compares, which is what
		// Limits.StockProfilePPT is documented to hold. The daemon's envelope
		// also carries APU and Platform sPPT — it mirrors both from PL2 — and
		// IsStockPPT ignores them. Carrying them anyway would make a fetched
		// Limits differ from DefaultLimits on fields nothing reads, which
		// costs twice: the fallback would no longer be interchangeable with
		// the fetched value, and the drift guard comparing the two would fire
		// on a difference that means nothing while saying nothing about the
		// ones that do.
		if len(p.StockProfilePPT) > 0 {
			l.StockProfilePPT = make(map[string]api.TDPState, len(p.StockProfilePPT))
			for name, t := range p.StockProfilePPT {
				l.StockProfilePPT[name] = api.TDPState{PL1SPL: t.PL1SPL, PL2SPPT: t.PL2SPPT, FPPT: t.FPPT}
			}
		}
	}

	if f := info.Fans; f != nil {
		l.TempMin, l.TempMax = f.TempMin, f.TempMax
		// Copied per preset, not just the outer slice: the document is cached
		// for the process lifetime and Sanitized hands this straight to the
		// caller, so a shared points slice would let the editor's own repairs
		// rewrite the preset it loaded from.
		for _, p := range f.Presets {
			p.Curve = append([]api.FanCurvePoint(nil), p.Curve...)
			l.Presets = append(l.Presets, p)
		}
	}

	return l.Sanitized()
}
