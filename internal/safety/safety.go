// Package safety holds the thermal-safety rules that sit between command
// handlers and hardware drivers — above them, deliberately: a driver reports
// its envelope and performs writes it is handed, and this package decides what
// may be written. Every function is pure and parameterized by
// driver.PowerEnvelope, so the rules are testable without hardware and hold
// for any device that declares a floor, not just the Z13 whose constants they
// were extracted from.
//
// The one rule everything here serves: while a sustained power limit exceeds
// the device's safe maximum, the fans must hold the device's floor curve, and
// no code path — apply, edit, reset, reconcile — may leave the machine at a
// high limit without it. The functions are the pure halves of that rule; the
// hardware-reading halves (and the fail-closed apply) remain with the code
// that owns sysfs access until the driver extraction completes.
package safety

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// FanCurveForTDP returns the curve that must be in force for a sustained limit
// of pl1, given the curve the caller intends to run:
//
//   - nil when pl1 needs no floor at all — at or below the envelope's safe
//     maximum, unreadable (-1), or on a device that declares no floor. "The
//     limit imposes nothing; run whatever you were going to run."
//   - want raised point-by-point to the envelope's floor curve, so each point
//     ends at whichever of the two is *higher*. A point the user set above the
//     floor is theirs and is left alone; a point below it comes up. Nothing is
//     ever lowered.
//   - a copy of the floor curve itself when there is no want at all — nothing
//     to raise, so the floor is the only thing left to write.
//
// The floor is the whole curve, not its scalar bottom, and that distinction is
// load-bearing. The rationale for lowering the Z13's minimum from 204 to 127
// was that "the *ramp* is what protects the APU, since a machine actually
// sustaining >75W is well past 60°C where the curve is far above the floor
// anyway". Clamping to the scalar alone honoured the first half and threw away
// the second: a curve flat at 127 satisfied it everywhere, so 93W sustained at
// 90°C ran the fans at 50% where every earlier path would have reached 100%.
//
// Raising preserves the monotonically non-decreasing PWM order ParseFanCurve
// requires, because FloorPWMAt is itself non-decreasing in temperature and the
// pointwise max of two non-decreasing sequences is non-decreasing.
// Temperatures are the user's throughout — only PWM values move.
//
// The floor is evaluated at each point's *temperature*, via FloorPWMAt, not at
// the matching slice index. Index matching was the first attempt and is wrong
// whenever the user's temperatures differ from the floor's: a curve of
// 70:130,75:135,80:140,… clears every index-matched comparison and still runs
// 55% fans at 80°C, where the Z13's floor demands 100%. Since the whole
// justification is stated in temperature terms, the comparison has to be too.
//
// want is never mutated: it aliases the saved profile in daemon state, and
// raising in place would rewrite the user's stored curve. The no-want return
// is a copy for the same reason in reverse — the caller hands the result to a
// fan write and must not be able to reach the envelope's own slice through it.
//
// This is the single place the rule lives. The apply path and the reconcile
// watcher both call it; they used to carry separate copies that disagreed, and
// the apply path's copy was wrong — it treated the floor as an override and
// replaced *every* curve above the safe maximum. A user curve of 204→255 came
// back as the 127→255 ramp, and a curve at 100% everywhere was downgraded to
// one that idles at 50%. The watcher would not correct it either: the fans
// were left in mode 1, which reads as "the curve is live".
func FanCurveForTDP(env driver.PowerEnvelope, pl1 int, want []api.FanCurvePoint) []api.FanCurvePoint {
	if pl1 <= env.TDPMaxSafe || len(env.FloorCurve) == 0 {
		return nil
	}
	if len(want) == 0 {
		out := make([]api.FanCurvePoint, len(env.FloorCurve))
		copy(out, env.FloorCurve)
		return out
	}
	out := make([]api.FanCurvePoint, len(want))
	copy(out, want)
	for i := range out {
		if lowest := FloorPWMAt(env.FloorCurve, out[i].Temp); out[i].PWM < lowest {
			out[i].PWM = lowest
		}
	}
	return out
}

