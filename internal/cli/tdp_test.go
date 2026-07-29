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

// TestSetTDPStateWritesStockValuesVerbatim is the regression guard for issue #14:
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
