package cli

import (
	"errors"
	"strings"
	"testing"
)

// gz302eaVRAMReply is DSTS(0x000600C4) as read on a 64 GB GZ302EA with the
// reservation at 8 GB (BIOS 314).
const gz302eaVRAMReply = "{0x09, 0x00, 0x03, 0x00, 0x10, 0x00, 0x00, 0x00, 0x01, 0x00, 0x02, 0x00, 0x04, 0x00, 0x08, 0x00, 0x10, 0x00, 0x20, 0x00, 0x40, 0x00, 0x60, 0x00}"

// fakeACPI stands in for /proc/acpi/call: it answers DSTS with dsts and DEVS
// with devs, and records every command it was given.
type fakeACPI struct {
	dsts  string
	devs  string
	calls []string
}

func (f *fakeACPI) install(t *testing.T) {
	t.Helper()
	prev := acpiCall
	acpiCall = func(command string) (string, error) {
		f.calls = append(f.calls, command)
		switch {
		case strings.Contains(command, " 0x53545344 "):
			return f.dsts, nil
		case strings.Contains(command, " 0x53564544 "):
			return f.devs, nil
		}
		return "Error: AE_BAD_PARAMETER", nil
	}
	t.Cleanup(func() { acpiCall = prev })
}

func (f *fakeACPI) devsCalls() []string {
	var out []string
	for _, c := range f.calls {
		if strings.Contains(c, " 0x53564544 ") {
			out = append(out, c)
		}
	}
	return out
}

func TestReadVRAMDecodesGZ302EA(t *testing.T) {
	(&fakeACPI{dsts: gz302eaVRAMReply + "\x00\n"}).install(t)

	st, err := ReadVRAM()
	if err != nil {
		t.Fatal(err)
	}
	if st.UnitMB != 512 {
		t.Errorf("UnitMB = %d, want 512", st.UnitMB)
	}
	if got := st.Label(st.Current); got != "8G" {
		t.Errorf("current = %s, want 8G", got)
	}
	want := "auto,0.5G,1G,2G,4G,8G,16G,32G,48G"
	if got := strings.Join(st.OptionLabels(), ","); got != want {
		t.Errorf("options = %s, want %s", got, want)
	}
}

func TestReadVRAMRejectsBadReplies(t *testing.T) {
	cases := []struct {
		name, reply string
		unsupported bool
	}{
		{"integer zero (AUFI == 0)", "0x0", true},
		{"present bit clear", "{0x02, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00}", true},
		{"unsupported flag", "{0x02, 0x00, 0x0b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00}", true},
		{"truncated option list", "{0x09, 0x00, 0x03, 0x00, 0x10, 0x00, 0x00, 0x00}", false},
		{"too short", "{0x09, 0x00}", false},
		{"evaluation error", "Error: AE_AML_BUFFER_LIMIT", false},
		{"not called", "not called", false},
		{"garbage", "hello", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			(&fakeACPI{dsts: tc.reply}).install(t)
			_, err := ReadVRAM()
			if err == nil {
				t.Fatal("ReadVRAM accepted the reply")
			}
			if got := errors.Is(err, ErrVRAMUnsupported); got != tc.unsupported {
				t.Errorf("errors.Is(err, ErrVRAMUnsupported) = %v, want %v (err: %v)", got, tc.unsupported, err)
			}
		})
	}
}

func TestVRAMCommandBuffer(t *testing.T) {
	got := vramCommand(wmiMethodDEVS, 0x02)
	want := `\_SB.ATKD.WMNB 0x0 0x53564544 bC400060002000000` + strings.Repeat("0", 48)
	if got != want {
		t.Errorf("command =\n  %s\nwant\n  %s", got, want)
	}
	hexArgs := got[strings.LastIndex(got, " b")+2:]
	if len(hexArgs) != 2*wmiArgsLen {
		t.Errorf("argument buffer is %d bytes, want %d", len(hexArgs)/2, wmiArgsLen)
	}
}

