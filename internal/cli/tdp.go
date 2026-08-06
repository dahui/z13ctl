package cli

// tdp.go — PPT sysfs path discovery and I/O helpers for ASUS TDP control.
// Uses the asus-nb-wmi platform device attributes (NOT firmware-attributes,
// which have empty calibration data on the 2025 Z13).

import (
	"fmt"
	"os"

	"github.com/dahui/z13ctl/api"
)

// TDP safety limits in watts, derived from G-Helper's model config for the
// 2025 ROG Flow Z13 (GZ302E) and Armoury Crate custom mode limits.
const (
	TDPMin       = 5  // absolute minimum
	TDPMaxSafe   = 75 // max in Armoury Crate custom mode
	TDPMaxForced = 93 // absolute max for GZ302E (G-Helper)
	TDPDefault   = 50 // G-Helper default for Z13
)

// StockProfilePPT maps stock platform_profile names to their actual PPT values
// as measured with ryzenadj on the 2025 Z13. The kernel's sysfs PPT attributes
// are a stale cache (initialized to 5W on module load) and do not reflect the
// EC's actual per-profile limits unless explicitly written.
//
// This table is authoritative on write: switching to a stock profile writes it
// to hardware verbatim via SetTDPState, because the asus-nb-wmi PPT attributes
// have no "reset to firmware default" operation and the firmware does not
// re-apply per-profile limits on a platform_profile change. Values are measured
// on the GZ302E and will need a per-model lookup when other models are supported.
var StockProfilePPT = map[string]api.TDPState{
	"quiet":       {PL1SPL: 40, PL2SPPT: 55, FPPT: 55, APUSPPT: 70, PlatformSPPT: 70},
	"balanced":    {PL1SPL: 52, PL2SPPT: 71, FPPT: 70, APUSPPT: 70, PlatformSPPT: 70},
	"performance": {PL1SPL: 70, PL2SPPT: 86, FPPT: 86, APUSPPT: 70, PlatformSPPT: 70},
}

// ReadEffectivePPT returns the current PPT values. If sysfs returns the stale
// kernel cache (PL1 == 5) and the active profile is a known stock profile,
// the measured per-profile defaults are returned instead. This fallback still
// matters after a fresh boot, before any z13ctl profile switch has written real
// values to the attributes.
//
// profile must be the *effective* profile, which for daemon callers is the
// daemon's own state ("custom" when a custom TDP is active) — NOT the raw
// platform_profile value. platform_profile is never "custom" (it is a virtual
// profile that is deliberately not written to sysfs), so passing it would make a
// legitimate 5W custom TDP indistinguishable from the stale cache and report the
// stock table instead. Any profile name not in StockProfilePPT disables the
// fallback, which is the desired behaviour for "custom".
func ReadEffectivePPT(profile string) (api.TDPState, error) {
	s, err := ReadAllPPT()
	if err != nil {
		return s, err
	}
	if s.PL1SPL == TDPMin {
		if stock, ok := StockProfilePPT[profile]; ok {
			return stock, nil
		}
	}
	return s, nil
}

// FindPPTBasePath returns the sysfs path to the asus-nb-wmi platform device.
func FindPPTBasePath() string {
	if _, err := os.Stat(pptBasePath); err == nil {
		return pptBasePath
	}
	return pptBasePath // return default even if missing, callers handle errors
}

// FindPPTPath returns the full sysfs path for a specific PPT attribute.
func FindPPTPath(attr string) string {
	return FindPPTBasePath() + "/" + attr
}

// ReadPPT reads a single PPT value (watts) from sysfs.
func ReadPPT(attr string) (int, error) {
	return readIntFile(FindPPTPath(attr))
}

