package asusz13

// smu.go — ryzen_smu kernel module sysfs interface for sending AMD SMU commands.
//
// The ryzen_smu module exposes a binary sysfs interface at /sys/kernel/ryzen_smu_drv/
// for communicating with the Ryzen System Management Unit. All reads and writes
// are binary (little-endian u32 values), not text.

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
)

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

// smuReadFile and smuWriteFile indirect the mailbox I/O so tests can supply a
// fake ryzen_smu driver. The real driver replaces a mailbox file's contents with
// the firmware response after a command write, which plain files cannot emulate.
var (
	smuReadFile  = os.ReadFile
	smuWriteFile = os.WriteFile
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
	if err := smuWriteFile(argsPath, argsBuf, 0o640); err != nil {
		return 0, [6]uint32{}, fmt.Errorf("writing smu_args: %w", err)
	}

	// Write command ID (4 bytes, u32 LE).
	cmdBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(cmdBuf, cmdID)
	if err := smuWriteFile(cmdPath, cmdBuf, 0o640); err != nil {
		return 0, [6]uint32{}, fmt.Errorf("writing %s: %w", mailbox, err)
	}

	// Read response code (4 bytes, u32 LE).
	respData, err := smuReadFile(cmdPath)
	if err != nil {
		return 0, [6]uint32{}, fmt.Errorf("reading %s response: %w", mailbox, err)
	}
	if len(respData) < 4 {
		return 0, [6]uint32{}, fmt.Errorf("short response from %s: %d bytes", mailbox, len(respData))
	}
	code = binary.LittleEndian.Uint32(respData[:4])

	// Read response arguments (24 bytes).
	respArgData, err := smuReadFile(argsPath)
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

// smuProbeOnce ensures the undervolt probe runs only once. smuProbeResult
// publishes its outcome to SMUUndervoltAvailable, which must never run it:
// 0 not yet probed, 1 supported, 2 not.
var (
	smuProbeOnce   = new(sync.Once)
	smuProbeOK     bool
	smuProbeResult atomic.Int32
)

// SMUProbeUndervolt reports whether the installed ryzen_smu module supports
// Curve Optimizer on this platform, by sending this CPU's CO command with an
// offset of 0. Returns false if the module is missing, the CPU has no known CO
// command (refused before any write), or the command errors (e.g. the leogx9r
// fork, which does not support Strix Halo). Cached after the first call.
//
// The probe is NOT read-only. A CO offset of 0 is exactly what
// ResetCurveOptimizer writes, so probing clears any undervolt currently applied.
// That is harmless where the caller sets a CO value immediately afterwards
// (SetCurveOptimizer, ResetCurveOptimizer) or holds the result for the process
// lifetime (the daemon probes once at startup, before restoring saved offsets).
//
// It is NOT safe to call speculatively from a short-lived process just to ask
// "is undervolting available?" — every invocation would silently wipe the user's
// undervolt, since the sync.Once cache does not survive the process. Ask the
// daemon (get-state's undervolt_available) or use SMUAvailable, which only stats
// the sysfs interface.
func SMUProbeUndervolt() bool {
	if !SMUAvailable() {
		return false
	}
	smuProbeOnce.Do(func() {
		// An unknown CPU is refused here, before the mailbox: the probe is a
		// write, and a guessed command is the speculative write by another name.
		if _, err := coPlatformFor(); err != nil {
			smuProbeResult.Store(2)
			slog.Warn("Curve Optimizer disabled: no SMU command for this CPU", "err", err)
			return
		}
		err := sendCO(0)
		smuProbeOK = err == nil
		if smuProbeOK {
			smuProbeResult.Store(1)
		} else {
			smuProbeResult.Store(2)
			slog.Warn("SMU undervolt probe failed — Curve Optimizer will be disabled", "err", err)
		}
	})
	return smuProbeOK
}

// SMUUndervoltAvailable answers "is undervolting available?" without ever
// writing the SMU. Once SMUProbeUndervolt has run in this process its result is
// returned; until then it falls back to SMUAvailable, a stat, which claims less —
// the module is loaded, not that this fork supports Curve Optimizer here.
//
// Every caller that is only *asking* must use this rather than the probe: the
// probe's first run is a CO reset, and asking is not a reason to write the MP1
// mailbox (see the 2026-08-14 lockup in Daemon.uvApplied). The probe is for the
// caller about to write an offset anyway. The trade is that a machine with the
// wrong ryzen_smu fork reports "available" until the first real write probes
// and fails, at which point it reports false and the write is refused. A CPU
// with no known CO command reports false from the start: that needs no write
// to find out.
func SMUUndervoltAvailable() bool {
	switch smuProbeResult.Load() {
	case 1:
		return true
	case 2:
		return false
	}
	if !SMUAvailable() {
		return false
	}
	_, err := coPlatformFor()
	return err == nil
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
