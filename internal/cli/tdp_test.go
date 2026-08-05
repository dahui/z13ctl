package cli

// tdp_test.go — PPT write-path tests. These use a temp directory in place of
// the real sysfs node, so no hardware is required.

import (
	"os"
	"testing"

	"github.com/dahui/z13ctl/api"
)

// usePPTTempDir redirects PPT sysfs access to a temp directory for the duration
// of the test and returns its path.
func usePPTTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := pptBasePath
	pptBasePath = dir
	t.Cleanup(func() { pptBasePath = orig })
	return dir
}

// TestSetTDPStateWritesStockValuesVerbatim is the regression guard for issue #12:
// switching back to a stock profile must write that profile's measured values
// exactly. It fails if anyone reintroduces PL2 mirroring into APU/Platform sPPT
// or drops one of the five attributes.
func TestSetTDPStateWritesStockValuesVerbatim(t *testing.T) {
	for profile, stock := range StockProfilePPT {
		t.Run(profile, func(t *testing.T) {
			usePPTTempDir(t)

			if err := SetTDPState(stock); err != nil {
				t.Fatalf("SetTDPState(%s) = %v, want nil", profile, err)
			}
			got, err := ReadAllPPT()
			if err != nil {
				t.Fatalf("ReadAllPPT() = %v, want nil", err)
			}
			if got != stock {
				t.Errorf("round-trip for %s = %+v, want %+v", profile, got, stock)
			}
		})
	}
}

func TestSetTDPStateWritesEveryAttribute(t *testing.T) {
	dir := usePPTTempDir(t)

	s := api.TDPState{PL1SPL: 11, PL2SPPT: 22, FPPT: 33, APUSPPT: 44, PlatformSPPT: 55}
	if err := SetTDPState(s); err != nil {
		t.Fatalf("SetTDPState() = %v, want nil", err)
	}

	want := map[string]int{
		"ppt_pl1_spl":       11,
		"ppt_pl2_sppt":      22,
		"ppt_fppt":          33,
		"ppt_apu_sppt":      44,
		"ppt_platform_sppt": 55,
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() = %v, want nil", err)
	}
	if len(entries) != len(want) {
		t.Errorf("wrote %d files, want %d", len(entries), len(want))
	}
	for attr, wantWatts := range want {
		got, err := ReadPPT(attr)
		if err != nil {
			t.Errorf("ReadPPT(%s) = %v, want nil", attr, err)
			continue
		}
		if got != wantWatts {
			t.Errorf("%s = %d, want %d", attr, got, wantWatts)
		}
	}
}

