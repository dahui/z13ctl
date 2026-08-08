package safety

// engine.go — the fail-closed apply and release orderings, bound to one
// device's fan and power drivers.

import (
	"fmt"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/driver"
)

// Fans is the slice of driver.FanController the engine needs. Declared here,
// consumer-side, so the engine can be driven by the full driver interface or
// by a package's own adapter equally — any implementation must keep
// FanController's verify-after-write contracts, which the fail-closed
// reasoning below depends on.
type Fans interface {
	ApplyCurve(pts []api.FanCurvePoint) error
	Release() error
}

// Power is the slice of driver.PowerLimiter the engine needs. Apply is the raw
// limit write; the engine existing is what keeps it out of everyone else's
// reach.
type Power interface {
	Read() (api.TDPState, error)
	Apply(api.TDPState) error
	Envelope() driver.PowerEnvelope
}

// Engine binds one device's fan controller and power limiter and owns the two
// orderings that keep a machine from ever sitting at a high power limit
// without its fan floor:
//
//   - raising: fans first, then power (ApplyTDPSafely) — and no power write at
//     all if the fans refuse.
//   - lowering: power first, then fans (ReleaseTDP) — the mirror image, so the
//     floor outlives the limit rather than the limit outliving the floor.
//
// The device registry hands callers an Engine instead of the raw
// driver.PowerLimiter, so these orderings are not a convention callers follow
// but the only route that exists.
type Engine struct {
	Fans  Fans
	Power Power
}

// Read returns the current power limits from hardware.
func (e Engine) Read() (api.TDPState, error) {
	return e.Power.Read()
}

// Envelope returns the device's power envelope.
func (e Engine) Envelope() driver.PowerEnvelope {
	return e.Power.Envelope()
}

// ApplyTDPSafely writes s, first putting the fans into the state the sustained
// limit requires — see FanCurveForTDP, which decides between want and the
// envelope's floor. If that fan write fails, the TDP is NOT applied:
// sustaining more than the safe maximum without the floor is the exact
// condition the floor exists to prevent, so failing closed is the only safe
// outcome.
//
// want is the curve the caller intends to have in force — the active profile's
// own curve, or nil when it has none. Passing it is what keeps the user's own
// points from being thrown away: only points below the floor are raised.
// Passing nil asks for the whole floor curve, which is right only when there
// is genuinely no curve.
//
// "Fails" includes the driver accepting the write and then the kernel dropping
// the curve: FanController.ApplyCurve must read back and verify, so a floor
// lost to a concurrent platform_profile write is a refusal rather than a false
// success. This function only guarantees the floor at the moment the limit is
// raised — keeping it in force afterwards is the reconcile watcher's job,
// since a later profile write would otherwise return the fans to firmware auto
// while the power limit stays high.
//
// This is the single entry point for every path that applies a custom TDP —
// the socket handler, the "custom" profile recall, daemon startup, resume, and
// the no-daemon CLI path. They previously enforced the floor four different
// ways, including one that raised power before raising the fans and discarded
// the fan error.
//
// Values are written verbatim; resolve mirrored fields (APU/Platform sPPT)
// before calling.
func (e Engine) ApplyTDPSafely(s api.TDPState, want []api.FanCurvePoint) error {
	env := e.Power.Envelope()
	if c := FanCurveForTDP(env, s.PL1SPL, want); c != nil {
		// A floor with no fan control cannot be satisfied, only refused. The
		// device registry validates this pairing out of existence; the guard is
		// for an engine assembled by hand.
		if e.Fans == nil {
			return fmt.Errorf("refusing to apply %dW sustained TDP: the device declares a fan floor but no fan control", s.PL1SPL)
		}
		if err := e.Fans.ApplyCurve(c); err != nil {
			return fmt.Errorf("setting high-TDP fan curve: %w (refusing to apply %dW sustained TDP without the %d PWM floor)",
				err, s.PL1SPL, env.FloorCurve[0].PWM)
		}
	}
	return e.Power.Apply(s)
}

// ReleaseTDP lowers the power limits to stock and only then releases the fans
// to firmware auto — the mirror image of ApplyTDPSafely's ordering, so the
// machine is never at a high limit with no floor. If the power write fails the
// fans are NOT released, for the same fail-closed reason: the limit that
// requires the floor is still in force.
//
// The caller chooses stock (the underlying firmware profile's row from the
// envelope's StockProfilePPT) and decides whether releasing is right at all —
// a profile with its own curve restores that curve instead of calling this.
func (e Engine) ReleaseTDP(stock api.TDPState) error {
	if err := e.Power.Apply(stock); err != nil {
		return fmt.Errorf("lowering power limits: %w (fans stay on the floor until the limit comes down)", err)
	}
	if e.Fans == nil {
		return nil // power-only device: nothing to release
	}
	return e.Fans.Release()
}
