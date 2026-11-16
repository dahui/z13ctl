package daemon

// devices_test.go — the assembled device daemon tests run against.
//
// Assembly is hermetic: driver constructors are pure and device.Assemble
// performs no I/O, so handler tests get the real envelope and validation
// limits without touching hardware. The usual warning still applies with more
// force than ever: a test that gets past validation into a driver method
// writes the developer's actual machine, so tests must stay on rejection and
// state-only paths exactly as before.

import (
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"

	// Registers the Z13 driver factories, as the voltaire binary does.
	_ "github.com/dahui/voltaire/v2/internal/drivers/register"
)

// testDev is the full Z13 device assembled exactly as Run's assembly does,
// from the embedded device file, with no DMI matching involved. Constructors
// are pure — the lighting driver holds no HID handle until Reopen, which
// nothing here calls — so sharing one instance across tests is safe.
var testDev = func() *device.Device {
	configs, err := device.Configs()
	if err != nil {
		panic(err)
	}
	for _, c := range configs {
		if c.Device.ID == fallbackDeviceID {
			return assembleForTest(c)
		}
	}
	panic("Z13 device file not found in embedded device data")
}()

// assembleForTest assembles c with the registered drivers, then puts device
// data in place of every answer a driver would read from the running kernel,
// so a test's result does not depend on the machine running it. Reads and
// writes still go to the real drivers, so the rule that a daemon test must
// stay on rejection paths is unchanged.
func assembleForTest(c device.Config) *device.Device {
	d, err := device.Assemble(c)
	if err != nil {
		panic(err)
	}
	if d.Power != nil {
		// The PPT driver lays the running kernel's ranges over the device data
		// (asus-armoury's 28–80 W PL1 where it exists). DeviceEnvelope is the
		// driver's view of the device data alone — the file's numbers with the
		// driver's limit wording filled in — which is also what the daemon
		// serves while the EC is not answering.
		d.Power.Power = deviceFileEnvelope{Power: d.Power.Power,
			env: d.Power.Power.(driver.DeviceEnvelope).DeviceEnvelope()}
	}
	if d.Profiles != nil {
		// The driver offers the kernel's choices filtered by the device data,
		// and a machine without that handler would offer a different list.
		d.Profiles = deviceFileProfiles{ProfileController: d.Profiles, names: c.Profiles.Names, def: c.Profiles.Default}
	}
	if d.Toggles != nil {
		// The driver describes toggles from the kernel's attribute metadata
		// when asus-armoury is loaded.
		d.Toggles = deviceFileToggles{Toggles: d.Toggles, specs: declaredToggles(c)}
	}
	if d.Fans != nil {
		// The driver lays hwmon's point count and fan labels over the data.
		d.Fans = deviceFileFans{FanController: d.Fans, shape: c.Fans.Shape()}
	}
	if d.Telemetry != nil {
		// The driver drops sources whose hardware is absent and adds the
		// kernel's chart hints. Sample still reads the real machine, which the
		// declaration guard needs.
		d.Telemetry = deviceFileTelemetry{Telemetry: d.Telemetry, info: c.Telemetry.Info()}
	}
	return d
}

// deviceFileEnvelope reports the device file's envelope in place of the
// driver's live one.
type deviceFileEnvelope struct {
	safety.Power
	env driver.PowerEnvelope
}

func (p deviceFileEnvelope) Envelope() driver.PowerEnvelope { return p.env }

// deviceFileProfiles reports the device file's profile names in place of the
// driver's kernel-filtered list.
type deviceFileProfiles struct {
	driver.ProfileController
	names []string
	def   string
}

func (p deviceFileProfiles) Names() []string { return append([]string(nil), p.names...) }

// Default is the device file's declared default, rather than the driver's
// rule applied to its sysfs-read list.
func (p deviceFileProfiles) Default() string { return p.def }

// testEnv is the Z13 device file's power envelope, for the pure tick
// functions' tables. Not testDev.Power.Envelope(): the driver lays the running
// kernel's PPT ranges over the device data, so that would make every table
// depend on whether the machine running the tests has asus-armoury.
var testEnv driver.PowerEnvelope = func() driver.PowerEnvelope {
	configs, err := device.Configs()
	if err != nil {
		panic(err)
	}
	for _, c := range configs {
		if c.Device.ID == fallbackDeviceID {
			return c.Power.Envelope()
		}
	}
	panic("Z13 device file not found in embedded device data")
}()

// deviceFileFans reports the device file's fan shape in place of the driver's
// hwmon-resolved one.
type deviceFileFans struct {
	driver.FanController
	shape driver.FanShape
}

func (f deviceFileFans) Shape() driver.FanShape { return f.shape }

// deviceFileTelemetry reports the device file's telemetry declaration in
// place of the driver's hardware-checked one.
type deviceFileTelemetry struct {
	driver.Telemetry
	info driver.TelemetryInfo
}

func (t deviceFileTelemetry) Info() driver.TelemetryInfo { return t.info }

// deviceFileToggles reports the device file's toggle entries in place of the
// driver's kernel-described list.
type deviceFileToggles struct {
	driver.Toggles
	specs []driver.ToggleSpec
}

func (t deviceFileToggles) List() []driver.ToggleSpec {
	return append([]driver.ToggleSpec(nil), t.specs...)
}

// declaredToggles is the device file's toggle entries as specs.
func declaredToggles(c device.Config) []driver.ToggleSpec {
	if c.Toggles == nil {
		return nil
	}
	var out []driver.ToggleSpec
	for _, e := range c.Toggles.Entries {
		if !e.Hidden {
			out = append(out, driver.ToggleSpec{ID: e.ID, Label: e.Label, Description: e.Description, Kind: driver.ToggleBool})
		}
	}
	return out
}
