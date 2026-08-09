package asusz13

// undervolt.go — AMD Curve Optimizer (CO) control via the ryzen_smu kernel module.
//
// Curve Optimizer adjusts the voltage-frequency curve for all CPU cores. Negative
// values reduce voltage (undervolt), improving efficiency and thermals. Values are
// volatile — they reset on reboot, sleep, or profile change.
//
// The 2025 ROG Flow Z13 uses AMD Ryzen AI MAX+ 395 (Strix Halo, FAMID=14).
// CPU CO uses MP1 mailbox command 0x4C, matching ryzenadj's set_coall implementation.
// iGPU CO (PSMU 0xB7) is not supported on Strix Halo — ryzenadj explicitly excludes
// FAM_STRIXHALO from set_cogfx.

import "fmt"

// Strix Halo (FAMID=14) SMU command ID for Curve Optimizer.
const (
	smuCmdMP1COALL uint32 = 0x4C // MP1 mailbox: set all-core CO
)

// encodeCOValue encodes a Curve Optimizer offset for the SMU.
// Input is a non-positive integer (e.g. -20 for 20mV undervolt).
// Encoding: 0x100000 - abs(value).
func encodeCOValue(offset int) uint32 {
	if offset >= 0 {
		return 0x100000
	}
	return uint32(0x100000) - uint32(-offset)
}

// SetCurveOptimizer applies a Curve Optimizer offset to all CPU cores.
// The value must be <= 0. A value of 0 means "stock" (no change).
//
// The offset bounds live in device data and are enforced by the undervolter
// driver's Apply before this runs; encodeCOValue independently maps any
// positive value to stock, so an overvolt cannot be encoded here at all.
//
// Uses MP1 mailbox command 0x4C (matching ryzenadj set_coall for Strix Halo).
func SetCurveOptimizer(cpuOffset int) error {
	if !SMUProbeUndervolt() {
		return fmt.Errorf("curve optimizer not available — ryzen_smu module missing or does not support this platform")
	}

	encoded := encodeCOValue(cpuOffset)
	args := [6]uint32{encoded}

	resp, _, err := SendSMUCommand(MailboxMP1, smuCmdMP1COALL, args)
	if err != nil {
		return fmt.Errorf("CPU CO MP1 command: %w", err)
	}
	if rErr := smuResponseError(resp); rErr != nil {
		return fmt.Errorf("CPU CO MP1: %w", rErr)
	}

	return nil
}

// ResetCurveOptimizer resets the CPU Curve Optimizer to stock (0).
func ResetCurveOptimizer() error {
	if !SMUProbeUndervolt() {
		return fmt.Errorf("curve optimizer not available — ryzen_smu module missing or does not support this platform")
	}

	encoded := encodeCOValue(0)
	args := [6]uint32{encoded}

	if resp, _, err := SendSMUCommand(MailboxMP1, smuCmdMP1COALL, args); err != nil {
		return fmt.Errorf("reset CPU CO MP1: %w", err)
	} else if rErr := smuResponseError(resp); rErr != nil {
		return fmt.Errorf("reset CPU CO MP1: %w", rErr)
	}

	return nil
}
