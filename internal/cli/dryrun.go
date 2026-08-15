package cli

// dryrun.go — display for --dry-run mode: the exact HID reports, sysfs writes
// and SMU commands a command would perform, without touching any hardware.
//
// Device limits (the power envelope) come in as parameters — mirroring how the
// real paths get them from the assembled device — while the sysfs *paths* are
// asked of the asusz13 driver directly: a dry run's job is to spell out what
// this machine's driver would write, and the paths are that driver's own
// knowledge. Per-device dry-run display becomes driver data in the
// capability-protocol milestone.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/aura"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/asusz13"
	"github.com/dahui/voltaire/v2/internal/hid"
	"github.com/dahui/voltaire/v2/internal/safety"
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
	fmt.Printf("Would write %d to %s\n", limit, asusz13.FindBatteryThresholdPath())
}

// DryRunProfile prints the sysfs writes that would be performed for a profile
// change, including mapped names for secondary devices (e.g. amd-pmf uses
// "low-power" not "quiet"). env supplies the stock PPT rows the switch would
// restore.
func DryRunProfile(env driver.PowerEnvelope, profile string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")

	// A custom profile is never written to platform_profile — this printed the
	// name as a platform_profile write for every release up to now, describing
	// something the daemon has never done.
	if !api.IsStockProfileName(profile) {
		fmt.Printf("Would recall custom profile %q from daemon state and apply its\n", profile)
		fmt.Println("  saved fan curve, TDP, and Curve Optimizer offset")
		fmt.Println("Would NOT write platform_profile (custom profiles leave it to the desktop)")
		return
	}

	fmt.Println("Would reset the CPU Curve Optimizer to stock")
	primary := asusz13.FindProfilePath()
	// Name-mapped, as SetProfile does for every device including the primary —
	// printing the raw name here showed "quiet" where "low-power" gets written.
	fmt.Printf("Would write %q to %s\n", asusz13.ProfileNameForDevice(filepath.Dir(primary), profile), primary)
	dir := asusz13.SysProfileDir()
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
		name := asusz13.ProfileNameForDevice(base, profile)
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
	if stock, ok := env.StockProfilePPT[profile]; ok {
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
	fmt.Printf("  is activated: voltaire profile --set %s\n", profile)
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

// DryRunAutoswitch prints the AC/battery configuration that would be stored,
// and the enable-time one-shot that follows it.
//
// It used to promise "Would NOT change the profile now", which is false:
// powerTick's justEnabled branch applies the target for the source the machine
// is already on when autoswitch goes from off to on. A hardware smoke run
// caught it — enabling autoswitch moved a live custom profile to balanced while
// the dry run said nothing would happen. Turning it *off*, or re-configuring an
// autoswitch that is already on, genuinely changes no profile.
func DryRunAutoswitch(enabled bool, ac, battery string) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	if !enabled {
		fmt.Println("Would disable AC/battery autoswitching in daemon state")
		return
	}
	fmt.Printf("Would store autoswitch in daemon state: AC=%s battery=%s\n",
		dryRunTarget(ac), dryRunTarget(battery))
	fmt.Printf("Would read the power source from %s\n", dryRunACPath())
	fmt.Println("Would apply that source's profile now if this turns autoswitch on: enabling")
	fmt.Println("  it is a one-shot for the source you are already on. After that the daemon")
	fmt.Println("  only acts on an actual plug or unplug.")
}

func dryRunTarget(name string) string {
	if name == "" {
		return "(unchanged)"
	}
	return name
}

func dryRunACPath() string {
	if p := asusz13.FindACOnlinePath(); p != "" {
		return p
	}
	return "(no mains power supply found)"
}

// DryRunBootSound prints the sysfs write that would be performed for a boot sound change.
func DryRunBootSound(value int) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would write %d to %s\n", value, asusz13.FindBootSoundPath())
}

// DryRunCPUBoost prints the sysfs write that would be performed for a CPU
// boost change.
func DryRunCPUBoost(on bool) {
	v := 0
	if on {
		v = 1
	}
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would write %d to %s\n", v, asusz13.CPUBoostPath())
	fmt.Println("  (this moves every cpufreq policy at once)")
}