// FloorPWMAt returns the minimum PWM floor requires at temp, reading it as the
// piecewise-linear curve the EC treats it as.
//
// Below the curve's first point it returns that point's PWM — the floor is a
// floor, so it does not taper off at low temperature — and above the last
// point it returns the last PWM. Between points it interpolates, so a user
// point at 55°C is measured against roughly halfway between the 50°C and 60°C
// values rather than against whichever floor point happens to share its index.
// An empty floor requires nothing anywhere.
func FloorPWMAt(floor []api.FanCurvePoint, temp int) int {
	if len(floor) == 0 {
		return 0
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

// FloorAdjustsCurve reports whether FanCurveForTDP changes anything about want
// — either raising one or more points to the floor curve, or writing the floor
// whole because there is no want at all.
//
// Callers use it to tell the user the floor altered what they asked for, and
// just as importantly to stay quiet when it did not. It is derived from
// FanCurveForTDP rather than reimplementing the comparison, so the two cannot
// disagree about what counts as an adjustment — which is how a scalar version
// once came to answer "no problem" for a curve the ramp does raise.
//
// An absent curve counts as adjusted; CheckCurveAgainstTDP alone answers "no
// problem" for nil, because a curve with no points has none below the floor.
func FloorAdjustsCurve(env driver.PowerEnvelope, pl1 int, want []api.FanCurvePoint) bool {
	got := FanCurveForTDP(env, pl1, want)
	if got == nil {
		return false // the limit imposes nothing
	}
	if len(want) != len(got) {
		return true // no curve of its own, so the whole floor curve was written
	}
	for i := range got {
		if got[i] != want[i] {
			return true
		}
	}
	return false
}

// CheckCurveAgainstTDP rejects a curve holding any point below the floor while
// pl1 is above the envelope's safe maximum. It reads nothing, which is what
// makes it usable for a custom profile that is not currently applied: hardware
// says nothing about a profile that is not running, so the limit to check
// against is the one stored in the same profile.
//
// Applying that check when a profile is *edited* means no profile can be saved
// in a state that would be unsafe when it is later activated. The fail-closed
// apply still guards activation; this is the earlier, friendlier refusal.
//
// It measures against FloorPWMAt — the same floor FanCurveForTDP raises to —
// and not against the floor's scalar bottom. Using the scalar left
// `fancurve --set` as an open door around the apply-time rule: a curve flat at
// 127 clears 127 everywhere, so the handler accepted it and wrote it verbatim,
// and the reconcile watcher then read pwm_enable=1 as "the curve is live" and
// never corrected it. The machine sustained 93W at 90°C on 50% fans — the
// exact failure FanCurveForTDP was rewritten to prevent, reached through the
// one write path that did not consult it.
func CheckCurveAgainstTDP(env driver.PowerEnvelope, points []api.FanCurvePoint, pl1 int) error {
	if pl1 <= env.TDPMaxSafe || len(env.FloorCurve) == 0 {
		return nil
	}
	first := env.FloorCurve[0]
	last := env.FloorCurve[len(env.FloorCurve)-1]
	for _, p := range points {
		if lowest := FloorPWMAt(env.FloorCurve, p.Temp); p.PWM < lowest {
			return fmt.Errorf("PWM %d at %d°C is below the %d required there when sustained TDP is above %dW "+
				"(the floor rises with temperature, from %d at %d°C to %d at %d°C)",
				p.PWM, p.Temp, lowest, env.TDPMaxSafe, first.PWM, first.Temp, last.PWM, last.Temp)
		}
	}
	return nil
}

// CheckFanFloorReleaseAt reports whether the fans may be released to firmware
// auto — i.e. whether a fan curve reset is allowed — against a known sustained
// limit. It refuses while pl1 is above the envelope's safe maximum on a device
// that declares a floor, since firmware auto is precisely what the floor
// exists to override; dropping to it would remove the thermal floor while the
// power limit that requires it is still in force. Lowering the limit first
// remains the way out.
//
// Taking the limit as a parameter is what a custom profile that is not
// currently applied has to be checked against: hardware says nothing about a
// profile that is not running, and clearing the curve from a profile that
// keeps a high limit would be unsafe the moment that profile is activated.
func CheckFanFloorReleaseAt(env driver.PowerEnvelope, pl1 int) error {
	if pl1 <= env.TDPMaxSafe || len(env.FloorCurve) == 0 {
		return nil
	}
	return fmt.Errorf("sustained TDP is %dW (above %dW), so fans must stay at or above %d PWM; lower it first with 'voltaire tdp --reset'",
		pl1, env.TDPMaxSafe, env.FloorCurve[0].PWM)
}
