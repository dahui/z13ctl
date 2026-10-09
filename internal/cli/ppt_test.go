package cli

// ppt_test.go — the PPT interface choice (asus-armoury over the deprecated
// asus-nb-wmi), the kernel's bounds, and what is read and written on each.

import (
	"os"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
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
		if got := f.readInt(t, f.ppt+"/"+l.legacy); got != TDPMin {
			t.Errorf("asus-nb-wmi %s = %d, want the untouched %d: armoury is present, so the "+
				"deprecated interface must not be written", l.legacy, got, TDPMin)
		}
	}
}

func TestPPTBackendSelection(t *testing.T) {
	t.Run("asus-nb-wmi alone", func(t *testing.T) {
		newFakeSysfs(t)
		if lim, err := PPTLimits(); err != nil || lim.Backend != PPTBackendLegacy {
			t.Errorf("PPTLimits() = %+v, %v; want the %s fallback", lim, err, PPTBackendLegacy)
		}
	})
	t.Run("armoury preferred when both exist", func(t *testing.T) {
		f := newFakeSysfs(t)
		f.withArmouryPPT(t)
		lim, err := PPTLimits()
		if err != nil || lim.Backend != PPTBackendArmoury {
			t.Fatalf("PPTLimits() = %+v, %v; want %s", lim, err, PPTBackendArmoury)
		}
		want := api.TDPLimits{Backend: PPTBackendArmoury, PL1: api.TDPRange{Min: 28, Max: 80},
			PL2: api.TDPRange{Min: 32, Max: 92}, PL3: api.TDPRange{Min: 45, Max: 93}, SafeMax: TDPMaxSafe}
		if lim != want {
			t.Errorf("PPTLimits() = %+v, want the files' bounds %+v", lim, want)
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
		if lim, _ := PPTLimits(); lim.Backend != PPTBackendLegacy {
			t.Errorf("backend = %q, want the %s fallback", lim.Backend, PPTBackendLegacy)
		}
	})
	t.Run("neither", func(t *testing.T) {
		f := newFakeSysfs(t)
		swap(t, &pptBasePath, f.root+"/no-such-device")
		if _, err := PPTLimits(); err == nil {
			t.Error("PPTLimits() with no interface = nil error, want one")
		}
		if err := SetTDPState(StockProfilePPT["balanced"]); err == nil {
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
// and EffectiveTDP predicts exactly what was written, which is what the
// reconcile watcher compares against.
func TestSetTDPStateClampsIntoArmourysRange(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)

	for _, tc := range []struct {
		in, want api.TDPState
	}{
		{api.TDPState{PL1SPL: 15, PL2SPPT: 15, FPPT: 15}, api.TDPState{PL1SPL: 28, PL2SPPT: 32, FPPT: 45}},
		{api.TDPState{PL1SPL: 93, PL2SPPT: 93, FPPT: 93}, api.TDPState{PL1SPL: 80, PL2SPPT: 92, FPPT: 93}},
	} {
		if got := EffectiveTDP(tc.in); got != tc.want {
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

func TestResolveTDPOnArmoury(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)

	t.Run("a low unified value raises PL2 and PL3 to their minimums", func(t *testing.T) {
		s, notes, err := ResolveTDP(30, 0, 0, 0, false)
		if err != nil {
			t.Fatalf("ResolveTDP(30) = %v", err)
		}
		want := api.TDPState{PL1SPL: 30, PL2SPPT: 32, FPPT: 45, APUSPPT: 32, PlatformSPPT: 32}
		if s != want {
			t.Errorf("ResolveTDP(30) = %+v, want %+v", s, want)
		}
		if len(notes) != 2 || !strings.Contains(notes[0], "PL2 raised from 30W to 32W") ||
			!strings.Contains(notes[1], "PL3 raised from 30W to 45W") {
			t.Errorf("notes = %q, want one each for PL2 and PL3", notes)
		}
	})
	t.Run("an in-range request is untouched", func(t *testing.T) {
		s, notes, err := ResolveTDP(60, 0, 0, 0, false)
		if err != nil || len(notes) != 0 || s.PL1SPL != 60 || s.PL2SPPT != 60 || s.FPPT != 60 {
			t.Errorf("ResolveTDP(60) = %+v, %q, %v; want 60/60/60 with no notes", s, notes, err)
		}
	})
	for _, tc := range []struct {
		name                 string
		watts, pl1, pl2, pl3 int
		force                bool
		mention              string
	}{
		{"PL1 below the kernel's minimum", 20, 0, 0, 0, false, "28–75"},
		{"PL1 above the kernel's maximum even with force", 85, 0, 0, 0, true, "28–80"},
		{"PL1 above the safe maximum without force", 76, 0, 0, 0, false, "--force"},
		{"PL2 above the kernel's maximum", 50, 0, 93, 0, false, "PL2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ResolveTDP(tc.watts, tc.pl1, tc.pl2, tc.pl3, tc.force)
			if err == nil {
				t.Fatal("ResolveTDP = nil error, want a refusal")
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not mention %q", err, tc.mention)
			}
		})
	}
}

// TestResolveTDPOnLegacyKeepsItsRange: the fallback keeps the range it always
// had, so a kernel without armoury PPT behaves as before.
func TestResolveTDPOnLegacyKeepsItsRange(t *testing.T) {
	newFakeSysfs(t)
	s, notes, err := ResolveTDP(TDPMin, 0, 0, 0, false)
	if err != nil || len(notes) != 0 || s.PL1SPL != TDPMin || s.FPPT != TDPMin {
		t.Errorf("ResolveTDP(%d) on asus-nb-wmi = %+v, %q, %v; want it accepted as-is", TDPMin, s, notes, err)
	}
	if _, _, err := ResolveTDP(TDPMaxForced, 0, 0, 0, true); err != nil {
		t.Errorf("ResolveTDP(%d, force) on asus-nb-wmi = %v, want accepted", TDPMaxForced, err)
	}
}

// TestPPTBoundsAreReadAtCallTime: armoury answers from its battery table on
// battery, so the bounds must not be cached from an earlier read.
func TestPPTBoundsAreReadAtCallTime(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)
	if lim, _ := PPTLimits(); lim.PL1.Max != 80 {
		t.Fatalf("PL1 max = %d, want 80", lim.PL1.Max)
	}
	f.writeFile(t, f.firmware+"/ppt_pl1_spl/max_value", "65")
	if lim, _ := PPTLimits(); lim.PL1.Max != 65 {
		t.Errorf("PL1 max after the table changed = %d, want 65", lim.PL1.Max)
	}
	if _, _, err := ResolveTDP(70, 0, 0, 0, false); err == nil {
		t.Error("ResolveTDP(70) against a 65 W maximum = nil error, want a refusal")
	}
}

// TestReadEffectivePPTOnArmoury: armoury's cache starts at default_value, the
// counterpart of asus-nb-wmi's 5 W, so an untouched cache on a stock profile
// reports that profile's row as armoury holds it (APU/Platform not exposed); a
// written value is reported as written; and a custom profile never substitutes.
func TestReadEffectivePPTOnArmoury(t *testing.T) {
	f := newFakeSysfs(t)
	f.withArmouryPPT(t)

	got, err := ReadEffectivePPT("balanced")
	if err != nil {
		t.Fatalf("ReadEffectivePPT = %v", err)
	}
	if want := (api.TDPState{PL1SPL: 52, PL2SPPT: 71, FPPT: 70}); got != want {
		t.Errorf("untouched cache on balanced = %+v, want the row as armoury holds it %+v", got, want)
	}
	if got, _ := ReadEffectivePPT("custom"); got.PL1SPL != 60 {
		t.Errorf("untouched cache on custom = %+v, want the raw cache (PL1 60)", got)
	}

	if err := SetTDPState(api.TDPState{PL1SPL: 30, PL2SPPT: 32, FPPT: 45}); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadEffectivePPT("balanced"); got.PL1SPL != 30 {
		t.Errorf("written cache on balanced = %+v, want what was written (PL1 30)", got)
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
	if len(plan) != len(want) {
		t.Fatalf("plan = %+v, want %+v", plan, want)
	}
	for i := range want {
		if plan[i] != want[i] {
			t.Errorf("plan[%d] = %+v, want %+v", i, plan[i], want[i])
		}
	}
}

// TestHandBackToFirmwareOnArmoury repeats the two load-bearing hand-back cases
// on armoury: it ends on the firmware's limits, and a row that cannot be
// written with a high limit still read back keeps the fan floor.
func TestHandBackToFirmwareOnArmoury(t *testing.T) {
	t.Run("ends on the firmware's limits", func(t *testing.T) {
		f, _ := releaseFixture(t)
		f.withArmouryPPT(t)
		if err := SetTDPState(api.TDPState{PL1SPL: 78, PL2SPPT: 78, FPPT: 78}); err != nil {
			t.Fatal(err)
		}
		if err := HandBackToFirmware("balanced"); err != nil {
			t.Fatalf("HandBackToFirmware = %v", err)
		}
		fw := fakeFirmwarePPT["balanced"]
		if got, want := f.armouryState(t), (api.TDPState{PL1SPL: fw.PL1SPL, PL2SPPT: fw.PL2SPPT, FPPT: fw.FPPT}); got != want {
			t.Errorf("armoury = %+v, want the firmware's own %+v", got, want)
		}
		f.assertFanModes(t, 2)
	})
	t.Run("keeps the floor when the row fails", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores the read-only mode this test relies on")
		}
		f, _ := releaseFixture(t)
		f.withArmouryPPT(t)
		if err := SetTDPState(api.TDPState{PL1SPL: 78, PL2SPPT: 78, FPPT: 78}); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f.firmware+"/ppt_pl1_spl/current_value", 0o444); err != nil {
			t.Fatal(err)
		}
		if err := HandBackToFirmware("balanced"); err == nil {
			t.Fatal("HandBackToFirmware with the limit stuck high = nil, want a refusal")
		}
		f.assertFanModes(t, 1)
	})
}
