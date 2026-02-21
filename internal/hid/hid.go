// Package hid provides Linux hidraw device I/O for ASUS Aura-capable peripherals.
//
// Scans /sys/class/hidraw to find devices matching known ASUS HID IDs, opens
// them for writing, and verifies Aura report ID 0x5d is present in the HID
// descriptor before accepting the node.
//
// File layout:
//
//	device.go — Device type, known device table, Write/SetFeature/Close methods
//	scan.go   — FindDevice, ListDevices, sysfs helpers, Aura report verification
package hid
