package asusz13

// pmtable_fixture_test.go — the parser against tables captured on a GZ302EA
// under sustained load (testdata/pmtable-0x64020C-*.bin), each with a sidecar
// recording what was written and what RAPL measured. Idle captures are
// useless for this — the limit registers read firmware defaults whatever was
// written — so every fixture here was taken with 32 workers running.

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

type pmSidecar struct {
	Condition string `json:"condition"`
	Read      string `json:"read"`
	Written   struct {
		PL1, PL2, PL3 float64
	} `json:"written_w"`
	RAPL float64 `json:"rapl_package_w"`
}

func loadPMFixture(t *testing.T, name string) (PMReading, pmSidecar) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".bin")
	if err != nil {
		t.Fatal(err)
	}
	var side pmSidecar
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if jerr := json.Unmarshal(data, &side); jerr != nil {
		t.Fatal(jerr)
	}
	r, err := ParsePMTable(z13PMVersion, raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(raw) != 0xE50 {
		t.Errorf("%s is %d bytes, want pm_table_size 0xE50", name, len(raw))
	}
	return r, side
}

var freshPMFixtures = []string{
	"pmtable-0x64020C-stock-balanced",
	"pmtable-0x64020C-tdp-30",
	"pmtable-0x64020C-tdp-50",
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// The names are earned here, not assumed: under load the three readings are
// the package power RAPL measured, and the slow limit is the sustained limit
// RAPL settled on — the one that binds. A layout that put the names on the
// wrong indices would fail this on real data rather than on synthetic bytes.
func TestPMTableAgreesWithRAPL(t *testing.T) {
	for _, name := range freshPMFixtures {
		r, side := loadPMFixture(t, name)
		for _, f := range []PMField{PMSTAPMValue, PMFastValue, PMSlowValue} {
			if v := r.Values[f]; !near(v, side.RAPL, 1) {
				t.Errorf("%s: %s = %.2f W, RAPL measured %.2f W", name, f, v, side.RAPL)
			}
		}
		if v := r.Values[PMSlowLimit]; !near(v, side.RAPL, 1) {
			t.Errorf("%s: slow limit %.2f W, but RAPL settled at %.2f W", name, v, side.RAPL)
		}
	}
}

// What we wrote reaches the table. At 50/50/50 every limit is the written
// value (to float32 rounding: the SMU holds 50.0000038). At 30/32/45 the fast
// limit is the written fPPT and the slow limit is 32 — armoury's practical
// floor, the same 32 W z13ctl measured holding under load — not the 30
// written for PL1.
func TestPMTableAgreesWithWhatWeWrote(t *testing.T) {
	r, side := loadPMFixture(t, "pmtable-0x64020C-tdp-50")
	for _, f := range []PMField{PMSTAPMLimit, PMFastLimit, PMSlowLimit} {
		if !near(r.Values[f], side.Written.PL1, 0.01) {
			t.Errorf("tdp-50: %s = %v, want the written %v", f, r.Values[f], side.Written.PL1)
		}
	}
	r, side = loadPMFixture(t, "pmtable-0x64020C-tdp-30")
	if !near(r.Values[PMFastLimit], side.Written.PL3, 0.01) {
		t.Errorf("tdp-30: fast limit = %v, want the written fPPT %v", r.Values[PMFastLimit], side.Written.PL3)
	}
	if !near(r.Values[PMSlowLimit], 32, 0.01) {
		t.Errorf("tdp-30: slow limit = %v, want 32 (the floor above the written %v)", r.Values[PMSlowLimit], side.Written.PL1)
	}
}

// The first read of a capture disagreed with RAPL and with the read five
// seconds later, while every later read agreed: the driver copies the table
// before the SMU has finished refreshing it, so a read reflects the previous
// request. Pinned so that a sampler, if one is ever written, starts from the
// fact that its first reading is stale and must be thrown away.
func TestPMTableFirstReadIsStale(t *testing.T) {
	stale, side := loadPMFixture(t, "pmtable-0x64020C-stock-balanced-stale")
	if side.Read != "stale-first-read" {
		t.Fatalf("sidecar read = %q", side.Read)
	}
	if v := stale.Values[PMSlowValue]; near(v, side.RAPL, 5) {
		t.Errorf("stale read's slow value %.2f W is close to RAPL %.2f W; the fixture no longer shows the lag", v, side.RAPL)
	}
}
