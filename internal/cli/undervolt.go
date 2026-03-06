package cli

// undervolt.go — AMD Curve Optimizer (CO) control via the ryzen_smu kernel module.
//
// Curve Optimizer adjusts the voltage-frequency curve for all CPU cores or the
// integrated GPU. Negative values reduce voltage (undervolt), improving efficiency
// and thermals. Values are volatile — they reset on reboot, sleep, or profile change.
//
// The 2025 ROG Flow Z13 uses AMD Ryzen AI MAX+ 395 (Strix Halo, FAMID=14).
// SMU command IDs and value encoding are derived from G-Helper's implementation.

import "fmt"

// Curve Optimizer safety limits matching G-Helper defaults.
const (
	UVMinCPU  = -40 // maximum CPU undervolt (most aggressive)
	UVMaxCPU  = 0   // no undervolt (stock)
	UVMinIGPU = -30 // maximum iGPU undervolt
	UVMaxIGPU = 0   // no undervolt (stock)
)

// Strix Halo (FAMID=14) SMU command IDs for Curve Optimizer.
const (
	// All-core CO requires two commands in sequence.
	smuCmdMP1COALL  uint32 = 0x4C // MP1 mailbox: set all-core CO
	smuCmdPSMUCOALL uint32 = 0x5D // PSMU mailbox: set all-core CO
	// iGPU CO uses a single PSMU command.
	smuCmdPSMUCOGFX uint32 = 0xB7 // PSMU mailbox: set iGPU CO
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

// ValidateCOValues checks that CPU and iGPU CO offsets are within safe ranges.
func ValidateCOValues(cpu, igpu int) error {
	if cpu < UVMinCPU || cpu > UVMaxCPU {
		return fmt.Errorf("CPU undervolt %d out of range %d to %d", cpu, UVMinCPU, UVMaxCPU)
	}
	if igpu < UVMinIGPU || igpu > UVMaxIGPU {
		return fmt.Errorf("iGPU undervolt %d out of range %d to %d", igpu, UVMinIGPU, UVMaxIGPU)
	}
	return nil
}

// SetCurveOptimizer applies Curve Optimizer offsets to the CPU and/or iGPU.
// A value of 0 means "no change" for that component. Both values must be <= 0.
//
// For Strix Halo (FAMID=14), the CPU CO sequence is:
//  1. MP1 cmd 0x4C with encoded value
//  2. PSMU cmd 0x5D with same encoded value
//
// The iGPU CO is a single PSMU cmd 0xB7.
func SetCurveOptimizer(cpuOffset, igpuOffset int) error {
	if !SMUAvailable() {
		return fmt.Errorf("ryzen_smu kernel module not detected; install ryzen_smu-dkms-git (AUR) or equivalent")
	}
	if err := ValidateCOValues(cpuOffset, igpuOffset); err != nil {
		return err
	}

	if cpuOffset != 0 {
		encoded := encodeCOValue(cpuOffset)
		args := [6]uint32{encoded}

		// Step 1: MP1 mailbox command.
		resp, _, err := SendSMUCommand(MailboxMP1, smuCmdMP1COALL, args)
		if err != nil {
			return fmt.Errorf("CPU CO MP1 command: %w", err)
		}
		if rErr := smuResponseError(resp); rErr != nil {
			return fmt.Errorf("CPU CO MP1: %w", rErr)
		}

		// Step 2: PSMU mailbox command.
		resp, _, err = SendSMUCommand(MailboxRSMU, smuCmdPSMUCOALL, args)
		if err != nil {
			return fmt.Errorf("CPU CO PSMU command: %w", err)
		}
		if rErr := smuResponseError(resp); rErr != nil {
			return fmt.Errorf("CPU CO PSMU: %w", rErr)
		}
	}

	if igpuOffset != 0 {
		encoded := encodeCOValue(igpuOffset)
		args := [6]uint32{encoded}

		resp, _, err := SendSMUCommand(MailboxRSMU, smuCmdPSMUCOGFX, args)
		if err != nil {
			return fmt.Errorf("iGPU CO command: %w", err)
		}
		if rErr := smuResponseError(resp); rErr != nil {
			return fmt.Errorf("iGPU CO: %w", rErr)
		}
	}

	return nil
}

// ResetCurveOptimizer resets both CPU and iGPU Curve Optimizer to stock (0).
func ResetCurveOptimizer() error {
	if !SMUAvailable() {
		return fmt.Errorf("ryzen_smu kernel module not detected; install ryzen_smu-dkms-git (AUR) or equivalent")
	}

	encoded := encodeCOValue(0)
	args := [6]uint32{encoded}

	// Reset CPU CO.
	if resp, _, err := SendSMUCommand(MailboxMP1, smuCmdMP1COALL, args); err != nil {
		return fmt.Errorf("reset CPU CO MP1: %w", err)
	} else if rErr := smuResponseError(resp); rErr != nil {
		return fmt.Errorf("reset CPU CO MP1: %w", rErr)
	}
	if resp, _, err := SendSMUCommand(MailboxRSMU, smuCmdPSMUCOALL, args); err != nil {
		return fmt.Errorf("reset CPU CO PSMU: %w", err)
	} else if rErr := smuResponseError(resp); rErr != nil {
		return fmt.Errorf("reset CPU CO PSMU: %w", rErr)
	}

	// Reset iGPU CO.
	if resp, _, err := SendSMUCommand(MailboxRSMU, smuCmdPSMUCOGFX, args); err != nil {
		return fmt.Errorf("reset iGPU CO: %w", err)
	} else if rErr := smuResponseError(resp); rErr != nil {
		return fmt.Errorf("reset iGPU CO: %w", rErr)
	}

	return nil
}
