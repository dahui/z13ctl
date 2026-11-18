package amd

// power_test.go — the amd-smu power limiter, with the SMU writes recorded and
// the power source scripted.

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// oxpEnvelopes is the OXP X2 Mini Pro's device data: OneXConsole's 10–85 W on
// AC and 55 W on battery, and the EC's presets as the balanced rows.
func oxpEnvelopes() (ac, battery driver.PowerEnvelope) {
	ac = driver.PowerEnvelope{TDPMin: 10, TDPMaxSafe: 85, TDPMaxForced: 85,
		StockProfilePPT: map[string]api.TDPState{"balanced": {PL1SPL: 80, PL2SPPT: 85, FPPT: 100}}}
	battery = driver.PowerEnvelope{TDPMin: 10, TDPMaxSafe: 55, TDPMaxForced: 55,
		StockProfilePPT: map[string]api.TDPState{"balanced": {PL1SPL: 50, PL2SPPT: 50, FPPT: 60}}}
	return ac, battery
}

type pptRecorder struct {
	writes [][3]int // stapm, fast, slow
	fail   error
}

func (r *pptRecorder) write(s, f, l int) error {
	r.writes = append(r.writes, [3]int{s, f, l})
	return r.fail
}

func newTestPower(t *testing.T, onAC *bool) (*SMUPower, *pptRecorder) {
	t.Helper()
	orig := pptMinGap
	pptMinGap = 0
	t.Cleanup(func() { pptMinGap = orig })
	ac, bat := oxpEnvelopes()
	p := NewSMUPower(ac, bat, "balanced", func() (bool, error) { return *onAC, nil })
	rec := &pptRecorder{}
	p.write = rec.write
	return p, rec
}

func TestSMUPowerReadModelsTheEC(t *testing.T) {
	onAC := true
	p, _ := newTestPower(t, &onAC)
	if got, _ := p.Read(); got != (api.TDPState{PL1SPL: 80, PL2SPPT: 85, FPPT: 100}) {
		t.Errorf("before any write Read = %+v, want the EC's AC preset", got)
	}
	if err := p.Apply(api.TDPState{PL1SPL: 25, PL2SPPT: 25, FPPT: 25}); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Read(); got.PL1SPL != 25 {
		t.Errorf("after a write Read = %+v, want it", got)
	}
	// Unplugged: the EC has written its battery preset over ours.
	onAC = false
	if got, _ := p.Read(); got != (api.TDPState{PL1SPL: 50, PL2SPPT: 50, FPPT: 60}) {
		t.Errorf("after a charger edge Read = %+v, want the EC's battery preset", got)
	}
}

func TestSMUPowerApplyClampsPerSourceAndSkipsRepeats(t *testing.T) {
	onAC := false
	p, rec := newTestPower(t, &onAC)
	// 70 W saved on AC, applied on battery: the battery cap.
	if err := p.Apply(api.TDPState{PL1SPL: 70, PL2SPPT: 70, FPPT: 70}); err != nil {
		t.Fatal(err)
	}
	if len(rec.writes) != 1 || rec.writes[0] != [3]int{55, 60, 55} {
		t.Errorf("writes = %v, want [55 60 55] (fast may reach the battery preset's 60)", rec.writes)
	}
	// The same request again on the same source: nothing written.
	_ = p.Apply(api.TDPState{PL1SPL: 70, PL2SPPT: 70, FPPT: 70})
	if len(rec.writes) != 1 {
		t.Errorf("a repeat was written: %v", rec.writes)
	}
	// Plugged in: the same request now applies in full.
	onAC = true
	_ = p.Apply(api.TDPState{PL1SPL: 70, PL2SPPT: 70, FPPT: 70})
	if len(rec.writes) != 2 || rec.writes[1] != [3]int{70, 70, 70} {
		t.Errorf("writes = %v, want 70/70/70 on AC", rec.writes)
	}
	// Forget (a resume): the same value is written again.
	p.Forget()
	_ = p.Apply(api.TDPState{PL1SPL: 70, PL2SPPT: 70, FPPT: 70})
	if len(rec.writes) != 3 {
		t.Errorf("after Forget the value must be written again: %v", rec.writes)
	}
}

