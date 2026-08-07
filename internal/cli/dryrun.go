package cli

// dryrun.go — packet display for --dry-run mode.
//
// Each function prints the exact sequence of 64-byte HID reports that would
// be sent to the device, without opening any hardware.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dahui/z13ctl/api"
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

	// A custom profile is never written to platform_profile — this printed the
	// name as a platform_profile write for every release up to now, describing
	// something the daemon has never done.
	if !IsStockProfile(profile) {
		fmt.Printf("Would recall custom profile %q from daemon state and apply its\n", profile)
		fmt.Println("  saved fan curve, TDP, and Curve Optimizer offset")
		fmt.Println("Would NOT write platform_profile (custom profiles leave it to the desktop)")
		return
	}

	fmt.Println("Would reset the CPU Curve Optimizer to stock")
	primary := FindProfilePath()
	// Name-mapped, as SetProfile does for every device including the primary —
	// printing the raw name here showed "quiet" where "low-power" gets written.
	fmt.Printf("Would write %q to %s\n", profileNameForDevice(filepath.Dir(primary), profile), primary)
	dir := sysProfileDir
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
	// Switching to a stock profile also restores that profile's PPT values,
	// since the firmware does not re-apply them on a platform_profile write.
	// Fans are released last, after the limit has been lowered.
	if stock, ok := StockProfilePPT[profile]; ok {
		fmt.Printf("Would write stock PPT for %s: PL1=%dW PL2=%dW PL3=%dW APU=%dW Platform=%dW\n",
			profile, stock.PL1SPL, stock.PL2SPPT, stock.FPPT, stock.APUSPPT, stock.PlatformSPPT)
		fmt.Printf("Would reset fan curves to auto (pwm_enable=2)\n")
	}
}

// DryRunProfileEdit prints what storing a setting in a profile that is not
// running would do. Nothing reaches hardware on that path, so printing the
// ordinary sysfs write list would describe the opposite of what happens.
func DryRunProfileEdit(profile, setting string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would store the %s in custom profile %q in daemon state\n", setting, profile)
	fmt.Println("Would NOT write hardware — the setting takes effect when that profile")
	fmt.Printf("  is activated: z13ctl profile --set %s\n", profile)
}

// DryRunProfileCreate prints what creating an empty custom profile would do.
// Custom profiles live in daemon state, not sysfs, so there is nothing to write.
func DryRunProfileCreate(name string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would create empty custom profile %q in daemon state\n", name)
	fmt.Println("Would NOT change the active profile or touch hardware")
}

// DryRunProfileSave prints what copying the active profile would do.
func DryRunProfileSave(name string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would copy the active custom profile to %q in daemon state\n", name)
	fmt.Println("Would NOT change the active profile or touch hardware")
}

// DryRunProfileDelete prints what deleting a custom profile would do.
func DryRunProfileDelete(name string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would delete custom profile %q from daemon state\n", name)
	fmt.Println("  (refused if it is the active profile or referenced by autoswitch)")
}

// DryRunAutoswitch prints the AC/battery configuration that would be stored.
// It stores configuration only — the daemon applies a profile on the next
// power-source transition, not now.
func DryRunAutoswitch(enabled bool, ac, battery string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	if !enabled {
		fmt.Println("Would disable AC/battery autoswitching in daemon state")
		return
	}
	fmt.Printf("Would store autoswitch in daemon state: AC=%s battery=%s\n",
		dryRunTarget(ac), dryRunTarget(battery))
	fmt.Printf("Would read the power source from %s\n", dryRunACPath())
	fmt.Println("Would NOT change the profile now — the daemon applies one on the next")
	fmt.Println("  plug or unplug")
}

func dryRunTarget(name string) string {
	if name == "" {
		return "(unchanged)"
	}
	return name
}

func dryRunACPath() string {
	if p := FindACOnlinePath(); p != "" {
		return p
	}
	return "(no mains power supply found)"
}

// DryRunBootSound prints the sysfs write that would be performed for a boot sound change.
func DryRunBootSound(value int) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would write %d to %s\n", value, FindBootSoundPath())
}

// DryRunPanelOverdrive prints the sysfs write that would be performed for a panel overdrive change.
func DryRunPanelOverdrive(value int) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would write %d to %s\n", value, FindPanelOverdrivePath())
}

// DryRunFanCurve prints the sysfs writes for a fan curve set operation.
// The same curve is written to both fans.
func DryRunFanCurve(points []api.FanCurvePoint) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	curveDir := FindFanCurveHwmonPath()
	if curveDir == "" {
		curveDir = "<hwmon not found>"
	}
	for _, f := range fanNames {
		for i, p := range points {
			fmt.Printf("Would write %d to %s/pwm%d_auto_point%d_temp\n", p.Temp, curveDir, f.index, i+1)
			fmt.Printf("Would write %d to %s/pwm%d_auto_point%d_pwm\n", p.PWM, curveDir, f.index, i+1)
		}
		fmt.Printf("Would write 1 (custom) to %s/pwm%d_enable\n", curveDir, f.index)
	}
	fmt.Printf("Would read %s/pwm*_enable back to confirm the kernel kept the curve\n", curveDir)
}

