package hid

import "os"

// NewTestDevice creates a Device backed by f, for use in unit tests.
// This is a test-only export; the hidrawNode fields are unexported.
func NewTestDevice(f *os.File) *Device {
	return &Device{nodes: []hidrawNode{{path: "test", name: "test", f: f}}}
}

// NewTestDeviceAnon creates a Device backed by f with no name, for testing
// the unnamed-path branch of Descriptions.
func NewTestDeviceAnon(f *os.File) *Device {
	return &Device{nodes: []hidrawNode{{path: "/dev/hidraw0", name: "", f: f}}}
}

// UeventToDevPath is the test-only export of ueventToDevPath.
var UeventToDevPath = ueventToDevPath

// DeviceNameFromUevent is the test-only export of deviceNameFromUevent.
var DeviceNameFromUevent = deviceNameFromUevent