func TestParseVRAMSize(t *testing.T) {
	(&fakeACPI{dsts: gz302eaVRAMReply}).install(t)
	st, err := ReadVRAM()
	if err != nil {
		t.Fatal(err)
	}

	accept := map[string]uint16{
		"auto": 0, "AUTO": 0,
		"512M": 1, "512mb": 1, "0.5G": 1, "0.5": 1,
		"1G": 2, "1": 2, "2g": 4, "4GB": 8, "8G": 0x10,
		"16G": 0x20, "32G": 0x40, "48G": 0x60,
	}
	for in, want := range accept {
		got, err := ParseVRAMSize(in, st)
		if err != nil || got != want {
			t.Errorf("ParseVRAMSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if got, err := ParseVRAMSize(" 48g ", st); err != nil || got != 0x60 {
		t.Errorf("ParseVRAMSize with surrounding spaces = %d, %v; want 96", got, err)
	}

	for _, in := range []string{"", "0", "3G", "24G", "64G", "96G", "0.3G", "100M", "1.5G", "-1G", "8X", "G", "1e9"} {
		if v, err := ParseVRAMSize(in, st); err == nil {
			t.Errorf("ParseVRAMSize(%q) = %d, want an error", in, v)
		}
	}
}

func TestSetVRAMWritesOfferedValue(t *testing.T) {
	f := &fakeACPI{dsts: gz302eaVRAMReply, devs: "0x1"}
	f.install(t)

	if err := SetVRAM(0x02); err != nil {
		t.Fatal(err)
	}
	writes := f.devsCalls()
	if len(writes) != 1 || writes[0] != vramCommand(wmiMethodDEVS, 0x02) {
		t.Errorf("DEVS calls = %q", writes)
	}
}

func TestSetVRAMRefusesUnlistedValueWithoutWriting(t *testing.T) {
	f := &fakeACPI{dsts: gz302eaVRAMReply, devs: "0x1"}
	f.install(t)

	for _, v := range []uint16{0x03, 0x30, 0x80, 0xC0, 0xFFFF} {
		if err := SetVRAM(v); err == nil {
			t.Errorf("SetVRAM(%#x) succeeded", v)
		}
	}
	if w := f.devsCalls(); len(w) != 0 {
		t.Errorf("unlisted values reached the firmware: %q", w)
	}
}

func TestSetVRAMDoesNotWriteWhenFirmwareOffersNothing(t *testing.T) {
	f := &fakeACPI{dsts: "0x0", devs: "0x1"}
	f.install(t)

	if err := SetVRAM(0x02); !errors.Is(err, ErrVRAMUnsupported) {
		t.Errorf("SetVRAM error = %v, want ErrVRAMUnsupported", err)
	}
	if w := f.devsCalls(); len(w) != 0 {
		t.Errorf("DEVS was called: %q", w)
	}
}

func TestSetVRAMReportsRefusal(t *testing.T) {
	for _, reply := range []string{"0x0", "Error: AE_AML_OPERAND_TYPE", "{0x01}"} {
		(&fakeACPI{dsts: gz302eaVRAMReply, devs: reply}).install(t)
		if err := SetVRAM(0x02); err == nil {
			t.Errorf("DEVS reply %q treated as success", reply)
		}
	}
}

func TestACPICallProcMissingModule(t *testing.T) {
	_, err := acpiCallProc("x")
	if err == nil || !strings.Contains(err.Error(), "acpi-call-dkms") {
		t.Errorf("error = %v, want a pointer to acpi-call-dkms", err)
	}
}

func TestVRAMLabelOneMBUnits(t *testing.T) {
	st := VRAMState{UnitMB: 1}
	for v, want := range map[uint16]string{0: "auto", 512: "0.5G", 1024: "1G", 1536: "1.5G", 768: "768M"} {
		if got := st.Label(v); got != want {
			t.Errorf("Label(%d) = %s, want %s", v, got, want)
		}
	}
}
