// device.go — Device type, known device table, and I/O methods.
package hid

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	// HIDIOCSFEATURE(len): _IOWR('H', 0x06, len) for len=64
	hidiocsfeature64 = 0xC0404806

	// ReportSize is the fixed 64-byte output report length used by all Aura commands.
	ReportSize = 64
)

// deviceSpec maps a HID_ID uevent value to a human-readable device name.
type deviceSpec struct {
	hidID string
	name  string
}

// knownDevices lists all ASUS HID devices that carry Aura (report 0x5d).
// On the 2025 ROG Flow Z13:
//
//	0x18c6 = N-KEY Device     → lightbar
//	0x1a30 = GZ302EA-Keyboard → keyboard
//
// g-helper's AsusHid.Write() broadcasts to ALL matching devices; we do the same.
var knownDevices = []deviceSpec{
	{"HID_ID=0003:00000B05:000018C6", "lightbar"},
	{"HID_ID=0003:00000B05:00001A30", "keyboard"},
}

// hidrawNode is one open hidraw file.
type hidrawNode struct {
	path string
	name string // "keyboard", "lightbar", or "" if unknown
	f    *os.File
}

// Device holds all open hidraw nodes that have the Aura report (0x5d).
// Writes are broadcast to every node, matching g-helper's AsusHid.Write() behavior.
type Device struct {
	nodes []hidrawNode
}

// DeviceInfo describes a discovered hidraw node, for display purposes.
type DeviceInfo struct {
	Path    string
	Name    string
	HasAura bool
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

// Close releases all open nodes.
func (d *Device) Close() {
	for _, n := range d.nodes {
		n.f.Close()
	}
}
