// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gamepad

import (
	"testing"

	evdev "github.com/holoplot/go-evdev"
)

// The fixtures below are the real capability sets of the devices in issue #18,
// transcribed from /proc/bus/input/devices on a ROG Flow Z13 (GZ302EA). The
// touchpad and the touchscreen both advertise ABS_MT_POSITION_X and neither
// carries a gamepad button, which is exactly why the unqualified multitouch test
// used to classify them as grab-only and take them away from the compositor for
// as long as the drawer was open. The stylus escaped only because it reports
// pressure and tilt instead of multitouch slots.

// z13Touchpad is the detachable keyboard's touchpad (0b05:1a30, event5).
func z13Touchpad() deviceInfo {
	return deviceInfo{
		name:  "ASUSTeK Computer Inc. GZ302EA-Keyboard Touchpad",
		id:    evdev.InputID{BusType: 0x0003, Vendor: 0x0B05, Product: 0x1A30, Version: 0x0110},
		props: []evdev.EvProp{evdev.INPUT_PROP_POINTER, evdev.INPUT_PROP_BUTTONPAD},
		keys: []evdev.EvCode{
			evdev.BTN_LEFT, evdev.BTN_TOOL_FINGER, evdev.BTN_TOUCH,
			evdev.BTN_TOOL_DOUBLETAP, evdev.BTN_TOOL_TRIPLETAP,
			evdev.BTN_TOOL_QUADTAP, evdev.BTN_TOOL_QUINTTAP,
		},
		abs: []evdev.EvCode{
			evdev.ABS_X, evdev.ABS_Y, evdev.ABS_MT_SLOT,
			evdev.ABS_MT_POSITION_X, evdev.ABS_MT_POSITION_Y,
			evdev.ABS_MT_TOOL_TYPE, evdev.ABS_MT_TRACKING_ID,
		},
		phys: "usb-0000:c6:00.0-4/input3",
	}
}

// z13Touchscreen is the built-in ELAN panel (04f3:43c7, event11).
func z13Touchscreen() deviceInfo {
	return deviceInfo{
		name:  "ELAN9008:00 04F3:43C7",
		id:    evdev.InputID{BusType: 0x0018, Vendor: 0x04F3, Product: 0x43C7, Version: 0x0100},
		props: []evdev.EvProp{evdev.INPUT_PROP_DIRECT},
		keys:  []evdev.EvCode{evdev.BTN_TOUCH},
		abs: []evdev.EvCode{
			evdev.ABS_X, evdev.ABS_Y, evdev.ABS_MT_SLOT,
			evdev.ABS_MT_TOUCH_MAJOR, evdev.ABS_MT_TOUCH_MINOR,
			evdev.ABS_MT_ORIENTATION, evdev.ABS_MT_POSITION_X,
			evdev.ABS_MT_POSITION_Y, evdev.ABS_MT_TRACKING_ID,
		},
		phys: "i2c-ELAN9008:00",
	}
}

// z13Stylus is the pen digitiser (event14). Same vendor:product and phys as the
// touchscreen — they are two nodes of one panel.
func z13Stylus() deviceInfo {
	return deviceInfo{
		name:  "ELAN9008:00 04F3:43C7 Stylus",
		id:    evdev.InputID{BusType: 0x0018, Vendor: 0x04F3, Product: 0x43C7, Version: 0x0100},
		props: []evdev.EvProp{evdev.INPUT_PROP_DIRECT},
		keys: []evdev.EvCode{
			evdev.BTN_TOOL_PEN, evdev.BTN_TOOL_RUBBER, evdev.BTN_TOUCH,
			evdev.BTN_STYLUS, evdev.BTN_STYLUS2,
		},
		abs: []evdev.EvCode{
			evdev.ABS_X, evdev.ABS_Y, evdev.ABS_PRESSURE,
			evdev.ABS_TILT_X, evdev.ABS_TILT_Y, evdev.ABS_MISC,
		},
		phys: "i2c-ELAN9008:00",
	}
}

const dualSenseMAC = "a0:ab:51:11:22:33"

