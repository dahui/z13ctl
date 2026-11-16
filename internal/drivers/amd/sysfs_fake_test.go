package amd

// sysfs_fake_test.go — a temporary directory tree standing in for the sysfs
// and procfs nodes this package reads, so the AMD helpers can be exercised
// without the hardware: k10temp, the ryzen_smu mailbox, the CPU's identity,
// and the DRM and accel roots the GPU and NPU are found under.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeSysfs is a temp-dir tree with the package path vars pointed at it.
type fakeSysfs struct {
	root      string
	hwmonTemp string // k10temp device
	smu       string
}

// newFakeSysfs builds the tree and redirects every path var for the test's
// lifetime, restoring the originals on cleanup.
func newFakeSysfs(t *testing.T) *fakeSysfs {
	t.Helper()
	root := t.TempDir()
	f := &fakeSysfs{
		root:      root,
		hwmonTemp: root + "/hwmon/hwmon2",
		smu:       root + "/ryzen_smu_drv",
	}
	for _, d := range []string{f.hwmonTemp, f.smu} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", d, err)
		}
	}
	f.writeFile(t, f.hwmonTemp+"/name", "k10temp")

	swap(t, &sysHwmonDir, root+"/hwmon")
	swap(t, &smuDriverPath, f.smu)
	// Read-only roots too, so nothing a test asserts depends on the GPU or NPU
	// of the machine running it.
	swap(t, &sysDrmDir, root+"/drm")
	swap(t, &sysAccelDir, root+"/accel")
	// The Z13's CPU, so the Curve Optimizer command is the one the hardware
	// takes; withCPU replaces it.
	swap(t, &procCPUInfoPath, root+"/cpuinfo")
	f.withCPU(t, "AuthenticAMD", 0x1A, 112)
	return f
}

// withCPU writes a two-processor cpuinfo naming the given CPU.
func (f *fakeSysfs) withCPU(t *testing.T, vendor string, family, model int) {
	t.Helper()
	block := fmt.Sprintf("processor\t: %%d\nvendor_id\t: %s\ncpu family\t: %d\nmodel\t\t: %d\nmodel name\t: test\n\n",
		vendor, family, model)
	f.writeFile(t, procCPUInfoPath, fmt.Sprintf(block, 0)+fmt.Sprintf(block, 1))
}

// swap points a path var at v and restores it when the test ends.
func swap(t *testing.T, target *string, v string) {
	t.Helper()
	orig := *target
	*target = v
	t.Cleanup(func() { *target = orig })
}

func (f *fakeSysfs) writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) = %v", path, err)
	}
}

// fakeSMU emulates the ryzen_smu mailbox: a command write is answered by the
// configured response code on the following read, which plain files cannot do.
type fakeSMU struct {
	mu       sync.Mutex
	response uint32
	args     []byte
	writes   int
	failRead bool
	// cmds is every command written, keyed by mailbox file.
	cmds map[string][]uint32
}

// install swaps in the fake's I/O for the duration of the test and resets the
// cached SMUProbeUndervolt result so each test observes its own fake.
func (s *fakeSMU) install(t *testing.T) {
	t.Helper()
	origR, origW := smuReadFile, smuWriteFile
	smuWriteFile = func(path string, data []byte, _ os.FileMode) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.writes++
		if name := filepath.Base(path); name == "smu_args" {
			s.args = append([]byte(nil), data...)
		} else if len(data) == 4 {
			if s.cmds == nil {
				s.cmds = map[string][]uint32{}
			}
			s.cmds[name] = append(s.cmds[name], binary.LittleEndian.Uint32(data))
		}
		return nil
	}
	smuReadFile = func(path string) ([]byte, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.failRead {
			return nil, os.ErrPermission
		}
		if filepath.Base(path) == "smu_args" {
			if s.args == nil {
				return make([]byte, 24), nil
			}
			return s.args, nil
		}
		buf := make([]byte, 4)
		buf[0] = byte(s.response)
		buf[1] = byte(s.response >> 8)
		buf[2] = byte(s.response >> 16)
		buf[3] = byte(s.response >> 24)
		return buf, nil
	}
	resetSMUProbe(t)
	t.Cleanup(func() { smuReadFile, smuWriteFile = origR, origW })
}

// resetSMUProbe clears the cached SMUProbeUndervolt result. The probe is a
// sync.Once in production; tests need each case to re-probe.
func resetSMUProbe(t *testing.T) {
	t.Helper()
	smuProbeOnce = new(sync.Once)
	smuProbeOK = false
	smuProbeResult.Store(0)
	t.Cleanup(func() {
		smuProbeOnce = new(sync.Once)
		smuProbeOK = false
		smuProbeResult.Store(0)
	})
}
