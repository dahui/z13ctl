package amd

import (
	"encoding/binary"
	"testing"
)

func TestDecodeNPUSensors(t *testing.T) {
	// Power in mW plus two utilisation columns to average.
	payload := append(npuSensorRecord(amdxdnaSensorPower, 1850, "mW", 0),
		append(npuSensorRecord(amdxdnaSensorColumnUtilization, 30, "%", 0),
			npuSensorRecord(amdxdnaSensorColumnUtilization, 10, "%", 0)...)...)

	powerW, util := decodeNPUSensors(payload)
	if powerW != 1.85 {
		t.Errorf("powerW = %v, want 1.85 (mW scaled)", powerW)
	}
	if util != 20 {
		t.Errorf("util = %d, want 20 (average of 30 and 10)", util)
	}

	// Watts-denominated power passes through unscaled.
	powerW, _ = decodeNPUSensors(npuSensorRecord(amdxdnaSensorPower, 2, "W", 0))
	if powerW != 2 {
		t.Errorf("powerW = %v, want 2", powerW)
	}
}

func TestDecodeNPUClockMHz(t *testing.T) {
	md := make([]byte, amdxdnaClockMDSize)
	copy(md, "MP-NPU Clock\x00")
	binary.LittleEndian.PutUint32(md[amdxdnaClockFreqOff:], 1267)
	got, err := decodeNPUClockMHz(md)
	if err != nil || got != 1267 {
		t.Errorf("got %v, %v; want 1267", got, err)
	}
	if _, err := decodeNPUClockMHz([]byte{1, 2, 3}); err == nil {
		t.Error("short clock metadata accepted")
	}
}

// npuSensorRecord builds one 168-byte amdxdna sensor record.
func npuSensorRecord(typ byte, input uint32, units string, unitm int8) []byte {
	rec := make([]byte, amdxdnaSensorSize)
	binary.LittleEndian.PutUint32(rec[amdxdnaSensorInputOff:], input)
	copy(rec[amdxdnaSensorUnitsOff:], units)
	rec[amdxdnaSensorUnitmOff] = byte(unitm)
	rec[amdxdnaSensorTypeOff] = typ
	return rec
}
