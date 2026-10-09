package cmd

// live_test.go — with a daemon running, status and fancurve --get must not
// read the embedded controller themselves (the daemon gates those reads on its
// wedged-EC latch; reading around it is the PR #26 hard lock). The drivers
// here fail the test on any call, so a read that slips past the daemon path is
// a failure rather than a silent sysfs access.

import (
	"slices"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// Each embeds its interface as nil: any method not overridden panics, and the
// overridden ones fail the test. Either way a call is caught.
type tripTelemetry struct {
	driver.Telemetry
	t *testing.T
}

func (f tripTelemetry) Sample() (driver.Sample, error) {
	f.t.Error("Telemetry.Sample called with a daemon up")
	return driver.Sample{}, nil
}

type tripFans struct {
	driver.FanController
	t *testing.T
}

func (f tripFans) ReadRPM() ([]int, error) {
	f.t.Error("Fans.ReadRPM called with a daemon up")
	return nil, nil
}

type tripBattery struct {
	driver.Battery
	t *testing.T
}

func (f tripBattery) Status() (driver.BatteryStatus, error) {
	f.t.Error("Battery.Status called with a daemon up")
	return driver.BatteryStatus{}, nil
}

func (f tripBattery) ChargeLimit() (int, error) {
	f.t.Error("Battery.ChargeLimit called with a daemon up")
	return 0, nil
}

func trippingDevice(t *testing.T) *device.Device {
	return &device.Device{
		Telemetry: tripTelemetry{t: t},
		Fans:      tripFans{t: t},
		Battery:   tripBattery{t: t},
	}
}

func TestReadLiveTakesEverythingFromTheDaemon(t *testing.T) {
	st := &api.State{
		Temperature: 61, RPM: []int{2100, 2350},
		OnAC: true, SourceKnown: true, Charger: "usb-c",
		BatteryLevel: 77, Battery: 80,
	}
	r := readLive(trippingDevice(t), st, true)
	if r.TempC != 61 || !slices.Equal(r.RPM, []int{2100, 2350}) || !r.OnAC || !r.ACKnown ||
		r.Charger != driver.ChargerUSBC || r.Capacity != 77 || r.Limit != 80 || !r.ViaDaemon {
		t.Errorf("readLive = %+v, want the daemon's values", r)
	}
}

// While wedged the daemon omits every EC-backed field. Those must come back
// unavailable — not be filled in by reading the hardware.
func TestReadLiveWedgedDaemonReadsNothing(t *testing.T) {
	r := readLive(trippingDevice(t), &api.State{Profile: "balanced"}, true)
	if r.TempC != 0 || len(r.RPM) != 0 || r.ACKnown || r.Capacity != 0 || r.Limit != 0 {
		t.Errorf("readLive = %+v, want nothing available", r)
	}
}

// A daemon that answered with an error is still up and still guarding the EC.
func TestReadLiveDaemonErrorReadsNothing(t *testing.T) {
	r := readLive(trippingDevice(t), nil, true)
	if !r.ViaDaemon || len(r.RPM) != 0 {
		t.Errorf("readLive = %+v, want an empty daemon-sourced reading", r)
	}
}

func TestReadLiveOldDaemonFallsBackToFanRPM(t *testing.T) {
	r := readLive(trippingDevice(t), &api.State{FanRPM: 1900}, true)
	if !slices.Equal(r.RPM, []int{1900}) {
		t.Errorf("RPM = %v, want fan_rpm from a daemon that predates the slice", r.RPM)
	}
}

func TestFormatRPM(t *testing.T) {
	for _, tt := range []struct {
		in   []int
		want string
	}{
		{nil, "N/A"},
		{[]int{0}, "0 RPM"},
		{[]int{2100, 2350}, "2100 / 2350 RPM"},
	} {
		if got := formatRPM(tt.in); got != tt.want {
			t.Errorf("formatRPM(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPowerSourceText(t *testing.T) {
	for _, tt := range []struct {
		onAC bool
		c    driver.Charger
		want string
	}{
		{true, driver.ChargerAdapter, "AC via adapter"},
		{true, driver.ChargerUSBC, "AC via USB-C"},
		{true, driver.ChargerUnknown, "AC"},      // a device with one input
		{true, driver.Charger("wireless"), "AC"}, // a kind this build does not know
		{false, driver.ChargerNone, "battery"},
		{false, driver.ChargerUSBC, "battery"}, // a stale kind never outranks OnAC
	} {
		if got := powerSourceText(tt.onAC, tt.c); got != tt.want {
			t.Errorf("powerSourceText(%v, %q) = %q, want %q", tt.onAC, tt.c, got, tt.want)
		}
	}
}
