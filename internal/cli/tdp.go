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

// pptBasePath is the sysfs directory holding the asus-nb-wmi PPT attributes.
// Declared as a var rather than a const so tests can redirect it to a temp dir.
var pptBasePath = "/sys/devices/platform/asus-nb-wmi"

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

// SetTDP writes all PPT values. pl1/pl2/pl3 override the unified watts value
// when non-zero. APU sPPT and Platform sPPT always follow PL2.
func SetTDP(watts, pl1, pl2, pl3 int) error {
	if pl1 == 0 {
		pl1 = watts
	}
	if pl2 == 0 {
		pl2 = watts
	}
	if pl3 == 0 {
		pl3 = watts
	}
	return SetTDPState(api.TDPState{
		PL1SPL:       pl1,
		PL2SPPT:      pl2,
		FPPT:         pl3,
		APUSPPT:      pl2,
		PlatformSPPT: pl2,
	})
}