// TestSetTDPMirrorsPL2 pins SetTDP's documented behaviour after the refactor
// that made it delegate to SetTDPState.
func TestSetTDPMirrorsPL2(t *testing.T) {
	tests := []struct {
		name                 string
		watts, pl1, pl2, pl3 int
		want                 api.TDPState
	}{
		{
			name:  "unified watts fills all limits",
			watts: 45,
			want:  api.TDPState{PL1SPL: 45, PL2SPPT: 45, FPPT: 45, APUSPPT: 45, PlatformSPPT: 45},
		},
		{
			name:  "non-zero overrides replace watts",
			watts: 45, pl1: 40, pl2: 60, pl3: 70,
			want: api.TDPState{PL1SPL: 40, PL2SPPT: 60, FPPT: 70, APUSPPT: 60, PlatformSPPT: 60},
		},
		{
			name:  "zero override falls back to watts",
			watts: 50, pl2: 65,
			want: api.TDPState{PL1SPL: 50, PL2SPPT: 65, FPPT: 50, APUSPPT: 65, PlatformSPPT: 65},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usePPTTempDir(t)

			if err := SetTDP(tt.watts, tt.pl1, tt.pl2, tt.pl3); err != nil {
				t.Fatalf("SetTDP() = %v, want nil", err)
			}
			got, err := ReadAllPPT()
			if err != nil {
				t.Fatalf("ReadAllPPT() = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("SetTDP() wrote %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestStockProfilePPTSanity guards the table itself: every stock profile must be
// present and every value within the safety envelope, since these are now
// written to hardware rather than only displayed.
func TestStockProfilePPTSanity(t *testing.T) {
	for _, profile := range []string{"quiet", "balanced", "performance"} {
		stock, ok := StockProfilePPT[profile]
		if !ok {
			t.Errorf("StockProfilePPT is missing %q", profile)
			continue
		}
		for _, f := range []struct {
			name  string
			watts int
		}{
			{"PL1SPL", stock.PL1SPL},
			{"PL2SPPT", stock.PL2SPPT},
			{"FPPT", stock.FPPT},
			{"APUSPPT", stock.APUSPPT},
			{"PlatformSPPT", stock.PlatformSPPT},
		} {
			if f.watts < TDPMin || f.watts > TDPMaxForced {
				t.Errorf("%s.%s = %dW, out of range %d–%d", profile, f.name, f.watts, TDPMin, TDPMaxForced)
			}
		}
		if stock.PL2SPPT < stock.PL1SPL {
			t.Errorf("%s: PL2 %dW is below PL1 %dW; burst must not be under sustained",
				profile, stock.PL2SPPT, stock.PL1SPL)
		}
	}
}

// TestApplyTDPSafely is the regression guard for the thermal-floor cluster: the
// four paths that apply a custom TDP each enforced the 50% fan floor
// differently, and one of them raised power before raising the fans and threw
// the fan error away. They now all go through ApplyTDPSafely, which must fail
// closed.
func TestApplyTDPSafely(t *testing.T) {
	high := api.TDPState{PL1SPL: 90, PL2SPPT: 90, FPPT: 90, APUSPPT: 90, PlatformSPPT: 90}
	safe := api.TDPState{PL1SPL: TDPMaxSafe, PL2SPPT: 80, FPPT: 85, APUSPPT: 80, PlatformSPPT: 80}

	t.Run("above safe max raises fans then applies TDP", func(t *testing.T) {
		f := newFakeSysfs(t)
		f.seedFanCurveFiles(t, 40, 50)

		if err := ApplyTDPSafely(high); err != nil {
			t.Fatalf("ApplyTDPSafely() = %v, want nil", err)
		}

		got, err := ReadAllPPT()
		if err != nil {
			t.Fatalf("ReadAllPPT() = %v, want nil", err)
		}
		if got != high {
			t.Errorf("PPT = %+v, want %+v", got, high)
		}
		curves, err := ReadBothFanCurves()
		if err != nil {
			t.Fatalf("ReadBothFanCurves() = %v, want nil", err)
		}
		for fan, points := range curves {
			for i, p := range points {
				if p.PWM < HighTDPMinPWM {
					t.Errorf("fan%d point %d PWM = %d, want >= %d", fan+1, i+1, p.PWM, HighTDPMinPWM)
				}
			}
		}
		modes, err := ReadBothFanModes()
		if err != nil {
			t.Fatalf("ReadBothFanModes() = %v, want nil", err)
		}
		for fan, m := range modes {
			if m != 1 {
				t.Errorf("fan%d pwm_enable = %d, want 1 (custom)", fan+1, m)
			}
		}
	})

	t.Run("refuses the TDP when the fan write fails", func(t *testing.T) {
		f := newFakeSysfs(t)
		f.seedFanCurveFiles(t, 40, 50)
		// Establish a known baseline, then break fan discovery so the high-TDP
		// curve cannot be written.
		baseline := StockProfilePPT["balanced"]
		if err := SetTDPState(baseline); err != nil {
			t.Fatalf("SetTDPState(baseline) = %v, want nil", err)
		}
		swap(t, &sysHwmonDir, t.TempDir())

		err := ApplyTDPSafely(high)
		if err == nil {
			t.Fatal("ApplyTDPSafely() = nil, want an error when the fan curve cannot be written")
		}

		got, readErr := ReadAllPPT()
		if readErr != nil {
			t.Fatalf("ReadAllPPT() = %v, want nil", readErr)
		}
		if got != baseline {
			t.Errorf("PPT = %+v, want the untouched baseline %+v — an unsafe TDP was applied without the fan floor", got, baseline)
		}
	})

	// The other half of failing closed: the write is accepted but the kernel
	// does not keep the mode, which is what a concurrent platform_profile write
	// produces. Before the readback in SetBothFanCurves this looked like success
	// and the machine ran above the safe sustained max with no floor at all.
	t.Run("refuses the TDP when the fan curve does not stick", func(t *testing.T) {
		f := newFakeSysfs(t)
		f.seedFanCurveFiles(t, 40, 50) // pwm_enable = 2 (auto)
		baseline := StockProfilePPT["balanced"]
		if err := SetTDPState(baseline); err != nil {
			t.Fatalf("SetTDPState(baseline) = %v, want nil", err)
		}
		orig := fanWriteInt
		fanWriteInt = func(string, int) error { return nil } // accepted, no effect
		t.Cleanup(func() { fanWriteInt = orig })

		if err := ApplyTDPSafely(high); err == nil {
			t.Fatal("ApplyTDPSafely() = nil, want an error when the kernel drops the fan curve")
		}

		got, readErr := ReadAllPPT()
		if readErr != nil {
			t.Fatalf("ReadAllPPT() = %v, want nil", readErr)
		}
		if got != baseline {
			t.Errorf("PPT = %+v, want the untouched baseline %+v — an unsafe TDP was applied without the fan floor", got, baseline)
		}
	})

	t.Run("at the safe max leaves fans alone", func(t *testing.T) {
		f := newFakeSysfs(t)
		f.seedFanCurveFiles(t, 40, 50)

		if err := ApplyTDPSafely(safe); err != nil {
			t.Fatalf("ApplyTDPSafely() = %v, want nil", err)
		}
		got, err := ReadAllPPT()
		if err != nil {
			t.Fatalf("ReadAllPPT() = %v, want nil", err)
		}
		if got != safe {
			t.Errorf("PPT = %+v, want %+v", got, safe)
		}
		modes, err := ReadBothFanModes()
		if err != nil {
			t.Fatalf("ReadBothFanModes() = %v, want nil", err)
		}
		for fan, m := range modes {
			if m != 2 {
				t.Errorf("fan%d pwm_enable = %d, want 2 (untouched auto) at exactly the safe max", fan+1, m)
			}
		}
	})
}

func TestCheckFanCurveFloor(t *testing.T) {
	lowCurve := make([]api.FanCurvePoint, fanCurvePoints)
	for i := range lowCurve {
		lowCurve[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: 100}
	}

	t.Run("rejects a low curve above the safe max", func(t *testing.T) {
		newFakeSysfs(t)
		if err := SetTDPState(api.TDPState{PL1SPL: 90, PL2SPPT: 90, FPPT: 90, APUSPPT: 90, PlatformSPPT: 90}); err != nil {
			t.Fatalf("SetTDPState() = %v, want nil", err)
		}
		if err := CheckFanCurveFloor("custom", lowCurve); err == nil {
			t.Error("CheckFanCurveFloor() = nil, want an error for a 100 PWM curve at 90W sustained")
		}
	})

	t.Run("accepts the high-TDP curve above the safe max", func(t *testing.T) {
		newFakeSysfs(t)
		if err := SetTDPState(api.TDPState{PL1SPL: 90, PL2SPPT: 90, FPPT: 90, APUSPPT: 90, PlatformSPPT: 90}); err != nil {
			t.Fatalf("SetTDPState() = %v, want nil", err)
		}
		if err := CheckFanCurveFloor("custom", HighTDPFanCurve()); err != nil {
			t.Errorf("CheckFanCurveFloor(HighTDPFanCurve()) = %v, want nil", err)
		}
	})

	t.Run("accepts a low curve at a safe TDP", func(t *testing.T) {
		newFakeSysfs(t)
		if err := SetTDP(45, 0, 0, 0); err != nil {
			t.Fatalf("SetTDP() = %v, want nil", err)
		}
		if err := CheckFanCurveFloor("custom", lowCurve); err != nil {
			t.Errorf("CheckFanCurveFloor() = %v, want nil at 45W", err)
		}
	})

	t.Run("unreadable PPT does not block fan control", func(t *testing.T) {
		newFakeSysfs(t) // no ppt_* files written
		if err := CheckFanCurveFloor("custom", lowCurve); err != nil {
			t.Errorf("CheckFanCurveFloor() = %v, want nil when PPT cannot be read", err)
		}
	})
}

func TestCheckFanFloorRelease(t *testing.T) {
	t.Run("refuses above the safe max", func(t *testing.T) {
		newFakeSysfs(t)
		if err := SetTDPState(api.TDPState{PL1SPL: 90, PL2SPPT: 90, FPPT: 90, APUSPPT: 90, PlatformSPPT: 90}); err != nil {
			t.Fatalf("SetTDPState() = %v, want nil", err)
		}
		if err := CheckFanFloorRelease("custom"); err == nil {
			t.Error("CheckFanFloorRelease() = nil, want a refusal at 90W sustained")
		}
	})

	t.Run("allows at the safe max", func(t *testing.T) {
		newFakeSysfs(t)
		if err := SetTDP(TDPMaxSafe, 0, 0, 0); err != nil {
			t.Fatalf("SetTDP() = %v, want nil", err)
		}
		if err := CheckFanFloorRelease("custom"); err != nil {
			t.Errorf("CheckFanFloorRelease() = %v, want nil at exactly %dW", err, TDPMaxSafe)
		}
	})

	t.Run("stock profile with a stale cache is allowed", func(t *testing.T) {
		newFakeSysfs(t)
		// PL1 == TDPMin is the kernel's boot cache; the balanced fallback is 52W.
		if err := SetTDP(TDPMin, 0, 0, 0); err != nil {
			t.Fatalf("SetTDP() = %v, want nil", err)
		}
		if err := CheckFanFloorRelease("balanced"); err != nil {
			t.Errorf("CheckFanFloorRelease(balanced) = %v, want nil", err)
		}
	})
}

func TestReadEffectivePPT(t *testing.T) {
	quiet := StockProfilePPT["quiet"]

	t.Run("stale cache falls back to stock table", func(t *testing.T) {
		usePPTTempDir(t)
		// TDPMin (5W) is the value the kernel caches on module load.
		if err := SetTDP(TDPMin, 0, 0, 0); err != nil {
			t.Fatalf("SetTDP() = %v, want nil", err)
		}
		got, err := ReadEffectivePPT("quiet")
		if err != nil {
			t.Fatalf("ReadEffectivePPT() = %v, want nil", err)
		}
		if got != quiet {
			t.Errorf("ReadEffectivePPT(quiet) = %+v, want stock %+v", got, quiet)
		}
	})

	t.Run("real values are returned as-is", func(t *testing.T) {
		usePPTTempDir(t)
		if err := SetTDP(15, 0, 0, 0); err != nil {
			t.Fatalf("SetTDP() = %v, want nil", err)
		}
		got, err := ReadEffectivePPT("quiet")
		if err != nil {
			t.Fatalf("ReadEffectivePPT() = %v, want nil", err)
		}
		if got.PL1SPL != 15 {
			t.Errorf("PL1SPL = %d, want 15 (sysfs value, not the stock table)", got.PL1SPL)
		}
	})

	t.Run("unknown profile keeps stale values", func(t *testing.T) {
		usePPTTempDir(t)
		if err := SetTDP(TDPMin, 0, 0, 0); err != nil {
			t.Fatalf("SetTDP() = %v, want nil", err)
		}
		got, err := ReadEffectivePPT("custom")
		if err != nil {
			t.Fatalf("ReadEffectivePPT() = %v, want nil", err)
		}
		if got.PL1SPL != TDPMin {
			t.Errorf("PL1SPL = %d, want %d for an unknown profile", got.PL1SPL, TDPMin)
		}
	})
}
