package cli

// forward.go — transitional forwarders to internal/drivers/asusz13. The
// hardware layer moved there in the driver extraction; these keep cmd/ and
// internal/daemon compiling unchanged while they are converted to the device
// registry, and are deleted with the last caller. No logic belongs here: a
// forwarder that grows a body defeats the single-implementation point of the
// move.

import (
	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/drivers/asusz13"
)

// TDP safety limits, re-exported from the Z13 driver.
const (
	TDPMin       = asusz13.TDPMin
	TDPMaxSafe   = asusz13.TDPMaxSafe
	TDPMaxForced = asusz13.TDPMaxForced
	TDPDefault   = asusz13.TDPDefault
	// HighTDPMinPWM is the high-TDP floor curve's bottom.
	HighTDPMinPWM = asusz13.HighTDPMinPWM
)

// Curve Optimizer offset bounds, re-exported from the Z13 driver.
const (
	UVMinCPU = asusz13.UVMinCPU
	UVMaxCPU = asusz13.UVMaxCPU
)

// StockProfilePPT is the measured per-profile PPT table (asusz13's, aliased).
var StockProfilePPT = asusz13.StockProfilePPT

// --- power / TDP ---

// ReadEffectivePPT forwards to asusz13.ReadEffectivePPT.
func ReadEffectivePPT(profile string) (api.TDPState, error) {
	return asusz13.ReadEffectivePPT(profile)
}

// ReadAllPPT forwards to asusz13.ReadAllPPT.
func ReadAllPPT() (api.TDPState, error) { return asusz13.ReadAllPPT() }

// SetTDPState forwards to asusz13.SetTDPState.
func SetTDPState(s api.TDPState) error { return asusz13.SetTDPState(s) }

// TDPStateFor forwards to asusz13.TDPStateFor.
func TDPStateFor(watts, pl1, pl2, pl3 int) api.TDPState {
	return asusz13.TDPStateFor(watts, pl1, pl2, pl3)
}

// SetTDP forwards to asusz13.SetTDP.
func SetTDP(watts, pl1, pl2, pl3 int) error { return asusz13.SetTDP(watts, pl1, pl2, pl3) }

// ApplyTDPSafely forwards to asusz13.ApplyTDPSafely (the safety.Engine path).
func ApplyTDPSafely(s api.TDPState, want []api.FanCurvePoint) error {
	return asusz13.ApplyTDPSafely(s, want)
}

// FanCurveForTDP forwards to asusz13.FanCurveForTDP.
func FanCurveForTDP(pl1 int, want []api.FanCurvePoint) []api.FanCurvePoint {
	return asusz13.FanCurveForTDP(pl1, want)
}

// FloorPWMAt forwards to asusz13.FloorPWMAt.
func FloorPWMAt(temp int) int { return asusz13.FloorPWMAt(temp) }

// FloorAdjustsCurve forwards to asusz13.FloorAdjustsCurve.
func FloorAdjustsCurve(pl1 int, want []api.FanCurvePoint) bool {
	return asusz13.FloorAdjustsCurve(pl1, want)
}

// CheckFanCurveFloor forwards to asusz13.CheckFanCurveFloor.
func CheckFanCurveFloor(profile string, points []api.FanCurvePoint) error {
	return asusz13.CheckFanCurveFloor(profile, points)
}

// CheckCurveAgainstTDP forwards to asusz13.CheckCurveAgainstTDP.
func CheckCurveAgainstTDP(points []api.FanCurvePoint, pl1 int) error {
	return asusz13.CheckCurveAgainstTDP(points, pl1)
}

// CheckFanFloorRelease forwards to asusz13.CheckFanFloorRelease.
func CheckFanFloorRelease(profile string) error { return asusz13.CheckFanFloorRelease(profile) }

// CheckFanFloorReleaseAt forwards to asusz13.CheckFanFloorReleaseAt.
func CheckFanFloorReleaseAt(pl1 int) error { return asusz13.CheckFanFloorReleaseAt(pl1) }

// --- fans ---

// HighTDPFanCurve forwards to asusz13.HighTDPFanCurve.
func HighTDPFanCurve() []api.FanCurvePoint { return asusz13.HighTDPFanCurve() }

// ParseFanCurve forwards to asusz13.ParseFanCurve.
func ParseFanCurve(s string) ([]api.FanCurvePoint, error) { return asusz13.ParseFanCurve(s) }

// FanModeName forwards to asusz13.FanModeName.
func FanModeName(mode int) string { return asusz13.FanModeName(mode) }

// ReadBothFanRPM forwards to asusz13.ReadBothFanRPM.
func ReadBothFanRPM() ([2]int, error) { return asusz13.ReadBothFanRPM() }

// ReadBothFanModes forwards to asusz13.ReadBothFanModes.
func ReadBothFanModes() ([2]int, error) { return asusz13.ReadBothFanModes() }