// dualSenseGamepad is the controller node hid-playstation creates (054c:0ce6).
func dualSenseGamepad() deviceInfo {
	return deviceInfo{
		name: "Sony Interactive Entertainment DualSense Wireless Controller",
		id:   evdev.InputID{BusType: 0x0003, Vendor: 0x054C, Product: 0x0CE6, Version: 0x8111},
		keys: []evdev.EvCode{
			evdev.BTN_SOUTH, evdev.BTN_EAST, evdev.BTN_NORTH, evdev.BTN_WEST,
			evdev.BTN_TL, evdev.BTN_TR, evdev.BTN_TL2, evdev.BTN_TR2,
			evdev.BTN_SELECT, evdev.BTN_START, evdev.BTN_MODE,
			evdev.BTN_THUMBL, evdev.BTN_THUMBR,
		},
		abs: []evdev.EvCode{
			evdev.ABS_X, evdev.ABS_Y, evdev.ABS_Z,
			evdev.ABS_RX, evdev.ABS_RY, evdev.ABS_RZ,
			evdev.ABS_HAT0X, evdev.ABS_HAT0Y,
		},
		uniq: dualSenseMAC,
		phys: "usb-0000:00:14.0-3/input0",
	}
}

// dualSenseTouchpad is the controller's own touchpad — the device the grab-only
// class exists for.
func dualSenseTouchpad() deviceInfo {
	return deviceInfo{
		name:  "Sony Interactive Entertainment DualSense Wireless Controller Touchpad",
		id:    evdev.InputID{BusType: 0x0003, Vendor: 0x054C, Product: 0x0CE6, Version: 0x8111},
		props: []evdev.EvProp{evdev.INPUT_PROP_POINTER, evdev.INPUT_PROP_BUTTONPAD},
		keys:  []evdev.EvCode{evdev.BTN_LEFT, evdev.BTN_TOOL_FINGER, evdev.BTN_TOUCH, evdev.BTN_TOOL_DOUBLETAP},
		abs: []evdev.EvCode{
			evdev.ABS_X, evdev.ABS_Y, evdev.ABS_MT_SLOT,
			evdev.ABS_MT_POSITION_X, evdev.ABS_MT_POSITION_Y,
			evdev.ABS_MT_TRACKING_ID,
		},
		uniq: dualSenseMAC,
		phys: "usb-0000:00:14.0-3/input0",
	}
}

// dualSenseMotion is the controller's accelerometer/gyro node.
func dualSenseMotion() deviceInfo {
	return deviceInfo{
		name:  "Sony Interactive Entertainment DualSense Wireless Controller Motion Sensors",
		id:    evdev.InputID{BusType: 0x0003, Vendor: 0x054C, Product: 0x0CE6, Version: 0x8111},
		props: []evdev.EvProp{evdev.INPUT_PROP_ACCELEROMETER},
		abs:   []evdev.EvCode{evdev.ABS_X, evdev.ABS_Y, evdev.ABS_Z, evdev.ABS_RX, evdev.ABS_RY, evdev.ABS_RZ},
		uniq:  dualSenseMAC,
		phys:  "usb-0000:00:14.0-3/input0",
	}
}

func steamVirtualGamepad() deviceInfo {
	return deviceInfo{
		name: "Microsoft X-Box 360 pad 0",
		id:   evdev.InputID{BusType: 0x0003, Vendor: steamVendor, Product: steamVirtualProduct, Version: 0x0001},
		keys: []evdev.EvCode{evdev.BTN_SOUTH, evdev.BTN_EAST, evdev.BTN_MODE, evdev.BTN_START},
		abs:  []evdev.EvCode{evdev.ABS_X, evdev.ABS_Y, evdev.ABS_HAT0X, evdev.ABS_HAT0Y},
	}
}

func keyboard() deviceInfo {
	return deviceInfo{
		name: "AT Translated Set 2 keyboard",
		id:   evdev.InputID{BusType: 0x0011, Vendor: 0x0001, Product: 0x0001, Version: 0xAB83},
		keys: []evdev.EvCode{evdev.KEY_A, evdev.KEY_ESC, evdev.KEY_LEFTSHIFT},
	}
}

