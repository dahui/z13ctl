package cli_test

import (
	"io"
	"os"
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

func TestDryRunTdp_Force(t *testing.T) {
	out := captureStdout(t, func() { cli.DryRunTdp(80, 0, 0, 0, true) })

	if !strings.Contains(out, "full speed") {
		t.Error("DryRunTdp(80W, force) should mention full speed")
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
		"ppt_pl1_spl",
		"ppt_fppt",
		"5", // firmware default
	} {
		if !strings.Contains(out, want) {
			t.Errorf("DryRunTdpReset output missing %q", want)
		}
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
