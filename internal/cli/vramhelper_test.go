package cli

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
)

// fakeFirmware answers DSTS with its running size and records DEVS writes, the
// way the GZ302EA does: the running size changes only at a reboot.
type fakeFirmware struct {
	current uint16
	options []uint16
	stored  []uint16
}

func gz302eaFirmware() *fakeFirmware {
	return &fakeFirmware{current: 0x10, options: []uint16{0, 1, 2, 4, 8, 0x10, 0x20, 0x40, 0x60}}
}

func (f *fakeFirmware) install(t *testing.T) {
	t.Helper()
	prev := acpiCall
	acpiCall = func(command string) (string, error) {
		switch {
		case strings.Contains(command, " 0x53545344 "):
			words := append([]uint16{uint16(len(f.options)), 0x3, f.current}, f.options...)
			parts := make([]string, 0, 2*len(words))
			for _, w := range words {
				parts = append(parts, fmt.Sprintf("0x%02x", w&0xff), fmt.Sprintf("0x%02x", w>>8))
			}
			return "{" + strings.Join(parts, ", ") + "}\x00", nil
		case strings.Contains(command, " 0x53564544 "):
			args, err := hex.DecodeString(command[strings.LastIndex(command, " b")+2:])
			if err != nil || len(args) < 8 {
				return "Error: AE_BAD_PARAMETER", nil
			}
			f.stored = append(f.stored, uint16(binary.LittleEndian.Uint32(args[4:])))
			return "0x1", nil
		}
		return "Error: AE_NOT_FOUND", nil
	}
	t.Cleanup(func() { acpiCall = prev })
}

// helperEnv points the run directory at a temp dir and sets the root check.
func helperEnv(t *testing.T, root bool) {
	t.Helper()
	prevDir, prevRoot := vramRunDir, isRoot
	vramRunDir = t.TempDir()
	isRoot = func() bool { return root }
	t.Cleanup(func() { vramRunDir, isRoot = prevDir, prevRoot })
}

func serve(t *testing.T, request string) vramHelperResponse {
	t.Helper()
	var out strings.Builder
	if err := ServeVRAMHelper(strings.NewReader(request+"\n"), &out); err != nil {
		t.Fatalf("ServeVRAMHelper: %v", err)
	}
	var resp vramHelperResponse
	if err := json.Unmarshal([]byte(out.String()), &resp); err != nil {
		t.Fatalf("reply %q: %v", out.String(), err)
	}
	return resp
}

func TestHelperGetReportsFirmwareState(t *testing.T) {
	helperEnv(t, true)
	gz302eaFirmware().install(t)

	resp := serve(t, `{"cmd":"vram-get"}`)
	if !resp.OK || resp.VRAM == nil {
		t.Fatalf("reply = %+v", resp)
	}
	if resp.VRAM.Current != "8G" || resp.VRAM.Next != "" {
		t.Errorf("state = %+v", *resp.VRAM)
	}
	if got := strings.Join(resp.VRAM.Options, ","); got != "auto,0.5G,1G,2G,4G,8G,16G,32G,48G" {
		t.Errorf("options = %s", got)
	}
}

func TestHelperSetRecordsNextBootAndCancels(t *testing.T) {
	helperEnv(t, true)
	fw := gz302eaFirmware()
	fw.install(t)

	resp := serve(t, `{"cmd":"vram","set":"0.5G"}`)
	if !resp.OK || resp.VRAM.Current != "8G" || resp.VRAM.Next != "0.5G" {
		t.Fatalf("set 0.5G reply = %+v %+v", resp, resp.VRAM)
	}
	if len(fw.stored) != 1 || fw.stored[0] != 1 {
		t.Errorf("firmware writes = %v, want [1]", fw.stored)
	}
	if got := serve(t, `{"cmd":"vram-get"}`); got.VRAM.Next != "0.5G" {
		t.Errorf("get after set: next = %q, want 0.5G", got.VRAM.Next)
	}

	// Setting the running size back must still reach the firmware, since 0.5G
	// is stored there, and must clear the record.
	resp = serve(t, `{"cmd":"vram","set":"8G"}`)
	if !resp.OK || resp.VRAM.Next != "" {
		t.Fatalf("set 8G reply = %+v %+v", resp, resp.VRAM)
	}
	if len(fw.stored) != 2 || fw.stored[1] != 0x10 {
		t.Errorf("firmware writes = %v, want [1 16]", fw.stored)
	}
	if _, err := os.Stat(vramNextPath()); !os.IsNotExist(err) {
		t.Errorf("next-boot record still present: %v", err)
	}
}