// DryRunPanelOverdrive prints the sysfs write that would be performed for a panel overdrive change.
func DryRunPanelOverdrive(value int) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Printf("Would write %d to %s\n", value, asusz13.FindPanelOverdrivePath())
}

// DryRunFeature prints the sysfs write a generic firmware-toggle set would
// perform. An id the driver has no path mapping for still prints something
// truthful rather than an empty path.
func DryRunFeature(id string, value int) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	path := asusz13.FindTogglePath(id)
	if path == "" {
		path = fmt.Sprintf("<no %s attribute on this driver>", id)
	}
	fmt.Printf("Would write %d to %s\n", value, path)
}

// DryRunFanCurve prints the sysfs writes for a fan curve set operation.
// The same curve is written to both fans.
func DryRunFanCurve(points []api.FanCurvePoint) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	curveDir := asusz13.FindFanCurveHwmonPath()
	if curveDir == "" {
		curveDir = "<hwmon not found>"
	}
	for _, idx := range asusz13.FanPWMIndices() {
		for i, p := range points {
			fmt.Printf("Would write %d to %s/pwm%d_auto_point%d_temp\n", p.Temp, curveDir, idx, i+1)
			fmt.Printf("Would write %d to %s/pwm%d_auto_point%d_pwm\n", p.PWM, curveDir, idx, i+1)
		}
		fmt.Printf("Would write 1 (custom) to %s/pwm%d_enable\n", curveDir, idx)
	}
	fmt.Printf("Would read %s/pwm*_enable back to confirm the kernel kept the curve\n", curveDir)
}

// DryRunFanCurveReset prints the sysfs writes for a fan curve reset (both fans).
func DryRunFanCurveReset() {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	curveDir := asusz13.FindFanCurveHwmonPath()
	if curveDir == "" {
		curveDir = "<hwmon not found>"
	}
	for _, idx := range asusz13.FanPWMIndices() {
		fmt.Printf("Would write 2 (auto) to %s/pwm%d_enable\n", curveDir, idx)
	}
}

// DryRunTdp prints the sysfs writes for a TDP set operation.
//
// The limits come from TDPStateFor and the fan state from the same
// FanCurveForTDP / FloorAdjustsCurve pair ApplyTDPSafely uses, so the *rule*
// cannot drift from the real path. The input can: live is the curve the caller
// intends to run — the CLI passes the assembled device's live curve — while the
// daemon applies the active profile's *stored* curve. The two agree whenever that
// profile is the one in force, which is the normal case, and a preview is allowed
// to be approximate where a real write is not.
//
// live is a parameter rather than a LiveCurve() call in here, mirroring
// ApplyTDPSafely's own want parameter, because reading hwmon made this function —
// and so dryrun_test.go, which is an external test package and cannot reach
// newFakeSysfs — depend on the developer's fan mode. That was not theoretical:
// only the len(live) == 0 branch below prints the floor curve's bottom PWM, and
// TestDryRunTdp_HighSustained asserts on it, so the test passed only while the
// machine happened to be on firmware auto and failed outright with a curve live.
//
// It previously claimed the fans would go to *full speed* (pwm_enable=0) whenever
// --force was given and any limit exceeded the safe max — three separate
// inaccuracies: the floor is a curve, not full speed; it is driven by the
// sustained limit alone; and it does not depend on --force. It then claimed the
// whole floor curve would always be written above the safe max, which stopped
// being true once the floor became a per-point minimum rather than a replacement
// curve.
func DryRunTdp(env driver.PowerEnvelope, watts, pl1, pl2, pl3 int, force bool, live []api.FanCurvePoint) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	s := TDPStateFor(watts, pl1, pl2, pl3)
	if force {
		fmt.Printf("--force given: sustained limit allowed above %dW (hardware max %dW)\n",
			env.TDPMaxSafe, env.TDPMaxForced)
	}
	if s.PL1SPL > env.TDPMaxSafe {
		curveDir := asusz13.FindFanCurveHwmonPath()
		if curveDir == "" {
			curveDir = "<hwmon not found>"
		}
		floorMin := 0
		if len(env.FloorCurve) > 0 {
			floorMin = env.FloorCurve[0].PWM
		}
		switch {
		case len(live) == 0:
			fmt.Printf("Would write the high-TDP fan curve (minimum %d PWM) to both fans in %s\n",
				floorMin, curveDir)
		case safety.FloorAdjustsCurve(env, s.PL1SPL, live):
			fmt.Println("Would raise the current fan curve's points below the device's high-TDP curve")
			fmt.Printf("  to it, leaving every other point as-is, and write it to both fans in %s\n", curveDir)
		default:
			fmt.Printf("Would write the current fan curve back to both fans in %s unchanged:\n", curveDir)
			fmt.Println("  it already clears the device's high-TDP curve at every temperature")
		}
		fmt.Printf("Would write 1 (custom) to %s/pwm{1,2}_enable\n", curveDir)
		fmt.Printf("  (sustained %dW is above the %dW safe max; if the fan write fails the TDP is not applied at all)\n",
			s.PL1SPL, env.TDPMaxSafe)
	}
	base := asusz13.FindPPTBasePath()
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
func DryRunTdpReset(env driver.PowerEnvelope) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Println("Would reset the CPU Curve Optimizer to stock (balanced is a stock profile)")
	fmt.Println("Would switch profile to balanced")
	stock := env.StockProfilePPT["balanced"]
	fmt.Printf("Would write stock PPT for balanced: PL1=%dW PL2=%dW PL3=%dW APU=%dW Platform=%dW\n",
		stock.PL1SPL, stock.PL2SPPT, stock.FPPT, stock.APUSPPT, stock.PlatformSPPT)
	fmt.Println("Would reset fan curves to auto mode (after the limit is lowered, not before)")
}

