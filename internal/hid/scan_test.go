package hid_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dahui/z13ctl/internal/hid"
)

func TestUeventToDevPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		uevent string
		want   string
	}{
		{"/sys/class/hidraw/hidraw0/device/uevent", "/dev/hidraw0"},
		{"/sys/class/hidraw/hidraw7/device/uevent", "/dev/hidraw7"},
		{"/sys/class/hidraw/hidraw12/device/uevent", "/dev/hidraw12"},
	}
	for _, tt := range tests {
		t.Run(tt.uevent, func(t *testing.T) {
			t.Parallel()
			got := hid.UeventToDevPath(tt.uevent)
			if got != tt.want {
				t.Errorf("UeventToDevPath(%q) = %q, want %q", tt.uevent, got, tt.want)
			}
		})
	}
}

func TestDeviceNameFromUevent_Known(t *testing.T) {
	t.Parallel()

	tests := []struct {
		hidID string
		want  string
	}{
		{"HID_ID=0003:00000B05:000018C6", "lightbar"},
		{"HID_ID=0003:00000B05:00001A30", "keyboard"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			// Write a temp uevent file containing the HID_ID line.
			dir := t.TempDir()
			path := filepath.Join(dir, "uevent")
			content := "DRIVER=hid\n" + tt.hidID + "\nHID_NAME=ASUS\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			got := hid.DeviceNameFromUevent(path)
			if got != tt.want {
				t.Errorf("DeviceNameFromUevent = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceNameFromUevent_Unknown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "uevent")
	content := "HID_ID=0003:00000B05:0000FFFF\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := hid.DeviceNameFromUevent(path)
	if got != "" {
		t.Errorf("DeviceNameFromUevent = %q, want \"\"", got)
	}
}

func TestDeviceNameFromUevent_Missing(t *testing.T) {
	t.Parallel()
	got := hid.DeviceNameFromUevent("/nonexistent/path/uevent")
	if got != "" {
		t.Errorf("DeviceNameFromUevent(missing) = %q, want \"\"", got)
	}
}

// writeUevent creates dir/<node>/device/uevent containing the given HID_ID line,
// mirroring the /sys/class/hidraw/<node>/device/uevent layout.
func writeUevent(t *testing.T, dir, node, hidID string) {
	t.Helper()
	devDir := filepath.Join(dir, node, "device")
	if err := os.MkdirAll(devDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "DRIVER=hid\n" + hidID + "\nHID_NAME=ASUS\n"
	if err := os.WriteFile(filepath.Join(devDir, "uevent"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHasDeviceGlob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeUevent(t, dir, "hidraw7", "HID_ID=0003:00000B05:00001A30") // keyboard
	writeUevent(t, dir, "hidraw9", "HID_ID=0003:00000B05:000018C6") // lightbar
	writeUevent(t, dir, "hidraw3", "HID_ID=0003:00001111:00002222") // unrelated
	glob := filepath.Join(dir, "hidraw*", "device", "uevent")

	tests := []struct {
		name string
		want bool
	}{
		{"keyboard", true},
		{"lightbar", true},
		{"mouse", false}, // not a known device name
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hid.HasDeviceGlob(glob, tt.name); got != tt.want {
				t.Errorf("HasDeviceGlob(_, %q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestHasDeviceGlob_KeyboardAbsent(t *testing.T) {
	t.Parallel()

	// Only the lightbar is present (the detachable keyboard is detached).
	dir := t.TempDir()
	writeUevent(t, dir, "hidraw9", "HID_ID=0003:00000B05:000018C6") // lightbar
	glob := filepath.Join(dir, "hidraw*", "device", "uevent")

	if hid.HasDeviceGlob(glob, "keyboard") {
		t.Error("HasDeviceGlob keyboard = true, want false when keyboard is detached")
	}
	if !hid.HasDeviceGlob(glob, "lightbar") {
		t.Error("HasDeviceGlob lightbar = false, want true")
	}
}

func TestHasDeviceGlob_NoMatches(t *testing.T) {
	t.Parallel()
	glob := filepath.Join(t.TempDir(), "hidraw*", "device", "uevent")
	if hid.HasDeviceGlob(glob, "keyboard") {
		t.Error("HasDeviceGlob on empty tree = true, want false")
	}
}

// TestDescriptorHasAuraReport covers the report-descriptor scan, including the
// clamp on the kernel-supplied length. The kernel caps it at the buffer size
// today, so an over-long value is defensive — but an unclamped index would
// panic during device enumeration, before any command has run.
func TestDescriptorHasAuraReport(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 4096)
	// Report ID item (0x85) followed by the Aura report ID (0x5d).
	copy(buf[10:], []byte{0x85, 0x5d})

	tests := []struct {
		name  string
		value []byte
		size  uint32
		want  bool
	}{
		{"aura report present", buf, 4096, true},
		{"size stops before the marker", buf, 10, false},
		{"size covers only the first marker byte", buf, 11, false},
		{"no marker", make([]byte, 64), 64, false},
		{"zero size", buf, 0, false},
		{"size beyond the buffer is clamped", buf, 1 << 20, true},
		{"size beyond an empty buffer is clamped", []byte{}, 1 << 20, false},
		{"marker at the very end", []byte{0x00, 0x85, 0x5d}, 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hid.DescriptorHasAuraReport(tt.value, tt.size); got != tt.want {
				t.Errorf("DescriptorHasAuraReport(size=%d) = %v, want %v", tt.size, got, tt.want)
			}
		})
	}
}
