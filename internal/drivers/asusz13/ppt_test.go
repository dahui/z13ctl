package asusz13

// ppt_test.go — the PPT interface choice (asus-armoury over the deprecated
// asus-nb-wmi), the envelope it reports, and what is read and written on each.

import (
	"os"
	"slices"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"
)

func (f *fakeSysfs) armouryState(t *testing.T) api.TDPState {
	t.Helper()
	return api.TDPState{
		PL1SPL:  f.readInt(t, f.firmware+"/ppt_pl1_spl/current_value"),
		PL2SPPT: f.readInt(t, f.firmware+"/ppt_pl2_sppt/current_value"),
		FPPT:    f.readInt(t, f.firmware+"/ppt_pl3_fppt/current_value"),
	}
}

func (f *fakeSysfs) assertLegacyUntouched(t *testing.T) {
	t.Helper()
	for _, l := range pptLimits {
		if got := f.readInt(t, f.ppt+"/"+l.legacy); got != 5 {
			t.Errorf("asus-nb-wmi %s = %d, want the untouched 5: armoury is present, so the "+
				"deprecated interface must not be written", l.legacy, got)
		}
	}
}

func TestPPTInterfaceSelection(t *testing.T) {
	base := z13Env(t)
	lim := NewPowerLimiter(base)

	t.Run("asus-nb-wmi alone keeps the device data", func(t *testing.T) {
		newFakeSysfs(t)
		env := lim.Envelope()
		if env.Interface != PPTInterfaceLegacy {
			t.Fatalf("Interface = %q, want %q", env.Interface, PPTInterfaceLegacy)
		}
		if env.TDPMin != base.TDPMin || env.TDPMaxForced != base.TDPMaxForced || !env.PL2.IsZero() || env.NoSPPTMirrors {
			t.Errorf("envelope = %+v, want the device file's ranges untouched", env)
		}
		if env.Initial != (api.TDPState{PL1SPL: base.TDPMin}) {
			t.Errorf("Initial = %+v, want PL1 at TDPMin (the 5 W boot cache)", env.Initial)
		}
	})
	t.Run("armoury preferred, with the kernel's ranges", func(t *testing.T) {
		f := newFakeSysfs(t)
		f.withArmouryPPT(t)
		env := lim.Envelope()
		want := base
		want.Interface = PPTInterfaceArmoury
		want.TDPMin, want.TDPMaxForced = 28, 80
		want.PL2 = driver.PowerRange{Min: 32, Max: 92}
		want.PL3 = driver.PowerRange{Min: 45, Max: 93}
		want.NoSPPTMirrors = true
		want.Initial = api.TDPState{PL1SPL: 60, PL2SPPT: 75, FPPT: 86}
		if env.Interface != want.Interface || env.TDPMin != want.TDPMin || env.TDPMaxForced != want.TDPMaxForced ||
			env.PL2 != want.PL2 || env.PL3 != want.PL3 || env.NoSPPTMirrors != want.NoSPPTMirrors ||
			env.Initial != want.Initial {
			t.Errorf("envelope = %+v, want %+v", env, want)
		}
		if env.TDPMaxSafe != base.TDPMaxSafe || len(env.StockProfilePPT) != len(base.StockProfilePPT) ||
			len(env.FloorCurve) != len(base.FloorCurve) {
			t.Error("the device data's safe maximum, stock rows and floor must survive the overlay")
		}
	})
	t.Run("armoury without readable bounds falls back", func(t *testing.T) {
		// A kernel that registers the attributes with no calibration data for
		// this model has nothing the fallback lacks.
		f := newFakeSysfs(t)
		f.withArmouryPPT(t)
		if err := os.Remove(f.firmware + "/ppt_pl1_spl/min_value"); err != nil {
			t.Fatal(err)
		}
		if got := lim.Envelope().Interface; got != PPTInterfaceLegacy {
			t.Errorf("Interface = %q, want the %s fallback", got, PPTInterfaceLegacy)
		}
	})
	t.Run("neither", func(t *testing.T) {
		f := newFakeSysfs(t)
		swap(t, &pptBasePath, f.root+"/no-such-device")
		if env := lim.Envelope(); env.Interface != "" || env.TDPMin != base.TDPMin {
			t.Errorf("envelope with no interface = %+v, want the device data alone", env)
		}
		if err := SetTDPState(base.StockProfilePPT["balanced"]); err == nil {
			t.Error("SetTDPState() with no interface = nil error, want one")
		}
	})
}

