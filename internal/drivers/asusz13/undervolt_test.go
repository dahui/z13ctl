package asusz13

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestEncodeCOValue_Zero(t *testing.T) {
	got := encodeCOValue(0)
	if got != 0x100000 {
		t.Errorf("encodeCOValue(0) = 0x%X, want 0x100000", got)
	}
}

func TestEncodeCOValue_Negative(t *testing.T) {
	tests := []struct {
		offset int
		want   uint32
	}{
		{-1, 0x0FFFFF},
		{-20, 0x100000 - 20},
		{-40, 0x100000 - 40},
	}
	for _, tt := range tests {
		got := encodeCOValue(tt.offset)
		if got != tt.want {
			t.Errorf("encodeCOValue(%d) = 0x%X, want 0x%X", tt.offset, got, tt.want)
		}
	}
}

// TestUndervolterApplyRejectsOutOfRange pins that the driver's Apply validates
// against the bounds it was constructed with — before anything else, since even
// the availability probe inside SetCurveOptimizer is a hardware write. The fake
// SMU is installed anyway so a future reordering fails against the fake rather
// than writing the developer's actual voltage curve.
func TestUndervolterApplyRejectsOutOfRange(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	u := NewUndervolter(-40, 0)
	for _, cpu := range []int{-41, 1, 5, -100} {
		if err := u.Apply(cpu); err == nil {
			t.Errorf("Apply(%d) = nil, want an out-of-range error", cpu)
		}
	}
}

// TestUndervolterApplyWithinBounds covers the values the deleted package-level
// validation accepted: everything from the constructed minimum to 0 reaches the
// SMU, with the last offset encoded on the mailbox.
func TestUndervolterApplyWithinBounds(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	u := NewUndervolter(-40, 0)
	for _, cpu := range []int{0, -1, -20, -40} {
		if err := u.Apply(cpu); err != nil {
			t.Errorf("Apply(%d) = %v, want nil", cpu, err)
		}
	}
	got := binary.LittleEndian.Uint32(fake.args[:4])
	if want := encodeCOValue(-40); got != want {
		t.Errorf("last encoded offset = 0x%X, want 0x%X", got, want)
	}
}

// TestUndervolterRange pins that Range reports the constructed bounds — the
// numbers the CLI and GUI print come from here.
func TestUndervolterRange(t *testing.T) {
	lo, hi := NewUndervolter(-40, 0).Range()
	if lo != -40 || hi != 0 {
		t.Errorf("Range() = %d..%d, want -40..0", lo, hi)
	}
}

func TestSMUResponseError_OK(t *testing.T) {
	if err := smuResponseError(SMUReturnOK); err != nil {
		t.Errorf("smuResponseError(OK) = %v, want nil", err)
	}
}

func TestSMUResponseError_NonOK(t *testing.T) {
	codes := []uint32{SMUReturnFailed, SMUReturnUnknownCmd, SMUReturnRejected, SMUReturnBusy, 0x42}
	for _, c := range codes {
		if err := smuResponseError(c); err == nil {
			t.Errorf("smuResponseError(0x%X) = nil, want error", c)
		}
	}
}

// The command is the CPU's: ryzenadj's set_coall sends 0x55 to Renoir,
// Lucienne and Cezanne and 0x4C from Rembrandt on. Checked against the table
// transcribed from ryzenadj 0.19.0, through the cpuinfo the driver reads.
func TestCurveOptimizerCommandFollowsTheCPU(t *testing.T) {
	for _, tt := range []struct {
		vendor        string
		family, model int
		want          string // "" = refused
	}{
		{"AuthenticAMD", 0x1A, 112, "MP1 cmd 0x4C (Strix Halo)"},
		{"AuthenticAMD", 0x19, 116, "MP1 cmd 0x4C (Phoenix)"},
		{"AuthenticAMD", 0x19, 80, "MP1 cmd 0x55 (Cezanne)"},
		{"AuthenticAMD", 0x17, 96, "MP1 cmd 0x55 (Renoir)"},
		{"AuthenticAMD", 0x19, 97, ""},  // Dragon Range: PSMU, not transcribed
		{"AuthenticAMD", 0x17, 17, ""},  // Raven: ryzenadj has no CO command
		{"AuthenticAMD", 0x1A, 999, ""}, // a model nobody has listed yet
		{"GenuineIntel", 6, 154, ""},
	} {
		f := newFakeSysfs(t)
		f.withCPU(t, tt.vendor, tt.family, tt.model)
		got, err := CurveOptimizerCommand()
		if got != tt.want || (err == nil) != (tt.want != "") {
			t.Errorf("%s 0x%X/%d: CurveOptimizerCommand() = %q, %v; want %q", tt.vendor, tt.family, tt.model, got, err, tt.want)
		}
	}
}

func TestCurveOptimizerSendsTheCPUsCommand(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	f.withCPU(t, "AuthenticAMD", 0x19, 80) // Cezanne
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	if err := SetCurveOptimizer(-10); err != nil {
		t.Fatalf("SetCurveOptimizer(-10) = %v", err)
	}
	// The probe and the write, both with Cezanne's command and nothing else.
	if got := fake.cmds[MailboxMP1]; len(got) != 2 || got[0] != 0x55 || got[1] != 0x55 {
		t.Errorf("MP1 commands = %#v, want [0x55 0x55]", got)
	}
	if len(fake.cmds) != 1 {
		t.Errorf("commands went to %v, want only %s", fake.cmds, MailboxMP1)
	}
}

// The safety property: on a CPU the table does not list, nothing reaches the
// mailbox — not the write, not the reset, and not the probe, whose CO-zero is a
// write too. Availability reports false without needing to find out by writing.
func TestUnknownCPUNeverWritesTheMailbox(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.smu+"/rsmu_cmd", "")
	f.withCPU(t, "AuthenticAMD", 0x1A, 999)
	fake := &fakeSMU{response: SMUReturnOK}
	fake.install(t)

	if SMUUndervoltAvailable() {
		t.Error("SMUUndervoltAvailable() = true on an unknown CPU")
	}
	if SMUProbeUndervolt() {
		t.Error("SMUProbeUndervolt() = true on an unknown CPU")
	}
	err := SetCurveOptimizer(-10)
	if err == nil || !strings.Contains(err.Error(), "model 999") {
		t.Errorf("SetCurveOptimizer() = %v, want a refusal naming the CPU", err)
	}
	if err := ResetCurveOptimizer(); err == nil {
		t.Error("ResetCurveOptimizer() = nil on an unknown CPU")
	}
	if fake.writes != 0 {
		t.Errorf("mailbox writes = %d, want 0", fake.writes)
	}
}

func TestReadCPUModelTakesTheFirstProcessor(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, procCPUInfoPath, "processor\t: 0\nvendor_id\t: AuthenticAMD\ncpu family\t: 26\nmodel\t\t: 112\n\n"+
		"processor\t: 1\nvendor_id\t: AuthenticAMD\ncpu family\t: 25\nmodel\t\t: 80\n\n")
	vendor, m, err := readCPUModel()
	if err != nil || vendor != "AuthenticAMD" || m != (cpuModel{26, 112}) {
		t.Errorf("readCPUModel() = %q, %+v, %v; want AuthenticAMD {26 112}", vendor, m, err)
	}
	f.writeFile(t, procCPUInfoPath, "processor\t: 0\nvendor_id\t: AuthenticAMD\n")
	if _, _, err := readCPUModel(); err == nil {
		t.Error("readCPUModel() without family/model = nil error")
	}
}
