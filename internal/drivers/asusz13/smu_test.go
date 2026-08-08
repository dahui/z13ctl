package cli

// smu_test.go — ryzen_smu mailbox protocol and Curve Optimizer command paths,
// driven by the fakeSMU mailbox from sysfs_fake_test.go.

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func TestSMUAvailable(t *testing.T) {
	f := newFakeSysfs(t)

	if SMUAvailable() {
		t.Error("SMUAvailable() = true before rsmu_cmd exists, want false")
	}
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	if !SMUAvailable() {
		t.Error("SMUAvailable() = false with rsmu_cmd present, want true")
	}
}

func TestSendSMUCommandEncodesArgsLittleEndian(t *testing.T) {
	newFakeSysfs(t)
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	args := [6]uint32{0x0FFFEC, 2, 3, 4, 5, 6}
	code, out, err := SendSMUCommand(MailboxMP1, smuCmdMP1COALL, args)
	if err != nil {
		t.Fatalf("SendSMUCommand() = %v, want nil", err)
	}
	if code != SMUReturnOK {
		t.Errorf("response code = 0x%X, want 0x%X", code, SMUReturnOK)
	}
	if out != args {
		t.Errorf("out args = %v, want the args echoed back %v", out, args)
	}

	// The driver expects 6 little-endian u32s in a single 24-byte write.
	if len(fake.args) != 24 {
		t.Fatalf("smu_args write was %d bytes, want 24", len(fake.args))
	}
	for i, want := range args {
		if got := binary.LittleEndian.Uint32(fake.args[i*4:]); got != want {
			t.Errorf("arg %d encoded as 0x%X, want 0x%X", i, got, want)
		}
	}
}

func TestSendSMUCommandPropagatesReadFailure(t *testing.T) {
	newFakeSysfs(t)
	fake := &fakeSMU{response: SMUReturnOK, failRead: true}
	fake.install(t)

	if _, _, err := SendSMUCommand(MailboxMP1, smuCmdMP1COALL, [6]uint32{}); err == nil {
		t.Error("SendSMUCommand() = nil, want an error when the mailbox read fails")
	}
}

func TestSendSMUCommandPropagatesWriteFailure(t *testing.T) {
	newFakeSysfs(t)
	origW := smuWriteFile
	smuWriteFile = func(string, []byte, os.FileMode) error { return os.ErrPermission }
	t.Cleanup(func() { smuWriteFile = origW })
	resetSMUProbe(t)

	if _, _, err := SendSMUCommand(MailboxMP1, smuCmdMP1COALL, [6]uint32{}); err == nil {
		t.Error("SendSMUCommand() = nil, want an error when smu_args is not writable")
	}
}

// TestSMUProbeUndervoltGatesOnResponse covers the check that distinguishes the
// amkillam ryzen_smu fork (which supports CO on Strix Halo) from the leogx9r
// fork, which answers 0xFE.
func TestSMUProbeUndervoltGatesOnResponse(t *testing.T) {
	tests := []struct {
		name     string
		response uint32
		want     bool
	}{
		{"supported fork returns OK", SMUReturnOK, true},
		{"unsupported fork returns unknown-command", SMUReturnUnknownCmd, false},
		{"firmware rejects", SMUReturnRejected, false},
		{"firmware busy", SMUReturnBusy, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeSysfs(t)
			f.writeFile(t, f.smu+"/rsmu_cmd", "")
			fake := &fakeSMU{response: tt.response}
			fake.install(t)

			if got := SMUProbeUndervolt(); got != tt.want {
				t.Errorf("SMUProbeUndervolt() = %v, want %v for response 0x%X", got, tt.want, tt.response)
			}
		})
	}
}

func TestSMUProbeUndervoltFalseWithoutModule(t *testing.T) {
	newFakeSysfs(t) // no rsmu_cmd file
	resetSMUProbe(t)
	if SMUProbeUndervolt() {
		t.Error("SMUProbeUndervolt() = true without ryzen_smu loaded, want false")
	}
}

// TestSMUProbeUndervoltCachesResult pins the sync.Once behaviour: the probe
// writes to the mailbox once and reuses the answer.
func TestSMUProbeUndervoltCachesResult(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	for range 5 {
		if !SMUProbeUndervolt() {
			t.Fatal("SMUProbeUndervolt() = false, want true")
		}
	}
	// One probe = one args write + one command write.
	if fake.writes != 2 {
		t.Errorf("mailbox writes = %d, want 2 (probe ran more than once)", fake.writes)
	}
}

