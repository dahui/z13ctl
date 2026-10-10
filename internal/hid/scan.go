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

// sysHidrawDir is the hidraw class directory; a var so tests can point it at
// a fake tree.
var sysHidrawDir = "/sys/class/hidraw"

func ueventGlob() string { return sysHidrawDir + "/hidraw*/device/uevent" }

// FindDevice opens the appropriate hidraw device(s) among known.
// override may be:
//   - "" — open all matching devices (default)
//   - a Known name — open only that device
//   - a /dev/hidrawN path — open that specific device
func FindDevice(override string, known []Known) (*Device, error) {
	for _, k := range known {
		if override == k.Name {
			return findByName(override, known)
		}
	}
	if override != "" {
		f, err := os.OpenFile(override, os.O_RDWR, 0)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", override, err)
		}
		name := nameFromPath(override, known)
		return &Device{nodes: []hidrawNode{{path: override, name: name, f: f}}}, nil
	}
	return findAll(known)
}

// knownNames lists the known names for an error message.
func knownNames(known []Known) string {
	names := make([]string, len(known))
	for i, k := range known {
		names[i] = k.Name
	}
	return strings.Join(names, " / ")
}

func findAll(known []Known) (*Device, error) {
	entries, err := filepath.Glob(ueventGlob())
	if err != nil {
		return nil, fmt.Errorf("glob hidraw: %w", err)
	}

	var nodes []hidrawNode
	for _, ueventPath := range entries {
		name := deviceNameFromUevent(ueventPath, known)
		if name == "" {
			continue
		}
		devPath := ueventToDevPath(ueventPath)
		f, err := os.OpenFile(devPath, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		if hasAuraReport(ueventPath, f) {
			nodes = append(nodes, hidrawNode{path: devPath, name: name, f: f})
		} else {
			_ = f.Close() //nolint:errcheck // best-effort cleanup
		}
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf(
			"no ASUS Aura devices found (%s); try sudo or add a udev rule:\n"+
				"  SUBSYSTEM==\"hidraw\", ATTRS{idVendor}==\"0b05\", MODE=\"0660\", GROUP=\"users\"",
			knownNames(known),
		)
	}
	return &Device{nodes: nodes}, nil
}

func findByName(want string, known []Known) (*Device, error) {
	entries, _ := filepath.Glob(ueventGlob())
	nameFound := false // true if any node matched the name, even without Aura
	for _, ueventPath := range entries {
		name := deviceNameFromUevent(ueventPath, known)
		if name != want {
			continue
		}
		nameFound = true
		devPath := ueventToDevPath(ueventPath)
		f, err := os.OpenFile(devPath, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		if !hasAuraReport(ueventPath, f) {
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

// ListDevices returns info on all candidate Aura hidraw nodes. The report
// check reads sysfs, so a node this user cannot open is still described.
func ListDevices(known []Known) []DeviceInfo {
	entries, _ := filepath.Glob(ueventGlob())
	var results []DeviceInfo
	for _, ueventPath := range entries {
		name := deviceNameFromUevent(ueventPath, known)
		if name == "" {
			continue
		}
		devPath := ueventToDevPath(ueventPath)
		info := DeviceInfo{Path: devPath, Name: name}
		f, err := os.OpenFile(devPath, os.O_RDWR, 0)
		if err != nil {
			info.OpenErr = err.Error()
			info.HasAura = hasAuraReport(ueventPath, nil)
		} else {
			info.HasAura = hasAuraReport(ueventPath, f)
			_ = f.Close() //nolint:errcheck // best-effort cleanup
		}
		results = append(results, info)
	}
	return results
}

// HasDevice reports whether the known device with the given name is
// currently present in sysfs. It checks presence only — it does not open the
// device or verify the Aura report descriptor.
func HasDevice(name string, known []Known) bool {
	return hasDeviceGlob(ueventGlob(), name, known)
}

// hasDeviceGlob reports whether any uevent file matched by glob identifies the
// device named name. Split out from HasDevice so tests can point glob at a
// temporary sysfs-shaped tree.
func hasDeviceGlob(glob, name string, known []Known) bool {
	entries, _ := filepath.Glob(glob)
	for _, ueventPath := range entries {
		if deviceNameFromUevent(ueventPath, known) == name {
			return true
		}
	}
	return false
}

// deviceNameFromUevent returns the known name for the device at ueventPath,
// or "" if it doesn't match any known device.
func deviceNameFromUevent(ueventPath string, known []Known) string {
	f, err := os.Open(ueventPath)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read-only uevent file, close error not actionable

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, k := range known {
			if line == k.hidID() {
				return k.Name
			}
		}
	}
	return ""
}

// nameFromPath looks up the known name for a /dev/hidrawN path via sysfs.
func nameFromPath(devPath string, known []Known) string {
	base := filepath.Base(devPath)
	return deviceNameFromUevent(sysHidrawDir+"/"+base+"/device/uevent", known)
}

// ueventToDevPath converts a sysfs uevent path (…/hidrawN/device/uevent) to
// its /dev/hidrawN counterpart.
func ueventToDevPath(ueventPath string) string {
	return "/dev/" + filepath.Base(filepath.Dir(filepath.Dir(ueventPath)))
}

// hasAuraReport reports whether the node's HID report descriptor carries
// Report ID 0x5d (short-form encoding: Report ID item 0x85, then the ID byte).
// It reads the descriptor from sysfs (report_descriptor beside the uevent),
// which needs no open node; the HIDIOCGRDESC ioctl on f is the fallback for a
// kernel or sandbox where that file cannot be read. f may be nil.
func hasAuraReport(ueventPath string, f *os.File) bool {
	if data, err := os.ReadFile(filepath.Dir(ueventPath) + "/report_descriptor"); err == nil && len(data) > 0 {
		return descriptorHasAuraReport(data, uint32(len(data)))
	}
	if f == nil {
		return false
	}
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