// The reset row writes the EC's own fast limit exactly (100 W on AC), above
// the sustained range.
func TestSMUPowerWritesTheFirmwarePresetExactly(t *testing.T) {
	onAC := true
	p, rec := newTestPower(t, &onAC)
	_ = p.Apply(api.TDPState{PL1SPL: 80, PL2SPPT: 85, FPPT: 100})
	if rec.writes[0] != [3]int{80, 100, 85} {
		t.Errorf("preset written as %v, want STAPM 80, fast 100, slow 85", rec.writes[0])
	}
	if env := p.Envelope(); env.PL3.Max != 100 || env.TDPMaxForced != 85 || !env.NoSPPTMirrors || env.Interface != "ryzen_smu" {
		t.Errorf("AC envelope = %+v", env)
	}
	onAC = false
	if env := p.Envelope(); env.TDPMaxForced != 55 || env.PL3.Max != 60 {
		t.Errorf("battery envelope = %+v, want the 55 W cap and the preset's 60 W fast", env)
	}
}

func TestSMUPowerFailedWriteForgets(t *testing.T) {
	onAC := true
	p, rec := newTestPower(t, &onAC)
	_ = p.Apply(api.TDPState{PL1SPL: 30, PL2SPPT: 30, FPPT: 30})
	rec.fail = errFake
	if err := p.Apply(api.TDPState{PL1SPL: 40, PL2SPPT: 40, FPPT: 40}); err == nil {
		t.Fatal("a failed write must be reported")
	}
	if got, _ := p.Read(); got.PL1SPL != 80 {
		t.Errorf("after a failed write Read = %+v, want the firmware's row (state unknown)", got)
	}
}

var errFake = errorString("SMU rejected")

type errorString string

func (e errorString) Error() string { return string(e) }

// The real writer on Strix Halo: three MP1 messages, milliwatts, in order.
func TestSetPPTSendsTheThreeMessages(t *testing.T) {
	f := newFakeSysfs(t)
	f.withCPU(t, "AuthenticAMD", 0x1A, 112) // Strix Halo
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)
	if err := setPPT(25, 30, 28); err != nil {
		t.Fatal(err)
	}
	if got := fake.cmds[MailboxMP1]; len(got) != 3 || got[0] != 0x14 || got[1] != 0x15 || got[2] != 0x16 {
		t.Errorf("MP1 commands = %#x, want 0x14 0x15 0x16", got)
	}
	if string(fake.args[:4]) != string([]byte{0x60, 0x6d, 0, 0}) { // 28000 mW, the last command's argument
		t.Errorf("last argument = % x, want 28000 mW", fake.args[:4])
	}
}

// A CPU with no known command is refused before the mailbox is touched — the
// same zero-write rule as the Curve Optimizer.
func TestSetPPTRefusesAnUnknownCPUWithoutWriting(t *testing.T) {
	f := newFakeSysfs(t)
	f.withCPU(t, "AuthenticAMD", 0x1A, 999)
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)
	if err := setPPT(25, 25, 25); err == nil || !strings.Contains(err.Error(), "model 999") {
		t.Errorf("setPPT = %v, want a refusal naming the CPU", err)
	}
	if fake.writes != 0 {
		t.Errorf("mailbox writes = %d, want 0", fake.writes)
	}
}

func TestSetPPTStopsOnARefusal(t *testing.T) {
	f := newFakeSysfs(t)
	f.withCPU(t, "AuthenticAMD", 0x1A, 112)
	fake := &fakeSMU{response: SMUReturnRejected}
	fake.install(t)
	if err := setPPT(25, 25, 25); err == nil {
		t.Fatal("a rejected command must be an error")
	}
	if got := fake.cmds[MailboxMP1]; len(got) != 1 {
		t.Errorf("commands after a refusal = %#x, want the sequence to stop at the first", got)
	}
}

// TestSMUMissingReason: the device offers no power control without ryzen_smu,
// and the reason names the module and, where one is known, the fork the CPU
// needs. With the module loaded there is nothing to say.
func TestSMUMissingReason(t *testing.T) {
	f := newFakeSysfs(t)
	got := SMUMissingReason()
	if !strings.Contains(got, "ryzen_smu") || !strings.Contains(got, "amkillam") {
		t.Errorf("Strix Halo without the module: %q, want the module and the amkillam fork named", got)
	}

	f.withCPU(t, "AuthenticAMD", 0x19, 116) // Phoenix: no fork note
	if got := SMUMissingReason(); !strings.Contains(got, "ryzen_smu") || strings.Contains(got, "amkillam") {
		t.Errorf("Phoenix without the module: %q, want the module named and no fork", got)
	}

	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	if got := SMUMissingReason(); got != "" {
		t.Errorf("module loaded: %q, want no reason", got)
	}
}
