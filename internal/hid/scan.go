package hid

// scan.go — sysfs device discovery and Aura report descriptor verification.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	// Linux HID ioctls for reading the report descriptor (from <linux/hidraw.h>).
	hidiocgrdescsize = 0x80044801
	hidiocgrdesc     = 0x90044802
)

// hidDescriptor mirrors struct hidraw_report_descriptor from <linux/hidraw.h>.
type hidDescriptor struct {
	size  uint32
	value [4096]byte
}

// FindDevice opens the appropriate hidraw device(s).
// override may be:
//   - "" — open all matching devices (default)
//   - "keyboard" or "lightbar" — open only that named device
//   - a /dev/hidrawN path — open that specific device
func FindDevice(override string) (*Device, error) {
	if override == "keyboard" || override == "lightbar" {
		return findByName(override)
	}
	if override != "" {
		f, err := os.OpenFile(override, os.O_RDWR, 0)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", override, err)
		}
		name := nameFromPath(override)
		return &Device{nodes: []hidrawNode{{path: override, name: name, f: f}}}, nil
	}
	return findAll()
}

func findAll() (*Device, error) {
	entries, err := filepath.Glob("/sys/class/hidraw/hidraw*/device/uevent")
	if err != nil {
		return nil, fmt.Errorf("glob hidraw: %w", err)
	}

	var nodes []hidrawNode
	for _, ueventPath := range entries {
		name := deviceNameFromUevent(ueventPath)
		if name == "" {
			continue
		}
		devPath := ueventToDevPath(ueventPath)
		f, err := os.OpenFile(devPath, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		if hasAuraReport(f) {
			nodes = append(nodes, hidrawNode{path: devPath, name: name, f: f})
		} else {
			_ = f.Close() //nolint:errcheck // best-effort cleanup
		}
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf(
			"no ASUS Aura devices found (keyboard / lightbar); try sudo or add a udev rule:\n" +
				"  SUBSYSTEM==\"hidraw\", ATTRS{idVendor}==\"0b05\", MODE=\"0660\", GROUP=\"users\"",
		)
	}
	return &Device{nodes: nodes}, nil
}

func findByName(want string) (*Device, error) {
	entries, _ := filepath.Glob("/sys/class/hidraw/hidraw*/device/uevent")
	nameFound := false // true if any node matched the name, even without Aura
	for _, ueventPath := range entries {
		name := deviceNameFromUevent(ueventPath)
		if name != want {
			continue
		}
		nameFound = true
		devPath := ueventToDevPath(ueventPath)
		f, err := os.OpenFile(devPath, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		if !hasAuraReport(f) {
			_ = f.Close() //nolint:errcheck // best-effort cleanup
			continue      // skip nodes without the Aura report, same as findAll
		}
		return &Device{nodes: []hidrawNode{{path: devPath, name: name, f: f}}}, nil
	}
	if nameFound {
		return nil, fmt.Errorf(
			"no Aura-capable node found for %q; try sudo or add a udev rule:\n"+
				"  SUBSYSTEM==\"hidraw\", ATTRS{idVendor}==\"0b05\", MODE=\"0660\", GROUP=\"users\"",
			want,
		)
	}
	return nil, fmt.Errorf(
		"%q device not found; try sudo or add a udev rule:\n"+
			"  SUBSYSTEM==\"hidraw\", ATTRS{idVendor}==\"0b05\", MODE=\"0660\", GROUP=\"users\"",
		want,
	)
}

// ListDevices returns info on all candidate Aura hidraw nodes.
func ListDevices() []DeviceInfo {
	entries, _ := filepath.Glob("/sys/class/hidraw/hidraw*/device/uevent")
	var results []DeviceInfo
	for _, ueventPath := range entries {
		name := deviceNameFromUevent(ueventPath)
		if name == "" {
			continue
		}
		devPath := ueventToDevPath(ueventPath)
		info := DeviceInfo{Path: devPath, Name: name}
		f, err := os.OpenFile(devPath, os.O_RDWR, 0)
		if err != nil {
			info.OpenErr = err.Error()
		} else {
			info.HasAura = hasAuraReport(f)
			_ = f.Close() //nolint:errcheck // best-effort cleanup
		}
		results = append(results, info)
	}
	return results
}

// HasDevice reports whether a known Aura device with the given friendly name
// ("keyboard" or "lightbar") is currently present in sysfs. It checks presence
// only — it does not open the device or verify the Aura report descriptor.
func HasDevice(name string) bool {
	return hasDeviceGlob("/sys/class/hidraw/hidraw*/device/uevent", name)
}

// hasDeviceGlob reports whether any uevent file matched by glob identifies the
// device named name. Split out from HasDevice so tests can point glob at a
// temporary sysfs-shaped tree.
func hasDeviceGlob(glob, name string) bool {
	entries, _ := filepath.Glob(glob)
	for _, ueventPath := range entries {
		if deviceNameFromUevent(ueventPath) == name {
			return true
		}
	}
	return false
}

// deviceNameFromUevent returns the friendly name for the device at ueventPath,
// or "" if it doesn't match any known device.
func deviceNameFromUevent(ueventPath string) string {
	f, err := os.Open(ueventPath)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read-only uevent file, close error not actionable

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, spec := range knownDevices {
			if line == spec.hidID {
				return spec.name
			}
		}
	}
	return ""
}

// nameFromPath looks up the friendly name for a /dev/hidrawN path via sysfs.
func nameFromPath(devPath string) string {
	base := filepath.Base(devPath)
	ueventPath := "/sys/class/hidraw/" + base + "/device/uevent"
	return deviceNameFromUevent(ueventPath)
}

// ueventToDevPath converts a sysfs uevent path to its /dev/hidrawN counterpart.
func ueventToDevPath(ueventPath string) string {
	parts := strings.Split(ueventPath, "/")
	return "/dev/" + parts[4]
}

// hasAuraReport reads the HID report descriptor and scans for Report ID 0x5d.
// In HID short-form encoding: Report ID item = 0x85 followed by the ID byte.
func hasAuraReport(f *os.File) bool {
	var desc hidDescriptor

	if _, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL, f.Fd(), hidiocgrdescsize,
		uintptr(unsafe.Pointer(&desc.size)),
	); errno != 0 {
		return false
	}

	if _, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL, f.Fd(), hidiocgrdesc,
		uintptr(unsafe.Pointer(&desc)),
	); errno != 0 {
		return false
	}

	return descriptorHasAuraReport(desc.value[:], desc.size)
}

// descriptorHasAuraReport scans the first size bytes of a report descriptor for
// the Report ID 0x5d item.
//
// size is kernel-supplied. The kernel caps it at HID_MAX_DESCRIPTOR_SIZE, which
// is the length of the buffer, but clamp anyway: an out-of-range value here
// would panic during device enumeration, before any command has run.
func descriptorHasAuraReport(value []byte, size uint32) bool {
	n := int(size)
	if n > len(value) || n < 0 {
		n = len(value)
	}
	for i := 0; i < n-1; i++ {
		if value[i] == 0x85 && value[i+1] == 0x5d {
			return true
		}
	}
	return false
}
