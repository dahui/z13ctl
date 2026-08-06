package cli_test

import (
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/cli"
)

// captureStdout redirects os.Stdout to a pipe, calls f, restores stdout,
// and returns all bytes written during f's execution.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = orig
	out, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestDryRunBatteryLimit(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunBatteryLimit(80) })

	for _, want := range []string{
		"DRY RUN",
		"charge_control_end_threshold",
		"80",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunBatteryLimit output missing %q", want)
		}
	}
}

func TestDryRunProfile(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunProfile("performance") })

	for _, want := range []string{
		"DRY RUN",
		"profile", // matches both /sys/class/platform-profile/.../profile and /sys/firmware/acpi/platform_profile
		"performance",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunProfile output missing %q", want)
		}
	}
}

// TestDryRunProfileCustomDoesNotClaimAPlatformProfileWrite is the regression
// guard for a long-standing lie: the dry run printed
// `Would write "custom" to /sys/.../profile` for a custom profile, which the
// daemon has never done — custom profiles deliberately leave platform_profile
// to the desktop.
func TestDryRunProfileCustomDoesNotClaimAPlatformProfileWrite(t *testing.T) {
	for _, name := range []string{"custom", "battery-uv"} {
		out := captureStdout(t, func() { cli.DryRunProfile(name) })
		if strings.Contains(out, "Would write") {
			t.Errorf("DryRunProfile(%q) claims a sysfs write:\n%s", name, out)
		}
		if !strings.Contains(out, name) {
			t.Errorf("DryRunProfile(%q) does not name the profile:\n%s", name, out)
		}
	}
}

func TestDryRunAutoswitch(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunAutoswitch(true, "balanced", "battery-uv") })
	for _, want := range []string{"DRY RUN", "balanced", "battery-uv"} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunAutoswitch output missing %q:\n%s", want, out)
		}
	}
	// Configuration only: it must not read as having changed the profile.
	if strings.Contains(out, "Would write") {
		t.Errorf("DryRunAutoswitch claims a sysfs write:\n%s", out)
	}

	off := captureStdout(t, func() { cli.DryRunAutoswitch(false, "balanced", "battery-uv") })
	if !strings.Contains(off, "disable") {
		t.Errorf("DryRunAutoswitch(false, ...) does not say it disables autoswitch:\n%s", off)
	}
}

func TestDryRunOff(t *testing.T) {
	out := captureStdout(t, cli.DryRunOff)

	for _, want := range []string{
		"DRY RUN",
		"Init 1",
		"Init 2",
		"Init 3",
		"Init 4",
		"Power OFF",
		"Brightness 0",
		"5DBD0100000000FF", // power-off bytes in hex (terminator 0xFF always present)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunOff output missing %q", want)
		}
	}
}

func TestDryRunBrightness_Zero(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunBrightness(0) })

	for _, want := range []string{
		"DRY RUN",
		"brightness (level 0)",
		"Power",
		"Brightness",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunBrightness(0) output missing %q", want)
		}
	}
	// Power OFF path: all power bytes zero
	if !strings.Contains(out, "5DBD0100000000") {
		t.Errorf("DryRunBrightness(0): power bytes should be zero")
	}
}

func TestDryRunBrightness_NonZero(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunBrightness(2) })

	for _, want := range []string{
		"DRY RUN",
		"brightness (level 2)",
		"Power",
		"Brightness",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunBrightness(2) output missing %q", want)
		}
	}
	// Power ON path: keyb=FF bar=1F lid=FF rear=FF
	if !strings.Contains(out, "5DBD01FF1FFFFF") {
		t.Errorf("DryRunBrightness(2): power bytes should be ON (FF 1F FF FF)")
	}
}

func TestDryRunApply_Static(t *testing.T) {
	out := captureStdout(t, func() {
		cli.DryRunApply(0xFF, 0x00, 0x00, 0, 0, 0, aura.ModeStatic, aura.SpeedNormal, 3)
	})

	for _, want := range []string{
		"DRY RUN",
		"Init 1",
		"Power ON",
		"Brightness",
		"SetMode z0",
		"SetMode z1",
		"MESSAGE_SET",
		"MESSAGE_APPLY",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunApply static output missing %q", want)
		}
	}
}

func TestDryRunApply_Breathe(t *testing.T) {
	// Breathe with non-zero primary color → randFlag = 0x01
	out := captureStdout(t, func() {
		cli.DryRunApply(0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, aura.ModeBreathe, aura.SpeedSlow, 2)
	})

	if !strings.Contains(out, "DRY RUN") {
		t.Error("DryRunApply breathe: missing DRY RUN header")
	}
	if !strings.Contains(out, "SetMode z0") {
		t.Error("DryRunApply breathe: missing SetMode z0")
	}
}

func TestDryRunFanCurve(t *testing.T) {
	points := []api.FanCurvePoint{
		{Temp: 48, PWM: 2}, {Temp: 53, PWM: 22}, {Temp: 57, PWM: 30}, {Temp: 60, PWM: 43},
		{Temp: 63, PWM: 56}, {Temp: 65, PWM: 68}, {Temp: 70, PWM: 89}, {Temp: 76, PWM: 102},
	}
	out := captureStdout(t, func() { cli.DryRunFanCurve(points) })

	for _, want := range []string{
		"DRY RUN",
		"pwm1_auto_point1_temp",
		"pwm1_auto_point8_pwm",
		"pwm2_auto_point1_temp",
		"pwm2_auto_point8_pwm",
		"48",
		"102",
		"custom",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunFanCurve output missing %q", want)
		}
	}
}

