package safety

// engine.go — the fail-closed apply and release orderings, bound to one
// device's fan and power drivers.

import (
	"errors"
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
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
	fans  Fans
	power Power
}

// NewEngine binds a fan controller and a power limiter. fans may be nil on a
// device with no fan control (the floor then cannot be written, so a limit
// that needs it is refused).
func NewEngine(fans Fans, power Power) *Engine {
	return &Engine{fans: fans, power: power}
}

// PlanWrites is what the power limiter would write for s, for a dry run
// (driver.WritePlanner); nil when it cannot say.
func (e Engine) PlanWrites(s api.TDPState) []string {
	if p, ok := e.power.(driver.WritePlanner); ok {
		return p.PlanWrites(s)
	}
	return nil
}

// Forget drops whatever the power limiter caches about what it last wrote, so
// the next write goes to hardware (driver.PowerForgetter). After a resume
// nothing knows whether the limits survived s2idle.
func (e Engine) Forget() {
	if f, ok := e.power.(driver.PowerForgetter); ok {
		f.Forget()
	}
}

// Read returns the current power limits from hardware.
func (e Engine) Read() (api.TDPState, error) {
	return e.power.Read()
}

// ReadEffective returns the current power limits, substituting the envelope's
// stock row when hardware reports the stale boot cache (CacheStale). The PPT
// attributes come up holding the interface's initial values after module load —
// 5W on asus-nb-wmi, asus-armoury's defaults — and do not reflect the EC's real
// per-profile limits until something writes them, so a raw read straight after
// boot is wrong in the most alarming way possible.
//
// profile must be the *effective* profile — for daemon callers the daemon's
// own state ("custom", or a custom profile's name, when one is active), NOT
// the raw platform_profile value. platform_profile never names a custom
// profile, so passing it would make a legitimate minimum-watts custom TDP
// indistinguishable from the stale cache and report the stock table instead.
// Any profile name absent from the envelope's StockProfilePPT disables the
// fallback, which is exactly what a custom profile needs.
func (e Engine) ReadEffective(profile string) (api.TDPState, error) {
	s, err := e.power.Read()
	if err != nil {
		return s, err
	}
	// The envelope only when a stock row could replace the reading: a driver
	// may read the kernel to build it (asus-armoury's bounds, each a live ACPI
	// call), and a custom profile — what the reconcile watcher reads every two
	// seconds — never substitutes. The row itself is device data, and the same
	// in every envelope a driver reports.
	if _, ok := e.DeviceEnvelope().StockProfilePPT[profile]; !ok {
		return s, nil
	}
	if env := e.power.Envelope(); CacheStale(env, s) {
		return env.StockProfilePPT[profile], nil
	}
	return s, nil
}

// ReadDisplayed returns the power limits to *show* for profile: a firmware
// profile's stock row from env, as the interface would hold it
// (EffectiveTDP: clamped to its ranges, APU/Platform sPPT zeroed where it has
// neither), and for anything else (a custom profile, or a profile env has no
// row for) the reading ReadEffective gives. A firmware profile reads no
// hardware; env is the caller's, so a cached envelope keeps it that way.
//
// On a firmware profile the readback is not the limit in force. The firmware
// applies its own limits on every platform_profile write and fan release, while
// the attributes go on showing the last value written (issue #22). On
// asus-armoury it is worse: current_value is the kernel's cache of the last
// value written while on the current power source. A custom profile that ran
// on battery therefore leaves its limit in the battery table, and a later
// switch to a firmware profile on AC never overwrites it. Measured on a GZ302EA
// (2026-11-15): balanced on battery read 35/35/45, a custom profile's value,
// while RAPL held 47.1 W under load. The row is the device's statement of what
// the profile runs at, and what HandBackToFirmware's row write leaves in the
// readback. Its PL1 matches the firmware on AC.
//
// The trade: a limit written out of band while a firmware profile is active
// (asusctl, a raw sysfs write) is no longer shown. The readback was never
// evidence of it either.
//
// This is for display only. Anything that decides whether the fans may be
// released must see the hardware, however stale, which is why
// CheckFanFloorRelease reads through ReadEffective.
func (e Engine) ReadDisplayed(env driver.PowerEnvelope, profile string) (api.TDPState, error) {
	if row, ok := env.StockProfilePPT[profile]; ok {
		return EffectiveTDP(env, row), nil
	}
	return e.ReadEffective(profile)
}

// DeviceEnvelope is the envelope from device data alone where the driver can
// give it without reading the kernel (driver.DeviceEnvelope), and the full
// envelope where it cannot. It is what the daemon serves while the EC is not
// answering, when the kernel's ranges must not be read.
func (e Engine) DeviceEnvelope() driver.PowerEnvelope {
	if de, ok := e.power.(driver.DeviceEnvelope); ok {
		return de.DeviceEnvelope()
	}
	return e.power.Envelope()
}

// CheckFanFloorRelease reports whether the fans may be released to firmware
// auto — i.e. whether a fan curve reset is allowed — judged against the
// sustained limit hardware currently reports for the effective profile.
// Firmware auto is precisely what the envelope's floor exists to override, so
// dropping to it while the limit is high would remove the thermal floor the
// limit requires; lowering the limit first remains the way out.
//
// A PPT read failure is deliberately not a refusal: the guard is best-effort
// and must not make fan control unavailable when sysfs cannot be read at all.
// The pure CheckFanFloorReleaseAt is the variant for a profile that is not
// running, whose limit hardware knows nothing about.
func (e Engine) CheckFanFloorRelease(profile string) error {
	tdp, err := e.ReadEffective(profile)
	if err != nil {
		return nil
	}
	return CheckFanFloorReleaseAt(e.power.Envelope(), tdp.PL1SPL)
}