// TestSetTDPStateUsesArmouryOnly: with armoury present, every write goes there
// and the deprecated files are neither written nor needed. APU and Platform
// sPPT, which armoury does not expose on the GZ302EA, are skipped.
func TestSetTDPStateUsesArmouryOnly(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)

	if err := SetTDPState(api.TDPState{PL1SPL: 40, PL2SPPT: 55, FPPT: 60, APUSPPT: 70, PlatformSPPT: 70}); err != nil {
		t.Fatalf("SetTDPState() = %v", err)
	}
	if got, want := f.armouryState(t), (api.TDPState{PL1SPL: 40, PL2SPPT: 55, FPPT: 60}); got != want {
		t.Errorf("armoury = %+v, want %+v", got, want)
	}
	f.assertLegacyUntouched(t)

	got, err := ReadAllPPT()
	if err != nil {
		t.Fatalf("ReadAllPPT() = %v", err)
	}
	if want := (api.TDPState{PL1SPL: 40, PL2SPPT: 55, FPPT: 60}); got != want {
		t.Errorf("ReadAllPPT() = %+v, want %+v with APU/Platform at 0 (not exposed)", got, want)
	}
}

// TestSetTDPStateClampsIntoArmourysRange: a profile saved under asus-nb-wmi's 5 W
// floor still applies — at armoury's minimum — instead of failing with -EINVAL,
// and safety.EffectiveTDP over the reported envelope predicts exactly what was
// written, which is what the reconcile watcher compares against.
func TestSetTDPStateClampsIntoArmourysRange(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)
	env := NewPowerLimiter(z13Env(t)).Envelope()

	for _, tc := range []struct {
		in, want api.TDPState
	}{
		{api.TDPState{PL1SPL: 15, PL2SPPT: 15, FPPT: 15, APUSPPT: 15, PlatformSPPT: 15}, api.TDPState{PL1SPL: 28, PL2SPPT: 32, FPPT: 45}},
		{api.TDPState{PL1SPL: 93, PL2SPPT: 93, FPPT: 93}, api.TDPState{PL1SPL: 80, PL2SPPT: 92, FPPT: 93}},
	} {
		if got := safety.EffectiveTDP(env, tc.in); got != tc.want {
			t.Errorf("EffectiveTDP(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
		if err := SetTDPState(tc.in); err != nil {
			t.Fatalf("SetTDPState(%+v) = %v", tc.in, err)
		}
		if got := f.armouryState(t); got != tc.want {
			t.Errorf("SetTDPState(%+v) wrote %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

// TestApplyTDPSafelyOnArmoury drives a request end to end the way the daemon
// does: resolved against the reported envelope, then applied by the engine.
func TestApplyTDPSafelyOnArmoury(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)
	eng, _ := z13Engine(t)

	s, notes, err := safety.ResolveTDP(eng.Envelope(), 30, 0, 0, 0, false)
	if err != nil || len(notes) != 2 {
		t.Fatalf("ResolveTDP(30) = %+v, %q, %v; want PL2 and PL3 raised", s, notes, err)
	}
	if err := eng.ApplyTDPSafely(s, nil); err != nil {
		t.Fatalf("ApplyTDPSafely = %v", err)
	}
	if got, want := f.armouryState(t), (api.TDPState{PL1SPL: 30, PL2SPPT: 32, FPPT: 45}); got != want {
		t.Errorf("armoury = %+v, want %+v", got, want)
	}
	f.assertLegacyUntouched(t)

	if _, _, err := safety.ResolveTDP(eng.Envelope(), 81, 0, 0, 0, true); err == nil {
		t.Error("ResolveTDP(81, force) against armoury's 80 W maximum = nil error, want a refusal")
	}
}

// TestPPTBoundsAreReadAtCallTime: armoury answers from its battery table on
// battery, so the bounds must not be cached from an earlier read.
func TestPPTBoundsAreReadAtCallTime(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)
	lim := NewPowerLimiter(z13Env(t))
	if got := lim.Envelope().TDPMaxForced; got != 80 {
		t.Fatalf("PL1 max = %d, want 80", got)
	}
	f.writeFile(t, f.firmware+"/ppt_pl1_spl/max_value", "65")
	if got := lim.Envelope().TDPMaxForced; got != 65 {
		t.Errorf("PL1 max after the table changed = %d, want 65", got)
	}
}

// TestReadEffectiveOnArmoury: armoury's cache starts at default_value, the
// counterpart of asus-nb-wmi's 5 W, so an untouched cache on a stock profile
// reports that profile's row; a written value is reported as written, armoury's
// minimum included; and a custom profile never substitutes.
func TestReadEffectiveOnArmoury(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)
	eng, env := z13Engine(t)

	got, err := eng.ReadEffective("balanced")
	if err != nil {
		t.Fatalf("ReadEffective = %v", err)
	}
	if want := env.StockProfilePPT["balanced"]; got != want {
		t.Errorf("untouched cache on balanced = %+v, want the stock row %+v", got, want)
	}
	if got, _ := eng.ReadEffective("custom"); got.PL1SPL != 60 {
		t.Errorf("untouched cache on custom = %+v, want the raw cache (PL1 60)", got)
	}
	if err := SetTDPState(api.TDPState{PL1SPL: 28, PL2SPPT: 32, FPPT: 45}); err != nil {
		t.Fatal(err)
	}
	if got, _ := eng.ReadEffective("balanced"); got.PL1SPL != 28 {
		t.Errorf("armoury's minimum on balanced = %+v, want what was written (PL1 28), not the stock row", got)
	}
}

// TestPlanTDPWritesNamesArmourysFiles: the dry run is handed this plan, so it
// must name the files SetTDPState would actually write, clamped.
func TestPlanTDPWritesNamesArmourysFiles(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)

	plan := PlanTDPWrites(api.TDPState{PL1SPL: 30, PL2SPPT: 30, FPPT: 30, APUSPPT: 30, PlatformSPPT: 30})
	want := []PPTWrite{
		{Path: f.firmware + "/ppt_pl1_spl/current_value", Watts: 30},
		{Path: f.firmware + "/ppt_pl2_sppt/current_value", Watts: 32},
		{Path: f.firmware + "/ppt_pl3_fppt/current_value", Watts: 45},
	}
	if !slices.Equal(plan, want) {
		t.Errorf("plan = %+v, want %+v", plan, want)
	}
}

// TestHandBackToFirmwareOnArmoury: the stock row lands on armoury, as armoury
// holds it, before the fans are released; and a row that cannot be written with
// a high limit still read back keeps the fan floor.
func TestHandBackToFirmwareOnArmoury(t *testing.T) {
	t.Run("writes the row, then releases", func(t *testing.T) {
		f := newFakeSysfs(t)
		f.withArmouryPPT(t)
		f.seedFanCurveFiles(t, 50, 127)
		f.writeFile(t, f.hwmon+"/pwm1_enable", "1") // the floor is on
		f.writeFile(t, f.hwmon+"/pwm2_enable", "1")
		eng, env := z13Engine(t)
		if err := SetTDPState(api.TDPState{PL1SPL: 78, PL2SPPT: 78, FPPT: 78}); err != nil {
			t.Fatal(err)
		}
		if err := eng.HandBackToFirmware("balanced"); err != nil {
			t.Fatalf("HandBackToFirmware = %v", err)
		}
		row := env.StockProfilePPT["balanced"]
		if got, want := f.armouryState(t), (api.TDPState{PL1SPL: row.PL1SPL, PL2SPPT: row.PL2SPPT, FPPT: row.FPPT}); got != want {
			t.Errorf("armoury = %+v, want the row %+v", got, want)
		}
		if got := f.readInt(t, f.hwmon+"/pwm1_enable"); got != 2 {
			t.Errorf("pwm1_enable = %d, want 2 (released)", got)
		}
	})
}

// TestUnwritableArmouryFallsBack: an install that has not re-run setup can read
// armoury's PPT attributes but not write them, while it still holds the grant
// on asus-nb-wmi's. Choosing armoury there would turn every TDP write into
// EACCES on upgrade; the driver keeps using the interface it can write. (The
// fail-closed hand-back for a row that genuinely fails is the engine's, and is
// covered by TestHandBackToFirmwareKeepsTheFloorWhenTheRowFails.)
func TestUnwritableArmouryFallsBack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only file, which is what this test relies on")
	}
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)
	if err := os.Chmod(f.firmware+"/ppt_pl1_spl/current_value", 0o444); err != nil {
		t.Fatal(err)
	}
	if got := NewPowerLimiter(z13Env(t)).Envelope().Interface; got != PPTInterfaceLegacy {
		t.Fatalf("Interface with armoury read-only = %q, want %q", got, PPTInterfaceLegacy)
	}
	if err := SetTDPState(tdpAll(40)); err != nil {
		t.Fatalf("SetTDPState = %v, want it written through asus-nb-wmi", err)
	}
	if got := f.readInt(t, f.ppt+"/ppt_pl1_spl"); got != 40 {
		t.Errorf("asus-nb-wmi ppt_pl1_spl = %d, want 40", got)
	}
	if got := f.armouryState(t); got != (api.TDPState{PL1SPL: 60, PL2SPPT: 75, FPPT: 86}) {
		t.Errorf("armoury = %+v, want its untouched defaults", got)
	}
}

// TestProfileControllerNamesThePolicyAttributes: both notifying attributes, so
// the daemon sees fan releases (throttle_thermal_policy) as well as profile
// writes.
func TestProfileControllerNamesThePolicyAttributes(t *testing.T) {
	newFakeSysfs(t)
	n, ok := NewProfileController(nil).(driver.PolicyWriteNotifier)
	if !ok {
		t.Fatal("the platform-profile driver does not implement driver.PolicyWriteNotifier")
	}
	paths := n.PolicyWritePaths()
	want := []string{sysProfileACPI, pptBasePath + "/throttle_thermal_policy"}
	if !slices.Equal(paths, want) {
		t.Errorf("PolicyWritePaths() = %q, want %q", paths, want)
	}
}
