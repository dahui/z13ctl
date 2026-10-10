package asusz13

// pmtable_test.go — the pm_table parser against synthetic tables. The
// captured fixtures and the checks that need real numbers are in
// pmtable_fixture_test.go.

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

const z13PMVersion = 0x0064020C

// synthPMTable builds a table of size bytes with the given float32s at the
// given indices.
func synthPMTable(size int, vals map[int]float32) []byte {
	b := make([]byte, size)
	for i, v := range vals {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}

func TestParsePMTableReadsTheHead(t *testing.T) {
	raw := synthPMTable(0xE50, map[int]float32{0: 52, 1: 51.5, 2: 71, 3: 60.25, 4: 70, 5: 49})
	r, err := ParsePMTable(z13PMVersion, raw)
	if err != nil {
		t.Fatal(err)
	}
	want := map[PMField]float64{
		PMSTAPMLimit: 52, PMSTAPMValue: 51.5, PMFastLimit: 71,
		PMFastValue: 60.25, PMSlowLimit: 70, PMSlowValue: 49,
	}
	for f, w := range want {
		if got, ok := r.Values[f]; !ok || got != w {
			t.Errorf("%s = %v (present %v), want %v", f, got, ok, w)
		}
	}
	if r.Version != z13PMVersion {
		t.Errorf("Version = 0x%X", r.Version)
	}
}

// A version not in the table is refused rather than read at someone else's
// offsets — a BIOS update that moves the layout must report nothing.
func TestParsePMTableRefusesUnknownVersions(t *testing.T) {
	raw := synthPMTable(0xE50, map[int]float32{0: 52})
	for _, v := range []uint32{0x0064010C, 0x0064020D, 0} {
		if _, err := ParsePMTable(v, raw); err == nil || !strings.Contains(err.Error(), "no known layout") {
			t.Errorf("version 0x%X: err = %v, want a refusal", v, err)
		}
	}
}

func TestParsePMTableRefusesShortBuffers(t *testing.T) {
	if _, err := ParsePMTable(z13PMVersion, make([]byte, 20)); err == nil {
		t.Error("20 bytes: want an error (the layout reads index 5)")
	}
	if _, err := ParsePMTable(z13PMVersion, make([]byte, 24)); err != nil {
		t.Errorf("24 bytes: %v, want it to cover index 5", err)
	}
}

// A garbage float drops that field alone: the others in the same table stand,
// and nothing implausible reaches a caller as a wattage.
func TestParsePMTableDropsImplausibleFieldsAlone(t *testing.T) {
	raw := synthPMTable(0xE50, map[int]float32{
		0: float32(math.NaN()), 1: float32(math.Inf(1)), 2: -3, 3: 1e9, 4: 70, 5: 0,
	})
	r, err := ParsePMTable(z13PMVersion, raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []PMField{PMSTAPMLimit, PMSTAPMValue, PMFastLimit, PMFastValue} {
		if v, ok := r.Values[f]; ok {
			t.Errorf("%s = %v, want it dropped", f, v)
		}
	}
	if r.Values[PMSlowLimit] != 70 {
		t.Errorf("slow_limit = %v, want 70", r.Values[PMSlowLimit])
	}
	// Zero is a reading (an idle rail), not garbage.
	if v, ok := r.Values[PMSlowValue]; !ok || v != 0 {
		t.Errorf("slow_value = %v (present %v), want 0 present", v, ok)
	}
}

func TestParsePMTableVersion(t *testing.T) {
	v, err := ParsePMTableVersion([]byte{0x0C, 0x02, 0x64, 0x00})
	if err != nil || v != z13PMVersion {
		t.Errorf("ParsePMTableVersion = 0x%X, %v", v, err)
	}
	if _, err := ParsePMTableVersion([]byte{1, 2}); err == nil {
		t.Error("2 bytes: want an error")
	}
}