// RestoreStock writes the envelope's stock PPT row for a firmware profile
// verbatim, and nothing else. The row is looser than the firmware's own limits
// on the Z13, so on its own it is not how a stock profile is entered —
// HandBackToFirmware is, which follows it with the release that re-applies the
// firmware's limits.
//
// It is a raw write on purpose: stock rows are the firmware's own defaults,
// at or below the safe maximum, so no floor decision applies. A profile with
// no row in the envelope is an error — the caller decides whether that means
// "nothing to do" or "the limit did not come down".
func (e Engine) RestoreStock(profile string) error {
	stock, ok := e.power.Envelope().StockProfilePPT[profile]
	if !ok {
		return fmt.Errorf("no stock PPT row for profile %q", profile)
	}
	return e.power.Apply(stock)
}

// HandBackToFirmware puts a firmware profile's own power limits back in force:
// it writes the profile's stock row and then releases the fans, whose release
// makes the firmware re-apply that profile's limits.
//
// The row must never be the last write. Its PL1 matches the firmware's, but the
// limits behind it do not: measured on a GZ302EA under load, balanced held 52 W
// on the firmware's own limits and 63–66 W for the whole minute after the row
// was written, and quiet 40 W against 55–70 W. Writing the row alone — which is
// what every daemon start on a stock profile did — ran each stock profile hotter
// than the firmware runs it. It is still written first, for two reasons: it
// lowers a high custom limit before the fans lose their floor, and it is what
// the PPT attributes show afterwards rather than a stale custom value.
//
// If the row cannot be written and hardware still reports a sustained limit
// that needs the floor, the fans are not released, as in every other release
// path. A profile with no row (an unreadable platform_profile) skips the write
// and still releases. A device with no fan control writes the row and stops.
func (e Engine) HandBackToFirmware(profile string) error {
	var rowErr error
	if _, ok := e.power.Envelope().StockProfilePPT[profile]; ok {
		if err := e.RestoreStock(profile); err != nil {
			rowErr = fmt.Errorf("writing the %s stock PPT row: %w", profile, err)
		}
	}
	if err := e.CheckFanFloorRelease(profile); err != nil {
		return errors.Join(rowErr, err)
	}
	return errors.Join(rowErr, e.ReleaseFans(nil))
}

// Envelope returns the device's power envelope.
func (e Engine) Envelope() driver.PowerEnvelope {
	return e.power.Envelope()
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
	env := e.power.Envelope()
	if c := FanCurveForTDP(env, s.PL1SPL, want); c != nil {
		// A floor with no fan control cannot be satisfied, only refused. The
		// device registry validates this pairing out of existence; the guard is
		// for an engine assembled by hand.
		if e.fans == nil {
			return fmt.Errorf("refusing to apply %dW sustained TDP: the device declares a fan floor but no fan control", s.PL1SPL)
		}
		if err := e.fans.ApplyCurve(c); err != nil {
			return fmt.Errorf("setting high-TDP fan curve: %w (refusing to apply %dW sustained TDP without the %d PWM floor)",
				err, s.PL1SPL, env.FloorCurve[0].PWM)
		}
	}
	return e.power.Apply(s)
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
	if err := e.power.Apply(stock); err != nil {
		return fmt.Errorf("lowering power limits: %w (fans stay on the floor until the limit comes down)", err)
	}
	if e.fans == nil {
		return nil // power-only device: nothing to release
	}
	return e.fans.Release()
}

// ReleaseFans hands the fans back to firmware auto and then re-applies keep —
// the power limit that should be in force — and is the only way anything outside
// this package may release them.
//
// The re-apply is not optional, and keep is a parameter rather than a separate
// call so that no caller can forget it. On the Z13 any pwm_enable=2 write to the
// curve device — even a redundant one, with the fans already on auto — makes the
// firmware re-apply the active platform profile's own power limits, discarding a
// custom TDP, while the ppt_* attributes go on showing the value that was
// written. Measured on a GZ302EA under load (issue #22): TDP 30 W on performance
// held 30 W until the release, then 70 W. Every curveless profile at or below the
// safe maximum released its fans right after writing its TDP, which is why "75 W
// does nothing, 76 W works" — above it the floor is kept and nothing is released.
//
// keep == nil means "the firmware profile's own limits", which is what a switch
// to a stock profile wants. A keep that needs the floor is refused before the
// fans are touched, by the same rule CheckFanFloorRelease applies — the mirror of
// ApplyTDPSafely's fail-closed ordering. ReleaseTDP is the other release: it
// lowers to stock first and then releases, landing on the firmware's own limits.
func (e Engine) ReleaseFans(keep *api.TDPState) error {
	if keep != nil && e.power != nil {
		if err := CheckFanFloorReleaseAt(e.power.Envelope(), keep.PL1SPL); err != nil {
			return err
		}
	}
	if e.fans == nil {
		return nil // power-only device: nothing to release, so nothing reset
	}
	if err := e.fans.Release(); err != nil {
		return err
	}
	if keep == nil || e.power == nil {
		return nil
	}
	if err := e.power.Apply(*keep); err != nil {
		return fmt.Errorf("fans released, but re-applying the %dW power limit failed "+
			"(the firmware profile's own limit is in force): %w", keep.PL1SPL, err)
	}
	return nil
}