// ReadAllPPT reads all 5 PPT values and returns a TDPState.
func ReadAllPPT() (api.TDPState, error) {
	var s api.TDPState
	var err error
	if s.PL1SPL, err = ReadPPT("ppt_pl1_spl"); err != nil {
		return s, fmt.Errorf("reading ppt_pl1_spl: %w", err)
	}
	if s.PL2SPPT, err = ReadPPT("ppt_pl2_sppt"); err != nil {
		return s, fmt.Errorf("reading ppt_pl2_sppt: %w", err)
	}
	if s.FPPT, err = ReadPPT("ppt_fppt"); err != nil {
		return s, fmt.Errorf("reading ppt_fppt: %w", err)
	}
	if s.APUSPPT, err = ReadPPT("ppt_apu_sppt"); err != nil {
		return s, fmt.Errorf("reading ppt_apu_sppt: %w", err)
	}
	if s.PlatformSPPT, err = ReadPPT("ppt_platform_sppt"); err != nil {
		return s, fmt.Errorf("reading ppt_platform_sppt: %w", err)
	}
	return s, nil
}

// WritePPT writes a single PPT value (watts) to sysfs.
func WritePPT(attr string, watts int) error {
	return writeIntFile(FindPPTPath(attr), watts)
}

// SetTDPState writes every PPT attribute verbatim from s, with no mirroring or
// derivation. Use this when the exact five values matter — notably when
// restoring StockProfilePPT, whose measured APU/Platform sPPT do not equal PL2.
func SetTDPState(s api.TDPState) error {
	for _, w := range []struct {
		attr  string
		watts int
	}{
		{"ppt_pl1_spl", s.PL1SPL},
		{"ppt_pl2_sppt", s.PL2SPPT},
		{"ppt_fppt", s.FPPT},
		{"ppt_apu_sppt", s.APUSPPT},
		{"ppt_platform_sppt", s.PlatformSPPT},
	} {
		if err := WritePPT(w.attr, w.watts); err != nil {
			return fmt.Errorf("writing %s: %w", w.attr, err)
		}
	}
	return nil
}

// TDPStateFor resolves a unified watts value plus optional per-limit overrides
// into the five PPT values. pl1/pl2/pl3 override watts when non-zero; APU sPPT
// and Platform sPPT always follow PL2.
//
// Exposed separately from SetTDP so callers can hand the resolved state to
// ApplyTDPSafely, which needs to know PL1 before deciding whether the fan floor
// applies.
func TDPStateFor(watts, pl1, pl2, pl3 int) api.TDPState {
	if pl1 == 0 {
		pl1 = watts
	}
	if pl2 == 0 {
		pl2 = watts
	}
	if pl3 == 0 {
		pl3 = watts
	}
	return api.TDPState{
		PL1SPL:       pl1,
		PL2SPPT:      pl2,
		FPPT:         pl3,
		APUSPPT:      pl2,
		PlatformSPPT: pl2,
	}
}

// SetTDP writes all PPT values. pl1/pl2/pl3 override the unified watts value
// when non-zero. APU sPPT and Platform sPPT always follow PL2.
//
// This applies the values unconditionally. Prefer ApplyTDPSafely for anything
// that can exceed TDPMaxSafe.
func SetTDP(watts, pl1, pl2, pl3 int) error {
	return SetTDPState(TDPStateFor(watts, pl1, pl2, pl3))
}

