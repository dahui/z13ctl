package cli

// smu.go — ryzen_smu kernel module sysfs interface for sending AMD SMU commands.
//
// The ryzen_smu module exposes a binary sysfs interface at /sys/kernel/ryzen_smu_drv/
// for communicating with the Ryzen System Management Unit. All reads and writes
// are binary (little-endian u32 values), not text.

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
)

const smuDriverPath = "/sys/kernel/ryzen_smu_drv"

// SMU mailbox identifiers. Each corresponds to a sysfs file that accepts
// binary command IDs and returns binary response codes.
const (
	MailboxMP1  = "mp1_smu_cmd" // MP1 mailbox
	MailboxRSMU = "rsmu_cmd"    // RSMU/PSMU mailbox (used for Curve Optimizer)
)

// SMU response codes returned by the firmware after a command.
const (
	SMUReturnOK         uint32 = 0x01
	SMUReturnFailed     uint32 = 0xFF
	SMUReturnUnknownCmd uint32 = 0xFE
	SMUReturnRejected   uint32 = 0xFD
	SMUReturnBusy       uint32 = 0xFC
)

// smuMu serializes all SMU command sequences. The ryzen_smu driver shares a
// single argument buffer across all mailboxes, so concurrent commands would
// corrupt each other's arguments.
var smuMu sync.Mutex

// SMUAvailable reports whether the ryzen_smu kernel module is loaded and its
// sysfs interface is accessible.
func SMUAvailable() bool {
	_, err := os.Stat(smuDriverPath + "/rsmu_cmd")
	return err == nil
}

// SendSMUCommand sends a command to the specified SMU mailbox and returns
// the response code and output arguments.
//
// Protocol:
//  1. Write 24 bytes (6 × u32 LE) to smu_args
//  2. Write 4 bytes (u32 LE command ID) to the mailbox file
//  3. Read 4 bytes (u32 LE response code) from the mailbox file
//  4. Read 24 bytes (6 × u32 LE) from smu_args for response arguments
func SendSMUCommand(mailbox string, cmdID uint32, args [6]uint32) (code uint32, outArgs [6]uint32, retErr error) {
	smuMu.Lock()
	defer smuMu.Unlock()

	argsPath := smuDriverPath + "/smu_args"
	cmdPath := smuDriverPath + "/" + mailbox

	// Write arguments (24 bytes, 6 × u32 LE).
	argsBuf := make([]byte, 24)
	for i, v := range args {
		binary.LittleEndian.PutUint32(argsBuf[i*4:], v)
	}
	if err := os.WriteFile(argsPath, argsBuf, 0o640); err != nil {
		return 0, [6]uint32{}, fmt.Errorf("writing smu_args: %w", err)
	}

	// Write command ID (4 bytes, u32 LE).
	cmdBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(cmdBuf, cmdID)
	if err := os.WriteFile(cmdPath, cmdBuf, 0o640); err != nil {
		return 0, [6]uint32{}, fmt.Errorf("writing %s: %w", mailbox, err)
	}

	// Read response code (4 bytes, u32 LE).
	respData, err := os.ReadFile(cmdPath)
	if err != nil {
		return 0, [6]uint32{}, fmt.Errorf("reading %s response: %w", mailbox, err)
	}
	if len(respData) < 4 {
		return 0, [6]uint32{}, fmt.Errorf("short response from %s: %d bytes", mailbox, len(respData))
	}
	code = binary.LittleEndian.Uint32(respData[:4])

	// Read response arguments (24 bytes).
	respArgData, err := os.ReadFile(argsPath)
	if err != nil {
		return code, [6]uint32{}, fmt.Errorf("reading smu_args response: %w", err)
	}
	for i := range outArgs {
		if (i+1)*4 <= len(respArgData) {
			outArgs[i] = binary.LittleEndian.Uint32(respArgData[i*4 : (i+1)*4])
		}
	}

	return code, outArgs, nil
}

// smuResponseError returns a human-readable error for a non-OK SMU response.
func smuResponseError(code uint32) error {
	switch code {
	case SMUReturnOK:
		return nil
	case SMUReturnFailed:
		return fmt.Errorf("SMU command failed (0xFF)")
	case SMUReturnUnknownCmd:
		return fmt.Errorf("SMU unknown command (0xFE)")
	case SMUReturnRejected:
		return fmt.Errorf("SMU command rejected (0xFD)")
	case SMUReturnBusy:
		return fmt.Errorf("SMU busy (0xFC)")
	default:
		return fmt.Errorf("SMU unexpected response: 0x%X", code)
	}
}
