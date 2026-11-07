package cli

// vram.go — iGPU memory reservation (the BIOS "UMA Frame Buffer Size", shown in
// Armoury Crate as "Free up memory") through the ASUS ATK WMI method.
//
// The 2025 Z13 keeps this setting at WMI device 0x000600C4. The kernel's
// asus-armoury driver only knows the ROG Ally's device (0x000600C1, its
// apu_mem attribute), which this firmware does not implement, so there is no
// sysfs file for it. z13ctl evaluates \_SB.ATKD.WMNB directly through the
// acpi_call module's /proc/acpi/call instead.
//
// DSTS on 0x000600C4 returns a buffer of little-endian u16 words:
//
//	[0] number of options
//	[1] flags: bit 0 present, bit 1 options are in 512 MB units (else 1 MB)
//	[2] the value in use this boot (not one DEVS stored for the next)
//	[3...] the options, the first one 0 (Auto)
//
// or the integer 0 when the firmware offers no setting. DEVS writes a value
// for the next boot and returns 1 on success.
//
// The firmware hands the DEVS value to the BIOS without checking it against the
// options it listed, so SetVRAM only ever sends a value read back from DSTS in
// the same call.
//
// /proc/acpi/call is left root-only on purpose: write access to it is the
// ability to run any ACPI method, so z13ctl setup does not grant it to a group.
// Non-root callers go through the z13ctl-vram helper instead (vramhelper.go).

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dahui/z13ctl/api"
)

const (
	vramDeviceID  uint32 = 0x000600C4
	wmiMethodDSTS        = 0x53545344
	wmiMethodDEVS        = 0x53564544
	atkWMNBMethod        = `\_SB.ATKD.WMNB`

	// WMNB reads five DWORD fields out of its argument buffer before
	// dispatching; a shorter buffer aborts the method with an AML error.
	wmiArgsLen = 32

	vramFlagPresent     = 0x1
	vramFlag512MBUnits  = 0x2
	vramFlagUnsupported = 0x8
)

// ErrVRAMUnsupported means the firmware answered but offers no VRAM setting.
var ErrVRAMUnsupported = errors.New("the firmware does not offer an iGPU memory setting on this machine")

// acpiCall evaluates one acpi_call command and returns its result text. A var
// so tests can stand in for the kernel module.
var acpiCall = acpiCallProc

// VRAMState is the firmware's view of the iGPU memory reservation. Values are
// in units of UnitMB; 0 means Auto.
type VRAMState struct {
	Current uint16
	Options []uint16
	UnitMB  int
}

// Label formats a firmware value as a size: "auto", "0.5G", "8G", "48G".
func (s VRAMState) Label(value uint16) string {
	if value == 0 {
		return "auto"
	}
	mb := int(value) * s.UnitMB
	if mb%1024 == 0 {
		return fmt.Sprintf("%dG", mb/1024)
	}
	if mb%512 == 0 {
		return strconv.FormatFloat(float64(mb)/1024, 'f', 1, 64) + "G"
	}
	return fmt.Sprintf("%dM", mb)
}

// Offers reports whether value is one of the firmware's options.
func (s VRAMState) Offers(value uint16) bool {
	for _, o := range s.Options {
		if o == value {
			return true
		}
	}
	return false
}

// OptionLabels returns the options in firmware order, formatted with Label.
func (s VRAMState) OptionLabels() []string {
	labels := make([]string, len(s.Options))
	for i, o := range s.Options {
		labels[i] = s.Label(o)
	}
	return labels
}

// ReadVRAM asks the firmware for the current reservation and the sizes it offers.
func ReadVRAM() (VRAMState, error) {
	out, err := acpiCall(vramCommand(wmiMethodDSTS, 0))
	if err != nil {
		return VRAMState{}, err
	}
	res, err := parseACPICallResult(out)
	if err != nil {
		return VRAMState{}, err
	}
	if res.buffer == nil {
		if res.integer == 0 {
			return VRAMState{}, ErrVRAMUnsupported
		}
		return VRAMState{}, fmt.Errorf("unexpected firmware reply %q (wanted a buffer)", out)
	}
	return decodeVRAM(res.buffer)
}

// SetVRAM stores value as the reservation for the next boot. It reads the
// options first and refuses anything the firmware did not list.
func SetVRAM(value uint16) error {
	st, err := ReadVRAM()
	if err != nil {
		return err
	}
	if !st.Offers(value) {
		return fmt.Errorf("value %d is not one of the firmware's options (%s)", value, strings.Join(st.OptionLabels(), ", "))
	}
	out, err := acpiCall(vramCommand(wmiMethodDEVS, uint32(value)))
	if err != nil {
		return err
	}
	res, err := parseACPICallResult(out)
	if err != nil {
		return err
	}
	if res.buffer != nil || res.integer != 1 {
		return fmt.Errorf("the firmware refused the change (reply %q)", out)
	}
	return nil
}

// ParseVRAMSize turns "auto", "512M", "0.5G", "8g", "8GB" or a bare number of
// GB into a firmware value, and checks it is one of st's options.
func ParseVRAMSize(s string, st VRAMState) (uint16, error) {
	mb, err := parseSizeMB(s)
	if err != nil {
		return 0, err
	}
	notOffered := fmt.Errorf("%s is not an offered size (%s)", s, strings.Join(st.OptionLabels(), ", "))
	if st.UnitMB <= 0 || mb%st.UnitMB != 0 || mb/st.UnitMB > 0xFFFF {
		return 0, notOffered
	}
	value := uint16(mb / st.UnitMB)
	if !st.Offers(value) {
		return 0, notOffered
	}
	return value, nil
}

