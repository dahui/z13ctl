package hid_test

import (
	"os"
	"path/filepath"
	"testing"

	"z13ctl/internal/hid"
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