// DryRunTuningReset prints what clearing every tuning override would do.
//
// The order shown is the order it happens in, because the order is the safety
// property: power comes down before the fans are released, so the machine is
// never at a high sustained limit with no floor.
func DryRunTuningReset(env driver.PowerEnvelope) {
	fmt.Println("=== DRY RUN (no sysfs write) ===")
	fmt.Println("Would clear every tuning override: fan curve, power limits, Curve Optimizer")
	fmt.Println("Would reset the CPU Curve Optimizer to stock (only if an offset is applied)")
	fmt.Println("Would switch profile to balanced")
	stock := env.StockProfilePPT["balanced"]
	fmt.Printf("Would write stock PPT for balanced: PL1=%dW PL2=%dW PL3=%dW APU=%dW Platform=%dW\n",
		stock.PL1SPL, stock.PL2SPPT, stock.FPPT, stock.APUSPPT, stock.PlatformSPPT)
	fmt.Println("Would reset fan curves to auto mode (after the limit is lowered, not before)")
	fmt.Println("Would forget the saved fan curve, power limits and offset in the edited profile")
}

// DryRunUndervolt prints the SMU commands that would be sent for a Curve
// Optimizer change.
//
// An offset of 0 is not "no change": it encodes to the same argument as
// ResetCurveOptimizer, so "--set 0" clears any offset currently applied. Saying
// "no changes" here told users the opposite of what the command does.
func DryRunUndervolt(cpu int) {
	fmt.Println("=== DRY RUN (no SMU write) ===")
	encoded := asusz13.EncodeCOValue(cpu)
	if cpu == 0 {
		fmt.Printf("Would send MP1 cmd 0x4C with arg 0x%X (CPU CO 0 — clears any active undervolt)\n", encoded)
		return
	}
	fmt.Printf("Would send MP1 cmd 0x4C with arg 0x%X (CPU CO %d)\n", encoded, cpu)
}

// DryRunUndervoltReset prints the SMU commands that would be sent to reset CO.
func DryRunUndervoltReset() {
	fmt.Println("=== DRY RUN (no SMU write) ===")
	encoded := asusz13.EncodeCOValue(0)
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