func TestClassify(t *testing.T) {
	withController := []deviceInfo{dualSenseGamepad()}

	tests := []struct {
		name     string
		device   deviceInfo
		gamepads []deviceInfo
		want     deviceClass
	}{
		// Issue #18: the machine's own touch devices must never be grabbed. The
		// "with a controller attached" cases are the real regression guard —
		// plugging a DualSense in must not resurrect the bug.
		{"z13 touchpad alone", z13Touchpad(), nil, deviceIgnore},
		{"z13 touchpad with a controller attached", z13Touchpad(), withController, deviceIgnore},
		{"z13 touchscreen alone", z13Touchscreen(), nil, deviceIgnore},
		{"z13 touchscreen with a controller attached", z13Touchscreen(), withController, deviceIgnore},
		{"z13 stylus", z13Stylus(), withController, deviceIgnore},

		// The behaviour being preserved.
		{"dualsense gamepad", dualSenseGamepad(), nil, deviceGamepad},
		{"dualsense touchpad with its gamepad", dualSenseTouchpad(), withController, deviceGrabOnly},
		{"dualsense motion sensors", dualSenseMotion(), withController, deviceIgnore},

		// Enumerated before its own gamepad node: ignored for now, picked up by
		// the next scan once the controller is tracked.
		{"dualsense touchpad before its gamepad", dualSenseTouchpad(), nil, deviceIgnore},
		// A second controller's touchpad must not be matched by the first's.
		{"dualsense touchpad with only the z13 touchpad known", dualSenseTouchpad(), []deviceInfo{z13Touchpad()}, deviceIgnore},

		{"steam virtual gamepad", steamVirtualGamepad(), nil, deviceGrabOnly},
		{"keyboard", keyboard(), withController, deviceIgnore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.device, tt.gamepads); got != tt.want {
				t.Errorf("classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestClassifyWholeSystem runs every attached device through classification the
// way scan() does — derive the gamepad list first, then classify against it —
// and checks the verdict for all of them at once. This is issue #18 in full: a
// Z13 with a DualSense plugged in must keep its own touchpad and touchscreen
// while still suppressing the controller's touchpad.
func TestClassifyWholeSystem(t *testing.T) {
	attached := []struct {
		device deviceInfo
		want   deviceClass
	}{
		{z13Touchpad(), deviceIgnore},
		{z13Touchscreen(), deviceIgnore},
		{z13Stylus(), deviceIgnore},
		{keyboard(), deviceIgnore},
		{dualSenseGamepad(), deviceGamepad},
		{dualSenseTouchpad(), deviceGrabOnly},
		{dualSenseMotion(), deviceIgnore},
		{steamVirtualGamepad(), deviceGrabOnly},
	}

	// scan() only admits a device to the gamepad list once it classifies as one.
	var gamepads []deviceInfo
	for _, a := range attached {
		if classify(a.device, nil) == deviceGamepad {
			gamepads = append(gamepads, a.device)
		}
	}

	for _, a := range attached {
		if got := classify(a.device, gamepads); got != a.want {
			t.Errorf("classify(%q) = %v, want %v", a.device.name, got, a.want)
		}
	}
}

func TestSameController(t *testing.T) {
	otherMAC := dualSenseGamepad()
	otherMAC.uniq = "a0:ab:51:44:55:66"
	otherMAC.phys = "usb-0000:00:14.0-4/input0"

	noIDs := dualSenseGamepad()
	noIDs.uniq, noIDs.phys = "", ""

	otherPhys := dualSenseTouchpad()
	otherPhys.uniq = ""
	otherPhys.phys = "usb-0000:00:14.0-4/input1"

	samePhys := dualSenseTouchpad()
	samePhys.uniq = ""

	tests := []struct {
		name string
		a, b deviceInfo
		want bool
	}{
		{"same controller, matching uniq", dualSenseTouchpad(), dualSenseGamepad(), true},
		{"two controllers of one model", dualSenseTouchpad(), otherMAC, false},
		{"no uniq, same phys root", samePhys, dualSenseGamepad(), true},
		{"no uniq, different phys root", otherPhys, dualSenseGamepad(), false},
		{"neither reports uniq or phys", dualSenseTouchpad(), noIDs, true},
		{"different vendor", z13Touchpad(), dualSenseGamepad(), false},
		// The Z13 panel's own two nodes do share everything — harmless, since
		// neither is ever a gamepad, but it shows vendor:product alone is not
		// what makes a device grabbable.
		{"touchscreen and its stylus", z13Touchscreen(), z13Stylus(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameController(tt.a, tt.b); got != tt.want {
				t.Errorf("sameController() = %v, want %v", got, tt.want)
			}
			if got := sameController(tt.b, tt.a); got != tt.want {
				t.Errorf("sameController() reversed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPhysRoot(t *testing.T) {
	tests := []struct{ in, want string }{
		{"usb-0000:00:14.0-3/input0", "usb-0000:00:14.0-3"},
		{"usb-0000:00:14.0-3/input1", "usb-0000:00:14.0-3"},
		{"i2c-ELAN9008:00", "i2c-ELAN9008:00"},
		{"", ""},
		{"/input0", ""},
	}
	for _, tt := range tests {
		if got := physRoot(tt.in); got != tt.want {
			t.Errorf("physRoot(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