// ApplyTDPSafely writes s, first raising both fans to HighTDPFanCurve when the
// sustained limit exceeds TDPMaxSafe. If that fan write fails, the TDP is NOT
// applied: sustaining more than TDPMaxSafe watts without the HighTDPMinPWM
// floor is the exact condition HighTDPFanCurve exists to prevent, so failing
// closed is the only safe outcome.
//
// "Fails" now includes the kernel accepting the write and then dropping the
// curve: SetBothFanCurves reads pwm_enable back, so a floor lost to a
// concurrent platform_profile write is a refusal rather than a false success.
// This function only guarantees the floor at the moment the limit is raised —
// keeping it in force afterwards is the reconcile watcher's job
// (internal/daemon/reconcile.go), since a later profile write would otherwise
// return the fans to firmware auto while the PPT stays high.
//
// This is the single entry point for every path that applies a custom TDP —
// the socket handler, the "custom" profile recall, daemon startup, resume, and
// the no-daemon CLI path. They previously enforced the floor four different
// ways, including one that raised power before raising the fans and discarded
// the fan error.
//
// Values are written verbatim via SetTDPState; use TDPStateFor first if PL2
// should be mirrored into APU/Platform sPPT.
func ApplyTDPSafely(s api.TDPState) error {
	if s.PL1SPL > TDPMaxSafe {
		if err := SetBothFanCurves(HighTDPFanCurve()); err != nil {
			return fmt.Errorf("setting high-TDP fan curve: %w (refusing to apply %dW sustained TDP without the %d PWM floor)",
				err, s.PL1SPL, HighTDPMinPWM)
		}
	}
	return SetTDPState(s)
}

// CheckFanCurveFloor rejects a curve holding any point below HighTDPMinPWM
// while the effective sustained TDP is above TDPMaxSafe.
//
// profile must be the *effective* profile (see ReadEffectivePPT). The check
// reads hardware rather than trusting cached state, which can disagree with it
// after a TDP change made while the daemon was down. A PPT read failure is not
// a rejection: the guard is best-effort and must not make fan control
// unavailable when sysfs cannot be read at all.
func CheckFanCurveFloor(profile string, points []api.FanCurvePoint) error {
	tdp, err := ReadEffectivePPT(profile)
	if err != nil {
		return nil
	}
	return CheckCurveAgainstTDP(points, tdp.PL1SPL)
}

// CheckCurveAgainstTDP rejects a curve holding any point below HighTDPMinPWM
// while pl1 is above TDPMaxSafe. It reads nothing, which is what makes it usable
// for a custom profile that is not currently applied: hardware says nothing
// about a profile that is not running, so the limit to check against is the one
// stored in the same profile.
//
// Applying that check when a profile is *edited* means no profile can be saved
// in a state that would be unsafe when it is later activated. ApplyTDPSafely
// still fails closed at activation; this is the earlier, friendlier refusal.
func CheckCurveAgainstTDP(points []api.FanCurvePoint, pl1 int) error {
	if pl1 <= TDPMaxSafe {
		return nil
	}
	for _, p := range points {
		if p.PWM < HighTDPMinPWM {
			return fmt.Errorf("PWM %d at %d°C is below minimum %d (50%%) required when sustained TDP is above %dW",
				p.PWM, p.Temp, HighTDPMinPWM, TDPMaxSafe)
		}
	}
	return nil
}

// CheckFanFloorRelease reports whether the fans may be released to firmware
// auto — i.e. whether a fan curve reset is allowed. It refuses while the
// effective sustained TDP is above TDPMaxSafe, since firmware auto is precisely
// what HighTDPFanCurve exists to override; dropping to it would remove the
// thermal floor while the power limit that requires it is still in force.
// "tdp --reset" remains the way out, as it lowers the limit first.
//
// As with CheckFanCurveFloor, a PPT read failure is not a refusal.
func CheckFanFloorRelease(profile string) error {
	tdp, err := ReadEffectivePPT(profile)
	if err != nil {
		return nil
	}
	return CheckFanFloorReleaseAt(tdp.PL1SPL)
}

// CheckFanFloorReleaseAt is CheckFanFloorRelease against a known sustained
// limit rather than one read from hardware. It is what a custom profile that is
// not currently applied has to be checked against: hardware says nothing about
// a profile that is not running, and clearing the curve from a profile that
// keeps a high limit would be unsafe the moment that profile is activated.
func CheckFanFloorReleaseAt(pl1 int) error {
	if pl1 <= TDPMaxSafe {
		return nil
	}
	return fmt.Errorf("sustained TDP is %dW (above %dW), so fans must stay at or above %d PWM; lower it first with 'z13ctl tdp --reset'",
		pl1, TDPMaxSafe, HighTDPMinPWM)
}
