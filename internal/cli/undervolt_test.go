package cli

import "testing"

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

func TestValidateCOValues_Valid(t *testing.T) {
	tests := []int{0, -1, -20, -40}
	for _, cpu := range tests {
		if err := ValidateCOValues(cpu); err != nil {
			t.Errorf("ValidateCOValues(%d) = %v, want nil", cpu, err)
		}
	}
}

func TestValidateCOValues_CPUTooLow(t *testing.T) {
	if err := ValidateCOValues(-41); err == nil {
		t.Error("expected error for CPU offset -41")
	}
}

func TestValidateCOValues_CPUPositive(t *testing.T) {
	if err := ValidateCOValues(1); err == nil {
		t.Error("expected error for positive CPU offset")
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
