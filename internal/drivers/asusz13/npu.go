package asusz13

// npu.go — AMD Ryzen AI NPU (XDNA) telemetry via the amdxdna DRM accel
// driver: power, average per-column utilisation, and the MP-NPU clock,
// through DRM_AMDXDNA_GET_INFO ioctls on /dev/accel/accelN.
//
// Ioctl numbers and struct layouts follow z13ctl-plus
// (github.com/aic0d3r/z13ctl-plus, Apache-2.0), which derived them from the
// mainline amdxdna driver. The payloads are decoded with explicit offsets
// rather than struct casts: the sensor record is 168 bytes, so records after
// the first sit at unaligned offsets.
//
// # The runtime-PM guard is load-bearing
//
// Opening the accel node resumes a runtime-suspended NPU, and this reader is
// called by a 1 Hz sampler — querying unconditionally would pin the NPU awake
// forever, a power cost imposed by the graph that measures power. The device
// is only opened while runtime_status reads "active" (something else already
// has it awake). A suspended NPU reports zeros with Known=true, deliberately:
// suspended genuinely means drawing nothing, so 0 W is a reading, and a chart
// that gapped whenever the NPU slept would be empty on every machine not
// running inference — indistinguishable from a broken sensor.

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	// _IOWR('d', 0x40+7, struct amdxdna_drm_get_info): dir=3 (RW),
	// magic='d'=0x64, nr=0x47, size=16 → 0xC0106447 on x86_64.
	amdxdnaIoctlGetInfo = 0xC0106447

	drmAmdxdnaQueryClockMetadata = 3 // enum amdxdna_drm_get_param
	drmAmdxdnaQuerySensors       = 4

	amdxdnaSensorPower             = 0 // enum amdxdna_sensor_type
	amdxdnaSensorColumnUtilization = 1

	// struct amdxdna_drm_query_sensor is 168 bytes: label[64], input u32,
	// max u32, average u32, highest u32, status[64], units[16], unitm s8,
	// type u8, pad[6].
	amdxdnaSensorSize     = 168
	amdxdnaSensorInputOff = 64
	amdxdnaSensorUnitsOff = 144
	amdxdnaSensorUnitmOff = 160
	amdxdnaSensorTypeOff  = 161
	// struct amdxdna_drm_query_clock_metadata is 48 bytes: two 24-byte
	// clocks (name[16], freq_mhz u32, pad u32); the first is mp_npu_clock.
	amdxdnaClockMDSize  = 48
	amdxdnaClockFreqOff = 16
)

// npuQueryFn is the ioctl transaction, a var so tests can substitute decoded
// payloads without a device.
var npuQueryFn = npuQueryIoctl

// findNPUDevicePath returns the /dev/accel/accelN node for the AMD XDNA NPU,
// or "" when none is bound. Uncached, like every discovery in this package.
func findNPUDevicePath() string {
	entries, err := os.ReadDir(sysAccelDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "accel") {
			continue
		}
		vendor, err := os.ReadFile(sysAccelDir + "/" + name + "/device/vendor")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(vendor)) != "0x1022" {
			continue
		}
		return devAccelDir + "/" + name
	}
	return ""
}

// ReadNPURuntimeActive reports whether the NPU is awake; exported for the
// daemon's declaration guard, which must tell "no amdxdna device on this
// machine" (skip) from "declared with no reader" (failure).
func ReadNPURuntimeActive() (bool, error) { return npuRuntimeActive() }

// npuRuntimeActive reports whether the NPU is awake, read from sysfs so the
// question itself cannot wake it.
func npuRuntimeActive() (bool, error) {
	dev := findNPUDevicePath()
	if dev == "" {
		return false, fmt.Errorf("amdxdna NPU not found")
	}
	name := strings.TrimPrefix(dev, devAccelDir+"/")
	status, err := os.ReadFile(sysAccelDir + "/" + name + "/device/power/runtime_status")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(status)) == "active", nil
}

// amdxdnaDrmGetInfo mirrors struct amdxdna_drm_get_info (16 bytes). buffer is
// an unsafe.Pointer rather than a uint64 so the GC keeps the backing slice
// alive across the syscall.
type amdxdnaDrmGetInfo struct {
	param      uint32
	bufferSize uint32
	buffer     unsafe.Pointer
}