func TestDryRunFanCurveReset(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunFanCurveReset() })

	for _, want := range []string{
		"DRY RUN",
		"pwm1_enable",
		"pwm2_enable",
		"auto",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunFanCurveReset output missing %q", want)
		}
	}
}

func TestDryRunTdp(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunTdp(50, 0, 0, 0, false) })

	for _, want := range []string{
		"DRY RUN",
		"ppt_pl1_spl",
		"ppt_pl2_sppt",
		"ppt_fppt",
		"ppt_apu_sppt",
		"ppt_platform_sppt",
		"50",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunTdp output missing %q", want)
		}
	}
	if strings.Contains(out, "full speed") {
		t.Error("DryRunTdp(50W) should not mention full speed")
	}
}

// TestDryRunTdp_HighSustained pins the dry run against what ApplyTDPSafely
// actually does. The old expectation here was "full speed", which the real path
// has never done — it writes the 50% floor curve with pwm_enable=1. A dry run
// that describes an operation the tool does not perform is worse than none.
func TestDryRunTdp_HighSustained(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunTdp(80, 0, 0, 0, true) })

	// Reference the constant rather than the number: the floor has moved once
	// already (80% -> 50%) and these assertions should not need revisiting.
	floor := strconv.Itoa(cli.HighTDPMinPWM)
	for _, want := range []string{floor, "pwm", "not applied at all"} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunTdp(80W) output missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "full speed") {
		t.Error("DryRunTdp must not claim full speed — the real path writes the 50% floor curve")
	}
}

// TestDryRunTdp_FanFloorIgnoresForce: the floor depends on the sustained limit,
// not on --force. The old code only mentioned fans when --force was passed.
func TestDryRunTdp_FanFloorIgnoresForce(t *testing.T) {
	forced := captureStdout(t, func() { cli.DryRunTdp(80, 0, 0, 0, true) })
	unforced := captureStdout(t, func() { cli.DryRunTdp(80, 0, 0, 0, false) })

	floor := strconv.Itoa(cli.HighTDPMinPWM)
	if !strings.Contains(unforced, floor) {
		t.Errorf("DryRunTdp(80W, no force) omitted the fan floor; got:\n%s", unforced)
	}
	if strings.Contains(forced, floor) != strings.Contains(unforced, floor) {
		t.Error("the fan floor must not depend on --force")
	}
}

// TestDryRunTdp_BurstAboveSafeMaxKeepsFansAlone: a burst limit over the safe max
// does not trigger the floor — only the sustained limit does. The old condition
// fired on pl2/pl3 as well.
func TestDryRunTdp_BurstAboveSafeMaxKeepsFansAlone(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunTdp(50, 50, 90, 90, true) })

	if strings.Contains(out, strconv.Itoa(cli.HighTDPMinPWM)) || strings.Contains(out, "fan") {
		t.Errorf("burst limits above the safe max must not imply a fan change; got:\n%s", out)
	}
}

func TestDryRunTdp_PLOverrides(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunTdp(50, 45, 55, 60, false) })

	if !strings.Contains(out, "45") {
		t.Error("DryRunTdp PL overrides: missing pl1=45")
	}
	if !strings.Contains(out, "55") {
		t.Error("DryRunTdp PL overrides: missing pl2=55")
	}
	if !strings.Contains(out, "60") {
		t.Error("DryRunTdp PL overrides: missing pl3=60")
	}
}

func TestDryRunTdpReset(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunTdpReset() })

	for _, want := range []string{
		"DRY RUN",
		"fan curves",
		"balanced",
		"Curve Optimizer", // a stock profile clears the undervolt
		"stock PPT",       // z13ctl writes these; the firmware does not
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunTdpReset output missing %q; got:\n%s", want, out)
		}
	}
	// The firmware does not re-apply per-profile PPT — assuming it did is the
	// whole of issue #12, and this text used to assert it.
	if strings.Contains(out, "firmware sets per-profile PPT") {
		t.Error("DryRunTdpReset still claims the firmware restores PPT")
	}
	// Power comes down before the fans are released.
	if strings.Index(out, "stock PPT") > strings.Index(out, "fan curves to auto") {
		t.Error("dry run shows fans released before the limit is lowered")
	}
}

// TestDryRunUndervoltZeroIsNotANoOp: an offset of 0 encodes identically to a
// reset, so "--set 0" clears an active undervolt. The dry run used to say "No
// changes", which is the opposite of what the command does.
func TestDryRunUndervoltZeroIsNotANoOp(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunUndervolt(0) })

	if strings.Contains(out, "No changes") {
		t.Error("DryRunUndervolt(0) claims no changes, but the command clears any active undervolt")
	}
	if !strings.Contains(out, "0x4C") {
		t.Errorf("DryRunUndervolt(0) should show the SMU command it sends; got:\n%s", out)
	}
}

func TestDryRunApply_ZeroColor_RandomFlag(t *testing.T) {
	// Zero primary color → randFlag = 0xFF (random color mode)
	out := captureStdout(t, func() {
		cli.DryRunApply(0, 0, 0, 0, 0, 0, aura.ModeCycle, aura.SpeedFast, 1)
	})

	if !strings.Contains(out, "DRY RUN") {
		t.Error("DryRunApply zero-color: missing DRY RUN header")
	}
	// randFlag 0xFF should appear in the SetMode packet hex
	if !strings.Contains(out, "FF") {
		t.Error("DryRunApply zero-color: expected FF (randFlag) in output")
	}
}