// MatchVRAMOption returns the option in info that s names, in any of the forms
// ParseVRAMSize accepts. Callers without firmware access use it to check a size
// before asking the helper, which checks again against the firmware.
func MatchVRAMOption(s string, info api.VRAMState) (string, error) {
	mb, err := parseSizeMB(s)
	if err != nil {
		return "", err
	}
	for _, o := range info.Options {
		if omb, err := parseSizeMB(o); err == nil && omb == mb {
			return o, nil
		}
	}
	return "", fmt.Errorf("%s is not an offered size (%s)", s, strings.Join(info.Options, ", "))
}

// parseSizeMB reads a size as megabytes; "auto" is 0.
func parseSizeMB(s string) (int, error) {
	in := strings.ToLower(strings.TrimSpace(s))
	if in == "auto" {
		return 0, nil
	}
	mbPerUnit := 1024.0
	num := in
	switch {
	case strings.HasSuffix(in, "mb"), strings.HasSuffix(in, "m"):
		mbPerUnit = 1
		num = strings.TrimSuffix(strings.TrimSuffix(in, "b"), "m")
	case strings.HasSuffix(in, "gb"), strings.HasSuffix(in, "g"):
		num = strings.TrimSuffix(strings.TrimSuffix(in, "b"), "g")
	}
	f, err := strconv.ParseFloat(num, 64)
	mb := f * mbPerUnit
	if err != nil || f <= 0 || mb > 1<<30 || mb != float64(int(mb)) {
		return 0, fmt.Errorf("invalid size %q: use auto or a size such as 512M, 0.5G or 8G", s)
	}
	return int(mb), nil
}

// vramCommand builds the acpi_call line for a WMNB call on the VRAM device.
func vramCommand(method, arg uint32) string {
	args := make([]byte, wmiArgsLen)
	binary.LittleEndian.PutUint32(args[0:], vramDeviceID)
	binary.LittleEndian.PutUint32(args[4:], arg)
	return fmt.Sprintf("%s 0x0 0x%08X b%s", atkWMNBMethod, method, strings.ToUpper(hex.EncodeToString(args)))
}

func decodeVRAM(buf []byte) (VRAMState, error) {
	if len(buf) < 6 {
		return VRAMState{}, fmt.Errorf("firmware reply too short (%d bytes)", len(buf))
	}
	word := func(i int) uint16 { return binary.LittleEndian.Uint16(buf[2*i:]) }
	count := int(word(0))
	flags := word(1)
	if flags&vramFlagPresent == 0 || flags&vramFlagUnsupported != 0 {
		return VRAMState{}, ErrVRAMUnsupported
	}
	if count < 2 || len(buf) < 6+2*count {
		return VRAMState{}, fmt.Errorf("firmware reply lists %d options in %d bytes", count, len(buf))
	}
	st := VRAMState{Current: word(2), UnitMB: 1}
	if flags&vramFlag512MBUnits != 0 {
		st.UnitMB = 512
	}
	st.Options = make([]uint16, count)
	for i := range st.Options {
		st.Options[i] = word(3 + i)
	}
	return st, nil
}

type acpiCallResult struct {
	buffer  []byte
	integer uint64
}

// parseACPICallResult reads acpi_call's output: "{0x09, 0x00, ...}" for a
// buffer, "0x1" for an integer, "Error: ..." when evaluation failed and
// "not called" when nothing was evaluated.
func parseACPICallResult(out string) (acpiCallResult, error) {
	s := strings.Trim(out, " \t\r\n\x00")
	switch {
	case strings.HasPrefix(s, "Error:"):
		return acpiCallResult{}, fmt.Errorf("ACPI evaluation failed: %s", strings.TrimSpace(strings.TrimPrefix(s, "Error:")))
	case s == "not called":
		return acpiCallResult{}, errors.New("acpi_call did not evaluate the method")
	case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
		body := strings.TrimSpace(s[1 : len(s)-1])
		if body == "" {
			return acpiCallResult{buffer: []byte{}}, nil
		}
		parts := strings.Split(body, ",")
		buf := make([]byte, len(parts))
		for i, p := range parts {
			v, err := strconv.ParseUint(strings.TrimSpace(p), 0, 8)
			if err != nil {
				return acpiCallResult{}, fmt.Errorf("unexpected byte %q in firmware reply", strings.TrimSpace(p))
			}
			buf[i] = byte(v)
		}
		return acpiCallResult{buffer: buf}, nil
	}
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		return acpiCallResult{}, fmt.Errorf("unexpected acpi_call output %q", s)
	}
	return acpiCallResult{integer: v}, nil
}

// acpiCallProc writes the command and reads the result through separate opens:
// the module keeps one result per call and resets it to "not called" once read.
func acpiCallProc(command string) (string, error) {
	if err := os.WriteFile(acpiCallPath, []byte(command), 0); err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return "", fmt.Errorf("%s not found: install acpi-call-dkms and run 'sudo modprobe acpi_call'", acpiCallPath)
		case errors.Is(err, os.ErrPermission):
			return "", fmt.Errorf("%s needs root: run this command with sudo", acpiCallPath)
		}
		return "", err
	}
	out, err := os.ReadFile(acpiCallPath)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
