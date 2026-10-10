package hid

// device.go — Device type, known device table, and I/O methods.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const (
	// HIDIOCSFEATURE(len): _IOWR('H', 0x06, len) for len=64
	hidiocsfeature64 = 0xC0404806

	// ReportSize is the fixed 64-byte output report length used by all Aura commands.
	ReportSize = 64
)

// Known names one Aura-carrying HID device: the zone name it serves and its
// USB vendor:product. The list is device data (the device file's lighting
// zones), passed in by the caller — this package knows the protocol's report
// and the hidraw ABI, not which machine it is running on.
type Known struct {
	Name            string
	Vendor, Product uint16
}

// hidID is the uevent HID_ID line this device's hidraw nodes carry (USB bus).
func (k Known) hidID() string {
	return fmt.Sprintf("HID_ID=0003:%08X:%08X", k.Vendor, k.Product)
}

// hidrawNode is one open hidraw file.
type hidrawNode struct {
	path string
	name string // the Known name it matched, or "" if unknown
	f    *os.File
}

// Device holds all open hidraw nodes that have the Aura report (0x5d).
// Writes are broadcast to every node, matching g-helper's AsusHid.Write() behavior.
type Device struct {
	nodes []hidrawNode
}

// DeviceInfo describes a discovered hidraw node, for display purposes.
type DeviceInfo struct {
	Path    string // e.g. /dev/hidraw0
	Name    string // the Known name it matched, or "" if unrecognised
	HasAura bool   // true if the HID descriptor contains Report ID 0x5d
	OpenErr string // non-empty if the device could not be opened
}

// Write sends a 64-byte output report to every Aura node (zero-padded).
func (d *Device) Write(data []byte) error {
	buf := make([]byte, ReportSize)
	copy(buf, data)
	var lastErr error
	for _, n := range d.nodes {
		if _, err := n.f.Write(buf); err != nil {
			lastErr = fmt.Errorf("write to %s: %w", n.path, err)
		}
	}
	return lastErr
}

// SetFeature sends a 64-byte feature report via ioctl HIDIOCSFEATURE to every node.
func (d *Device) SetFeature(data []byte) error {
	buf := make([]byte, ReportSize)
	copy(buf, data)
	var lastErr error
	for _, n := range d.nodes {
		_, _, errno := syscall.Syscall(
			syscall.SYS_IOCTL,
			n.f.Fd(),
			hidiocsfeature64,
			uintptr(unsafe.Pointer(&buf[0])),
		)
		if errno != 0 {
			lastErr = fmt.Errorf("HIDIOCSFEATURE on %s: errno %d", n.path, errno)
		}
	}
	return lastErr
}

// Paths returns the raw device paths (e.g. /dev/hidraw0).
func (d *Device) Paths() []string {
	paths := make([]string, len(d.nodes))
	for i, n := range d.nodes {
		paths[i] = n.path
	}
	return paths
}

// Descriptions returns human-readable device descriptions: "path (name)".
func (d *Device) Descriptions() []string {
	descs := make([]string, len(d.nodes))
	for i, n := range d.nodes {
		if n.name != "" {
			descs[i] = fmt.Sprintf("%s (%s)", n.path, n.name)
		} else {
			descs[i] = n.path
		}
	}
	return descs
}

// FilteredView returns a Device that writes only to the node matching nameOrPath.
// nameOrPath may be "keyboard", "lightbar", or a /dev/hidrawN path.
// If nameOrPath is empty, d itself is returned.
// If nameOrPath is non-empty but matches no node, an error is returned.
// The returned *Device shares file descriptors with d; do not call Close on it.
func (d *Device) FilteredView(nameOrPath string) (*Device, error) {
	if nameOrPath == "" {
		return d, nil
	}
	var matched []hidrawNode
	for _, n := range d.nodes {
		if n.name == nameOrPath || n.path == nameOrPath {
			matched = append(matched, n)
		}
	}
	if len(matched) == 0 {
		// Name the nodes actually open, not the names that could exist: during
		// a keyboard reattach this device may hold only the lightbar, and the
		// old hardcoded "available: keyboard, lightbar" reported the keyboard
		// as available in the same breath as "keyboard not found".
		names := make([]string, 0, len(d.nodes))
		for _, n := range d.nodes {
			if n.name != "" {
				names = append(names, n.name)
			}
		}
		sort.Strings(names)
		avail := "none"
		if len(names) > 0 {
			avail = strings.Join(names, ", ")
		}
		return nil, fmt.Errorf("device %q not found (available: %s)", nameOrPath, avail)
	}
	return &Device{nodes: matched}, nil
}

// Close releases all open nodes.
func (d *Device) Close() {
	for _, n := range d.nodes {
		_ = n.f.Close() //nolint:errcheck // best-effort cleanup
	}
}