// ReadFanCurveModes forwards to asusz13.ReadFanCurveModes.
func ReadFanCurveModes() ([2]int, error) { return asusz13.ReadFanCurveModes() }

// ReadBothFanCurves forwards to asusz13.ReadBothFanCurves.
func ReadBothFanCurves() ([2][]api.FanCurvePoint, error) { return asusz13.ReadBothFanCurves() }

// LiveFanCurve forwards to asusz13.LiveFanCurve.
func LiveFanCurve() []api.FanCurvePoint { return asusz13.LiveFanCurve() }

// SetBothFanCurves forwards to asusz13.SetBothFanCurves.
func SetBothFanCurves(points []api.FanCurvePoint) error { return asusz13.SetBothFanCurves(points) }

// ResetAllFanCurves forwards to asusz13.ResetAllFanCurves.
func ResetAllFanCurves() error { return asusz13.ResetAllFanCurves() }

// VerifyFanCurveActive forwards to asusz13.VerifyFanCurveActive.
func VerifyFanCurveActive() error { return asusz13.VerifyFanCurveActive() }

// SetAllFansFullSpeed forwards to asusz13.SetAllFansFullSpeed.
func SetAllFansFullSpeed() error { return asusz13.SetAllFansFullSpeed() }

// FindFanCurveHwmonPath forwards to asusz13.FindFanCurveHwmonPath.
func FindFanCurveHwmonPath() string { return asusz13.FindFanCurveHwmonPath() }

// FindPPTBasePath forwards to asusz13.FindPPTBasePath.
func FindPPTBasePath() string { return asusz13.FindPPTBasePath() }

// --- platform profile / power source ---

// FindProfilePath forwards to asusz13.FindProfilePath.
func FindProfilePath() string { return asusz13.FindProfilePath() }

// SetProfile forwards to asusz13.SetProfile.
func SetProfile(profile string) error { return asusz13.SetProfile(profile) }

// IsStockProfile forwards to asusz13.IsStockProfile.
func IsStockProfile(name string) bool { return asusz13.IsStockProfile(name) }

// ValidateProfileName forwards to asusz13.ValidateProfileName.
func ValidateProfileName(name string) error { return asusz13.ValidateProfileName(name) }

// FindACOnlinePath forwards to asusz13.FindACOnlinePath.
func FindACOnlinePath() string { return asusz13.FindACOnlinePath() }

// OnACPower forwards to asusz13.OnACPower.
func OnACPower() (bool, error) { return asusz13.OnACPower() }

// --- battery / firmware attributes / temperature ---

// FindBatteryThresholdPath forwards to asusz13.FindBatteryThresholdPath.
func FindBatteryThresholdPath() string { return asusz13.FindBatteryThresholdPath() }

// FindBatteryCapacityPath forwards to asusz13.FindBatteryCapacityPath.
func FindBatteryCapacityPath() string { return asusz13.FindBatteryCapacityPath() }

// FindBootSoundPath forwards to asusz13.FindBootSoundPath.
func FindBootSoundPath() string { return asusz13.FindBootSoundPath() }

// SetBootSound forwards to asusz13.SetBootSound.
func SetBootSound(value int) error { return asusz13.SetBootSound(value) }

// FindPanelOverdrivePath forwards to asusz13.FindPanelOverdrivePath.
func FindPanelOverdrivePath() string { return asusz13.FindPanelOverdrivePath() }

// SetPanelOverdrive forwards to asusz13.SetPanelOverdrive.
func SetPanelOverdrive(value int) error { return asusz13.SetPanelOverdrive(value) }

// FindAPUTemperaturePath forwards to asusz13.FindAPUTemperaturePath.
func FindAPUTemperaturePath() string { return asusz13.FindAPUTemperaturePath() }

// ReadAPUTemperature forwards to asusz13.ReadAPUTemperature.
func ReadAPUTemperature() (int, error) { return asusz13.ReadAPUTemperature() }

// --- undervolt / SMU ---

// SMUAvailable forwards to asusz13.SMUAvailable.
func SMUAvailable() bool { return asusz13.SMUAvailable() }

// SMUProbeUndervolt forwards to asusz13.SMUProbeUndervolt. The probe is
// destructive; every warning on the asusz13 function applies unchanged.
func SMUProbeUndervolt() bool { return asusz13.SMUProbeUndervolt() }

// ValidateCOValues forwards to asusz13.ValidateCOValues.
func ValidateCOValues(cpu int) error { return asusz13.ValidateCOValues(cpu) }

// SetCurveOptimizer forwards to asusz13.SetCurveOptimizer.
func SetCurveOptimizer(cpuOffset int) error { return asusz13.SetCurveOptimizer(cpuOffset) }

// ResetCurveOptimizer forwards to asusz13.ResetCurveOptimizer.
func ResetCurveOptimizer() error { return asusz13.ResetCurveOptimizer() }