// DryRunFanCurveReset prints the sysfs writes for a fan curve reset (both fans).
func DryRunFanCurveReset() {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	curveDir := FindFanCurveHwmonPath()
	if curveDir == "" {
		curveDir = "<hwmon not found>"
	}
	for _, f := range fanNames {
		fmt.Printf("Would write 2 (auto) to %s/pwm%d_enable\n", curveDir, f.index)
	}
}

// DryRunTdp prints the sysfs writes for a TDP set operation.
//
// The limits come from TDPStateFor and the fan state from the same
// FanCurveForTDP / FloorAdjustsCurve pair ApplyTDPSafely uses, so this cannot
// drift from what the real path does. It previously claimed the fans would go to
// *full speed* (pwm_enable=0) whenever --force was given and any limit exceeded
// the safe max — three separate inaccuracies: the floor is a curve, not full
// speed; it is driven by the sustained limit alone; and it does not depend on
// --force. It then claimed the whole floor curve would always be written above the
// safe max, which stopped being true once the floor became a per-point minimum
// rather than a replacement curve.
func DryRunTdp(watts, pl1, pl2, pl3 int, force bool) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	s := TDPStateFor(watts, pl1, pl2, pl3)
	if force {
		fmt.Printf("--force given: sustained limit allowed above %dW (hardware max %dW)\n",
			TDPMaxSafe, TDPMaxForced)
	}
	if s.PL1SPL > TDPMaxSafe {
		curveDir := FindFanCurveHwmonPath()
		if curveDir == "" {
			curveDir = "<hwmon not found>"
		}
		live := LiveFanCurve()
		switch {
		case len(live) == 0:
			fmt.Printf("Would write the high-TDP fan curve (minimum %d PWM / 50%%) to both fans in %s\n",
				HighTDPMinPWM, curveDir)
		case FloorAdjustsCurve(s.PL1SPL, live):
			fmt.Printf("Would raise the current fan curve's points below %d PWM (50%%) to that floor,\n",
				HighTDPMinPWM)
			fmt.Printf("  leaving every other point as-is, and write it to both fans in %s\n", curveDir)
		default:
			fmt.Printf("Would write the current fan curve back to both fans in %s unchanged:\n", curveDir)
			fmt.Printf("  it already clears the %d PWM (50%%) floor at every point\n", HighTDPMinPWM)
		}
		fmt.Printf("Would write 1 (custom) to %s/pwm{1,2}_enable\n", curveDir)
		fmt.Printf("  (sustained %dW is above the %dW safe max; if the fan write fails the TDP is not applied at all)\n",
			s.PL1SPL, TDPMaxSafe)
	}
	base := FindPPTBasePath()
	for _, w := range []struct {
		attr  string
		watts int
	}{
		{"ppt_pl1_spl", s.PL1SPL},
		{"ppt_pl2_sppt", s.PL2SPPT},
		{"ppt_fppt", s.FPPT},
		{"ppt_apu_sppt", s.APUSPPT},
		{"ppt_platform_sppt", s.PlatformSPPT},
	} {
		fmt.Printf("Would write %d to %s/%s\n", w.watts, base, w.attr)
	}
}

// DryRunTdpReset prints the actions for a TDP reset, in the order the real path
// performs them.
//
// It used to claim the firmware sets per-profile PPT on a profile change. It
// does not — that false assumption is the whole of issue #12, and z13ctl writes
// the stock values itself. The order matters too: power is lowered before the
// fans are released, never the other way round.
func DryRunTdpReset() {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Println("Would reset the CPU Curve Optimizer to stock (balanced is a stock profile)")
	fmt.Println("Would switch profile to balanced")
	stock := StockProfilePPT["balanced"]
	fmt.Printf("Would write stock PPT for balanced: PL1=%dW PL2=%dW PL3=%dW APU=%dW Platform=%dW\n",
		stock.PL1SPL, stock.PL2SPPT, stock.FPPT, stock.APUSPPT, stock.PlatformSPPT)
	fmt.Println("Would reset fan curves to auto mode (after the limit is lowered, not before)")
}

// DryRunUndervolt prints the SMU commands that would be sent for a Curve
// Optimizer change.
//
// An offset of 0 is not "no change": it encodes to the same argument as
// ResetCurveOptimizer, so "--set 0" clears any offset currently applied. Saying
// "no changes" here told users the opposite of what the command does.
func DryRunUndervolt(cpu int) {
	fmt.Println("=== DRY RUN (no SMU write) ===")
	encoded := encodeCOValue(cpu)
	if cpu == 0 {
		fmt.Printf("Would send MP1 cmd 0x4C with arg 0x%X (CPU CO 0 — clears any active undervolt)\n", encoded)
		return
	}
	fmt.Printf("Would send MP1 cmd 0x4C with arg 0x%X (CPU CO %d)\n", encoded, cpu)
}

// DryRunUndervoltReset prints the SMU commands that would be sent to reset CO.
func DryRunUndervoltReset() {
	fmt.Println("=== DRY RUN (no SMU write) ===")
	encoded := encodeCOValue(0)
	fmt.Printf("Would send MP1 cmd 0x4C with arg 0x%X (reset CPU CO)\n", encoded)
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
