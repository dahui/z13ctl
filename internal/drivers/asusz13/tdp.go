package asusz13

// tdp.go — PPT sysfs path discovery and I/O helpers for ASUS TDP control.
// Uses the asus-nb-wmi platform device attributes (NOT firmware-attributes,
// which have empty calibration data on the 2025 Z13).
//
// This is deliberately just the sysfs layer. The safety limits, the stock
// per-profile PPT table, and the high-TDP floor curve live in the device data
// (internal/device/devices/asus-rog-flow-z13-2025.toml), and every rule about
// them — fail-closed apply ordering, the stale-cache substitution, the floor —
// lives in internal/safety, reached through the engine the device registry
// assembles around NewPowerLimiter.

import (
	"fmt"
	"os"

	"github.com/dahui/z13ctl/api"
)

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
// derivation. This is the raw limit write the safety engine wraps: restoring a
// stock row needs the exact five values (measured APU/Platform sPPT do not
// equal PL2), and everything above the safe maximum must go through
// safety.Engine.ApplyTDPSafely rather than calling this directly.
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
