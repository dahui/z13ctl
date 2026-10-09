package safety

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// armouryEnvelope is the GZ302EA as asusz13 reports it on asus-armoury: the
// kernel's per-limit ranges over the device file's safe maximum.
func armouryEnvelope() driver.PowerEnvelope {
	return driver.PowerEnvelope{
		TDPMin: 28, TDPMaxSafe: 75, TDPMaxForced: 80,
		Interface:     "asus-armoury",
		PL2:           driver.PowerRange{Min: 32, Max: 92},
		PL3:           driver.PowerRange{Min: 45, Max: 93},
		NoSPPTMirrors: true,
		Initial:       api.TDPState{PL1SPL: 60, PL2SPPT: 75, FPPT: 86},
	}
}

// legacyEnvelope is device data alone: one range for every limit.
func legacyEnvelope() driver.PowerEnvelope {
	return driver.PowerEnvelope{TDPMin: 5, TDPMaxSafe: 75, TDPMaxForced: 93, Interface: "asus-nb-wmi"}
}

func TestResolveTDPRaisesBurstLimitsToTheirMinimum(t *testing.T) {
	got, notes, err := ResolveTDP(armouryEnvelope(), 30, 0, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	want := api.TDPState{PL1SPL: 30, PL2SPPT: 32, FPPT: 45, APUSPPT: 32, PlatformSPPT: 32}
	if got != want {
		t.Errorf("ResolveTDP(30) = %+v, want %+v", got, want)
	}
	if len(notes) != 2 || !strings.Contains(notes[0], "PL2 raised from 30W to 32W") ||
		!strings.Contains(notes[1], "PL3 raised from 30W to 45W") {
		t.Errorf("notes = %q, want one per raised limit", notes)
	}
}

func TestResolveTDPRefusals(t *testing.T) {
	tests := []struct {
		name                string
		env                 driver.PowerEnvelope
		watts, pl1, pl2, p3 int
		force               bool
		want                string
	}{
		{"PL1 below armoury's minimum", armouryEnvelope(), 27, 0, 0, 0, false, "PL1 27W out of range 28–75W (asus-armoury)"},
		{"PL1 above safe without force", armouryEnvelope(), 76, 0, 0, 0, false, "use force to allow up to 80W"},
		{"PL1 above armoury's maximum with force", armouryEnvelope(), 81, 0, 0, 0, true, "PL1 81W out of range 28–80W"},
		{"PL2 above its maximum", armouryEnvelope(), 50, 0, 93, 0, false, "PL2 93W out of range 32–92W"},
		{"PL3 above its maximum", armouryEnvelope(), 50, 0, 0, 94, false, "PL3 94W out of range 45–93W"},
		{"PL1 below device data's minimum", legacyEnvelope(), 4, 0, 0, 0, false, "PL1 4W out of range 5–75W (asus-nb-wmi)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ResolveTDP(tt.env, tt.watts, tt.pl1, tt.pl2, tt.p3, tt.force)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestResolveTDPKeepsDeviceDataRangeWithoutKernelBounds(t *testing.T) {
	got, notes, err := ResolveTDP(legacyEnvelope(), 15, 0, 0, 0, false)
	if err != nil || len(notes) != 0 {
		t.Fatalf("ResolveTDP(15) err = %v, notes = %q; want 15W accepted as-is", err, notes)
	}
	if got != (api.TDPState{PL1SPL: 15, PL2SPPT: 15, FPPT: 15, APUSPPT: 15, PlatformSPPT: 15}) {
		t.Errorf("ResolveTDP(15) = %+v", got)
	}
}

func TestEffectiveTDPClampsAndDropsAbsentMirrors(t *testing.T) {
	stored := api.TDPState{PL1SPL: 15, PL2SPPT: 15, FPPT: 95, APUSPPT: 15, PlatformSPPT: 15}
	got := EffectiveTDP(armouryEnvelope(), stored)
	want := api.TDPState{PL1SPL: 28, PL2SPPT: 32, FPPT: 93}
	if got != want {
		t.Errorf("EffectiveTDP on armoury = %+v, want %+v", got, want)
	}
	if got := EffectiveTDP(legacyEnvelope(), stored); got != (api.TDPState{PL1SPL: 15, PL2SPPT: 15, FPPT: 93, APUSPPT: 15, PlatformSPPT: 15}) {
		t.Errorf("EffectiveTDP on device data = %+v, want only PL3 clamped", got)
	}
}

func TestCacheStale(t *testing.T) {
	arm := armouryEnvelope()
	if !CacheStale(arm, api.TDPState{PL1SPL: 60, PL2SPPT: 75, FPPT: 86}) {
		t.Error("armoury at its defaults is the untouched cache")
	}
	// A custom 60 W limit is not the defaults: PL2/PL3 moved with it.
	if CacheStale(arm, api.TDPState{PL1SPL: 60, PL2SPPT: 60, FPPT: 60}) {
		t.Error("a written limit sharing PL1 with the defaults was reported stale")
	}
	// armoury's minimum is a legitimate value, not a boot cache.
	if CacheStale(arm, api.TDPState{PL1SPL: 28, PL2SPPT: 32, FPPT: 45}) {
		t.Error("armoury's minimum was reported stale")
	}
	leg := legacyEnvelope()
	if !CacheStale(leg, api.TDPState{PL1SPL: 5, PL2SPPT: 5, FPPT: 5}) {
		t.Error("with no Initial, PL1 at TDPMin is the boot cache")
	}
	if CacheStale(leg, api.TDPState{PL1SPL: 30}) {
		t.Error("a written limit was reported stale")
	}
}
