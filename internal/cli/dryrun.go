package cli

// dryrun.go — packet display for --dry-run mode.
//
// Each function prints the exact sequence of 64-byte HID reports that would
// be sent to the device, without opening any hardware.

import (
	"fmt"
	"os"

	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/hid"
)

// auraReportID is the HID Report ID byte that begins every Aura packet (0x5d).
// Duplicated here so dry-run display doesn't need to export it from the aura
// package, where it is an implementation detail.
const auraReportID = 0x5d

// printPacket prints a labeled 64-byte packet as hex, for dry-run output.
func printPacket(label string, data []byte) {
	buf := make([]byte, hid.ReportSize)
	copy(buf, data)
	fmt.Printf("  %-22s  %X\n", label+":", buf)
}

// printInitPackets prints the four Aura init packets sent before every device operation.
func printInitPackets() {
	printPacket("Init 1", []byte{auraReportID, 0xB9})
	printPacket("Init 2", []byte("]ASUS Tech.Inc."))
	printPacket("Init 3", []byte{auraReportID, 0x05, 0x20, 0x31, 0x00, 0x1A})
	printPacket("Init 4 (Z13)", []byte{auraReportID, 0xC0, 0x03, 0x01})
}

// DryRunApply prints the packet sequence for an apply operation.
// All values must be pre-parsed by the caller.
func DryRunApply(r, g, b, r2, g2, b2 uint8, mode aura.Mode, speed aura.Speed, brightness uint8) {
	var randFlag byte
	if r == 0 && g == 0 && b == 0 {
		randFlag = 0xFF
	} else if mode == aura.ModeBreathe {
		randFlag = 0x01
	}

	fmt.Println("=== DRY RUN (no device access) ===")
	printInitPackets()
	printPacket("Power ON", []byte{auraReportID, 0xBD, 0x01, 0xFF, 0x1F, 0xFF, 0xFF, 0xFF})
	printPacket("Brightness", []byte{auraReportID, 0xBA, 0xC5, 0xC4, brightness})
	for _, z := range []uint8{0, 1} { // z13Zones: keyboard=0, lightbar=1
		label := fmt.Sprintf("SetMode z%d (0xb3)", z)
		printPacket(label, []byte{
			auraReportID, 0xB3, z, byte(mode),
			r, g, b, byte(speed), 0x00, randFlag, r2, g2, b2,
		})
		printPacket("MESSAGE_SET (0xb5)", []byte{auraReportID, 0xB5, 0x00, 0x00, 0x00})
		printPacket("MESSAGE_APPLY (0xb4)", []byte{auraReportID, 0xB4})
	}
}

// DryRunOff prints the packet sequence for turning lighting off.
func DryRunOff() {
	fmt.Println("=== DRY RUN (no device access) ===")
	printInitPackets()
	printPacket("Power OFF", []byte{auraReportID, 0xBD, 0x01, 0x00, 0x00, 0x00, 0x00, 0xFF})
	printPacket("Brightness 0", []byte{auraReportID, 0xBA, 0xC5, 0xC4, 0x00})
}

// DryRunBatteryLimit prints the sysfs write that would be performed for a battery limit change.
func DryRunBatteryLimit(limit int) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would write %d to %s\n", limit, FindBatteryThresholdPath())
}

// DryRunProfile prints the sysfs writes that would be performed for a profile change,
// including mapped names for secondary devices (e.g. amd-pmf uses "low-power" not "quiet").
func DryRunProfile(profile string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	primary := FindProfilePath()
	fmt.Printf("Would write %q to %s\n", profile, primary)
	const dir = "/sys/class/platform-profile"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		base := dir + "/" + e.Name()
		p := base + "/profile"
		if p == primary {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		name := profileNameForDevice(base, profile)
		fmt.Printf("Would write %q to %s\n", name, p)
	}
	ppd := map[string]string{
		"quiet":       "power-saver",
		"balanced":    "balanced",
		"performance": "performance",
	}[profile]
	if ppd != "" {
		fmt.Printf("Would run: powerprofilesctl set %s\n", ppd)
	}
}

// DryRunBrightness prints the packet sequence for a brightness-only change.
func DryRunBrightness(level uint8) {
	var keyb, bar, lid, rear byte
	if level > 0 {
		keyb, bar, lid, rear = 0xFF, 0x1F, 0xFF, 0xFF
	}
	fmt.Println("=== DRY RUN (no device access) ===")
	fmt.Printf("Would send: brightness (level %d)\n", level)
	printInitPackets()
	printPacket("Power", []byte{auraReportID, 0xBD, 0x01, keyb, bar, lid, rear, 0xFF})
	printPacket("Brightness", []byte{auraReportID, 0xBA, 0xC5, 0xC4, level})
}