// npuQueryIoctl issues DRM_AMDXDNA_GET_INFO for the given param and returns
// the driver's response, trimmed to the size it wrote back. One retry when the
// driver signals ERANGE with the size it needs.
func npuQueryIoctl(param uint32) ([]byte, error) {
	dev := findNPUDevicePath()
	if dev == "" {
		return nil, fmt.Errorf("amdxdna NPU not found")
	}
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	size := 16 * amdxdnaSensorSize
	for range 2 {
		buf := make([]byte, size)
		req := amdxdnaDrmGetInfo{
			param:      param,
			bufferSize: uint32(size), //nolint:gosec // bounded small constant
			buffer:     unsafe.Pointer(&buf[0]),
		}
		_, _, errno := syscall.Syscall(
			syscall.SYS_IOCTL, f.Fd(),
			uintptr(amdxdnaIoctlGetInfo),
			uintptr(unsafe.Pointer(&req)),
		)
		if errno != 0 {
			if errno == syscall.ERANGE && int(req.bufferSize) > size {
				size = int(req.bufferSize)
				continue
			}
			return nil, fmt.Errorf("amdxdna ioctl %d: %w", param, errno)
		}
		if int(req.bufferSize) < len(buf) {
			buf = buf[:req.bufferSize]
		}
		return buf, nil
	}
	return nil, fmt.Errorf("amdxdna ioctl %d: buffer retry exhausted", param)
}

// decodeNPUSensors extracts power (W) and average column utilisation (%) from
// a sensor-query payload. Power arrives in mW or W depending on the record's
// unit fields.
func decodeNPUSensors(payload []byte) (powerW float64, utilPct int) {
	var utilSum, utilCount int
	for off := 0; off+amdxdnaSensorSize <= len(payload); off += amdxdnaSensorSize {
		input := binary.LittleEndian.Uint32(payload[off+amdxdnaSensorInputOff:])
		switch payload[off+amdxdnaSensorTypeOff] {
		case amdxdnaSensorPower:
			v := float64(input)
			units := strings.ToLower(strings.TrimRight(
				string(payload[off+amdxdnaSensorUnitsOff:off+amdxdnaSensorUnitmOff]), "\x00 "))
			if int8(payload[off+amdxdnaSensorUnitmOff]) < 0 || strings.HasPrefix(units, "m") {
				v /= 1000.0
			}
			powerW = v
		case amdxdnaSensorColumnUtilization:
			utilSum += int(input)
			utilCount++
		}
	}
	if utilCount > 0 {
		utilPct = utilSum / utilCount
	}
	return powerW, utilPct
}

// decodeNPUClockMHz extracts the live MP-NPU clock from a clock-metadata
// payload.
func decodeNPUClockMHz(payload []byte) (int, error) {
	if len(payload) < amdxdnaClockMDSize {
		return 0, fmt.Errorf("amdxdna clock metadata: short response (%d)", len(payload))
	}
	return int(binary.LittleEndian.Uint32(payload[amdxdnaClockFreqOff:])), nil
}

// ReadNPUTelemetry returns the NPU's power, utilisation and clock.
//
// known is true when something real can be said: either the device is awake
// and answered, or it is runtime-suspended — real zeros, per the package
// comment. It is false when no NPU exists, its state cannot be read, or the
// driver's query fails; those are gaps, not readings.
func ReadNPUTelemetry() (powerW float64, utilPct, clockMHz int, known bool) {
	active, err := npuRuntimeActive()
	if err != nil {
		return 0, 0, 0, false
	}
	if !active {
		return 0, 0, 0, true
	}
	payload, err := npuQueryFn(drmAmdxdnaQuerySensors)
	if err != nil {
		return 0, 0, 0, false
	}
	powerW, utilPct = decodeNPUSensors(payload)
	// Best-effort: a clock the driver cannot answer must not drop the power
	// reading that just succeeded.
	if md, err := npuQueryFn(drmAmdxdnaQueryClockMetadata); err == nil {
		if mhz, err := decodeNPUClockMHz(md); err == nil {
			clockMHz = mhz
		}
	}
	return powerW, utilPct, clockMHz, true
}
