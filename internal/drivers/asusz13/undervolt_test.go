package asusz13

import (
	"encoding/binary"
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