func TestHelperRefusesWithoutWriting(t *testing.T) {
	helperEnv(t, true)
	fw := gz302eaFirmware()
	fw.install(t)

	for _, req := range []string{
		`{"cmd":"vram","set":"3G"}`,
		`{"cmd":"vram","set":"64G"}`,
		`{"cmd":"vram","set":""}`,
		`{"cmd":"vram","set":"rm -rf /"}`,
		`{"cmd":"acpi","set":"\\_SB.FOO"}`,
		`not json`,
	} {
		if resp := serve(t, req); resp.OK || resp.Error == "" {
			t.Errorf("%s: reply = %+v, want an error", req, resp)
		}
	}
	if len(fw.stored) != 0 {
		t.Errorf("firmware writes = %v, want none", fw.stored)
	}
}

func TestHelperIgnoresBadNextRecord(t *testing.T) {
	helperEnv(t, true)
	gz302eaFirmware().install(t)

	for _, content := range []string{"3\n", "garbage", "99999"} {
		if err := os.WriteFile(vramNextPath(), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if resp := serve(t, `{"cmd":"vram-get"}`); resp.VRAM.Next != "" {
			t.Errorf("record %q: next = %q, want none", content, resp.VRAM.Next)
		}
	}
}

// listenHelper serves the helper protocol on VRAMHelperSocket, one goroutine
// per connection as systemd's Accept=yes gives one process per connection.
func listenHelper(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("unix", VRAMHelperSocket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_ = ServeVRAMHelper(bufio.NewReader(c), c)
			}()
		}
	}()
}

func TestNonRootGoesThroughHelper(t *testing.T) {
	helperEnv(t, false)
	fw := gz302eaFirmware()
	fw.install(t)
	listenHelper(t)

	info, err := VRAMGet()
	if err != nil || info.Current != "8G" {
		t.Fatalf("VRAMGet = %+v, %v", info, err)
	}
	info, err = VRAMSet("1G")
	if err != nil || info.Next != "1G" {
		t.Fatalf("VRAMSet = %+v, %v", info, err)
	}
	if _, err := VRAMSet("5G"); err == nil || !strings.Contains(err.Error(), "not an offered size") {
		t.Errorf("VRAMSet(5G) error = %v", err)
	}
	if len(fw.stored) != 1 || fw.stored[0] != 2 {
		t.Errorf("firmware writes = %v, want [2]", fw.stored)
	}
}

func TestNonRootWithoutHelperExplains(t *testing.T) {
	helperEnv(t, false)
	_, err := VRAMGet()
	if err == nil || !strings.Contains(err.Error(), "z13ctl-vram.socket") {
		t.Errorf("error = %v, want a pointer to z13ctl-vram.socket", err)
	}
}

func TestMatchVRAMOption(t *testing.T) {
	info := vramInfo(VRAMState{Current: 0x10, Options: gz302eaFirmware().options, UnitMB: 512}, 0, false)
	for in, want := range map[string]string{"auto": "auto", "512M": "0.5G", "0.5": "0.5G", "1g": "1G", "48GB": "48G"} {
		if got, err := MatchVRAMOption(in, info); err != nil || got != want {
			t.Errorf("MatchVRAMOption(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"3G", "0", "", "big"} {
		if got, err := MatchVRAMOption(in, info); err == nil {
			t.Errorf("MatchVRAMOption(%q) = %q, want an error", in, got)
		}
	}
}
