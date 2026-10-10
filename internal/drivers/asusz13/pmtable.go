package asusz13

// pmtable.go — the ryzen_smu power-management table, parsed.
//
// The table is the SMU's own view of its limits and the values it is holding
// them against: float32s at offsets that depend on the table version, which
// changes with the platform and with AGESA. This file is the parser only, and
// pure — it takes the bytes and the version and reads at explicit offsets, the
// way the gpu_metrics parser does.
//
// Nothing in voltaire reads the table yet, and that is deliberate (Jeff,
// 2026-10-09). A read of /sys/kernel/ryzen_smu_drv/pm_table is not passive: the
// driver sends TransferTableSmu2Dram over the RSMU mailbox on any read more
// than 1 ms after the last (ryzen_smu smu.c, smu_read_pm_table). A sampler
// would put a userspace SMU message on the bus every few seconds for as long
// as the daemon runs — the class of traffic behind the 2026-08-14 hard lock —
// for three readings RAPL and the PPT attributes largely give already. The
// parser is kept for the device that needs it (the OXP reads the same table,
// M5) and so that a sampler, if one is ever justified, starts from a tested
// decoder rather than a guessed one.
//
// Such a sampler must also throw its first reading away. Captured under load,
// the first read disagreed with RAPL and with a read five seconds later
// (readings 19.5/77.2/15.4 W against 52 W measured) while every later read
// agreed within 0.1 W: the driver copies the table before the SMU has finished
// refreshing it, so a read reflects the request before it
// (TestPMTableFirstReadIsStale).

import (
	"encoding/binary"
	"fmt"
	"math"
)

// PMField names one quantity in the table.
type PMField int

// The fields with names. Each is a (limit, value) pair at the head of the
// table — the standard Zen layout — in watts. Only these are named: the table
// carries far more (per-domain temperatures, per-core clocks, current limits),
// but a field is added here only once a capture has shown what it is, never
// from its position alone.
const (
	PMSTAPMLimit PMField = iota // the STAPM (skin-temperature-aware) limit
	PMSTAPMValue                // the STAPM moving average it is held against
	PMFastLimit                 // the fast PPT limit
	PMFastValue                 // the fast PPT reading
	PMSlowLimit                 // the slow (sustained) PPT limit
	PMSlowValue                 // the slow PPT reading
)

func (f PMField) String() string {
	switch f {
	case PMSTAPMLimit:
		return "stapm_limit"
	case PMSTAPMValue:
		return "stapm_value"
	case PMFastLimit:
		return "fast_limit"
	case PMFastValue:
		return "fast_value"
	case PMSlowLimit:
		return "slow_limit"
	case PMSlowValue:
		return "slow_value"
	}
	return fmt.Sprintf("pmfield(%d)", int(f))
}

// pmLayouts maps a table version to each named field's float32 index.
//
// 0x64020C is the Z13's (Strix Halo, AGESA at time of writing). The head
// offsets are the ones the OneXPlayer work established for 0x64010C, which
// differs only in the middle byte and has the same 0xE50 size; they were
// confirmed here against captures taken under sustained load (testdata/), since
// idle captures read firmware defaults whatever was written. A version not
// listed is refused: a BIOS update that moves the table must report nothing,
// not misread it.
var pmLayouts = map[uint32]map[PMField]int{
	0x0064020C: {
		PMSTAPMLimit: 0, PMSTAPMValue: 1,
		PMFastLimit: 2, PMFastValue: 3,
		PMSlowLimit: 4, PMSlowValue: 5,
	},
}

// pmMaxPlausibleW bounds a limit or reading. The table is read from DRAM the
// SMU wrote, and a NaN, an infinity or a garbage float there must not reach a
// caller as a wattage. A field outside it is dropped alone; the rest stand.
const pmMaxPlausibleW = 500

// PMReading is one parsed table. A field missing from Values is not carried by
// this version's layout or did not hold a plausible number — never zero.
type PMReading struct {
	Version uint32
	Values  map[PMField]float64
}

// ParsePMTable decodes raw, the contents of pm_table, as version (the
// little-endian u32 in pm_table_version). It refuses an unknown version and a
// buffer too short for the layout; a field holding an implausible value is
// left out of the reading rather than failing the whole table.
func ParsePMTable(version uint32, raw []byte) (PMReading, error) {
	layout, ok := pmLayouts[version]
	if !ok {
		return PMReading{}, fmt.Errorf("pm_table version 0x%06X has no known layout", version)
	}
	need := 0
	for _, idx := range layout {
		need = max(need, (idx+1)*4)
	}
	if len(raw) < need {
		return PMReading{}, fmt.Errorf("pm_table is %d bytes, layout 0x%06X needs %d", len(raw), version, need)
	}
	r := PMReading{Version: version, Values: make(map[PMField]float64, len(layout))}
	for f, idx := range layout {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[idx*4:])))
		if math.IsNaN(v) || v < 0 || v > pmMaxPlausibleW {
			continue
		}
		r.Values[f] = v
	}
	return r, nil
}

// ParsePMTableVersion decodes pm_table_version: a little-endian u32.
func ParsePMTableVersion(raw []byte) (uint32, error) {
	if len(raw) < 4 {
		return 0, fmt.Errorf("pm_table_version is %d bytes, want 4", len(raw))
	}
	return binary.LittleEndian.Uint32(raw), nil
}