func TestSMUResponseErrorMessages(t *testing.T) {
	if err := smuResponseError(SMUReturnOK); err != nil {
		t.Errorf("smuResponseError(OK) = %v, want nil", err)
	}
	for _, code := range []uint32{SMUReturnFailed, SMUReturnUnknownCmd, SMUReturnRejected, SMUReturnBusy, 0x42} {
		if err := smuResponseError(code); err == nil {
			t.Errorf("smuResponseError(0x%X) = nil, want an error", code)
		}
	}
	// The unknown-command message must name the required fork; that string is
	// the only guidance a user gets when they install the wrong one.
	if got := smuResponseError(SMUReturnUnknownCmd).Error(); !strings.Contains(got, "amkillam") {
		t.Errorf("unknown-command error = %q, want it to name the amkillam fork", got)
	}
}

func TestSetCurveOptimizerSendsEncodedOffset(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	if err := SetCurveOptimizer(-20); err != nil {
		t.Fatalf("SetCurveOptimizer(-20) = %v, want nil", err)
	}
	got := binary.LittleEndian.Uint32(fake.args[:4])
	if want := encodeCOValue(-20); got != want {
		t.Errorf("encoded offset = 0x%X, want 0x%X", got, want)
	}
}

func TestSetCurveOptimizerRejectsOutOfRange(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	for _, v := range []int{UVMinCPU - 1, UVMaxCPU + 1, -100, 5} {
		if err := SetCurveOptimizer(v); err == nil {
			t.Errorf("SetCurveOptimizer(%d) = nil, want a range error", v)
		}
	}
}

func TestSetCurveOptimizerUnavailableWithoutModule(t *testing.T) {
	newFakeSysfs(t)
	resetSMUProbe(t)
	if err := SetCurveOptimizer(-10); err == nil {
		t.Error("SetCurveOptimizer() = nil, want an error when ryzen_smu is absent")
	}
	if err := ResetCurveOptimizer(); err == nil {
		t.Error("ResetCurveOptimizer() = nil, want an error when ryzen_smu is absent")
	}
}

func TestSetCurveOptimizerSurfacesFirmwareRejection(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	// Probe succeeds, then the real command is rejected.
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)
	if !SMUProbeUndervolt() {
		t.Fatal("probe should succeed before the rejection case")
	}
	fake.mu.Lock()
	fake.response = SMUReturnRejected
	fake.mu.Unlock()

	if err := SetCurveOptimizer(-15); err == nil {
		t.Error("SetCurveOptimizer() = nil, want an error when the firmware rejects the command")
	}
}

func TestResetCurveOptimizerSendsZeroOffset(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	if err := ResetCurveOptimizer(); err != nil {
		t.Fatalf("ResetCurveOptimizer() = %v, want nil", err)
	}
	got := binary.LittleEndian.Uint32(fake.args[:4])
	if want := encodeCOValue(0); got != want {
		t.Errorf("reset sent 0x%X, want the stock encoding 0x%X", got, want)
	}
}

// TestSMUProbeIsDestructive documents, and pins, the fact that the availability
// probe is not read-only: it sends the same CO-zero command as
// ResetCurveOptimizer, so probing clears any undervolt currently applied.
//
// This is safe where the caller sets a value straight afterwards or caches the
// result for the process lifetime (the daemon probes once at startup). It is NOT
// safe to call speculatively from a short-lived CLI process — the sync.Once does
// not survive it, so every invocation would wipe the user's undervolt. If this
// test ever starts failing because the probe became read-only, the warnings on
// SMUProbeUndervolt and in cmd/status.go can be relaxed.
func TestSMUProbeIsDestructive(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")

	probe := &fakeSMU{response: SMUReturnOK}
	probe.install(t)
	if !SMUProbeUndervolt() {
		t.Fatal("SMUProbeUndervolt() = false, want true")
	}
	probeArgs := append([]byte(nil), probe.args...)

	reset := &fakeSMU{response: SMUReturnOK}
	reset.install(t)
	if err := ResetCurveOptimizer(); err != nil {
		t.Fatalf("ResetCurveOptimizer() = %v, want nil", err)
	}

	if !bytes.Equal(probeArgs, reset.args) {
		t.Fatalf("probe args %v differ from reset args %v", probeArgs, reset.args)
	}
	t.Logf("probe writes the same payload as a CO reset (%v) — it is not read-only", probeArgs)
}
