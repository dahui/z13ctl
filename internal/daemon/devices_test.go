package daemon

// devices_test.go — the assembled device daemon tests run against.
//
// Assembly is hermetic: driver constructors are pure and device.Assemble
// performs no I/O, so handler tests get the real envelope and validation
// limits without touching hardware. Every write method is guarded
// (writeguard_test.go), so a test that gets past validation gets an error
// rather than writing the developer's machine; a test of a write path uses
// fakeDevice instead.

import (
	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/amd"
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
		if c.Device.ID == device.FallbackID {
			return assembleForTest(c)
		}
	}
	panic("Z13 device file not found in embedded device data")
}()

// assembleForTest assembles c with the registered drivers, then puts device
// data in place of every answer a driver would read from the running kernel,
// so a test's result does not depend on the machine running it, and puts a
// guard in front of every write (writeguard_test.go): reads still reach the
// real drivers, writes return errTestWrite.
func assembleForTest(c device.Config) *device.Device {
	d, err := device.Assemble(c)
	if err != nil {
		panic(err)
	}
	var env *driver.PowerEnvelope
	if d.Power != nil {
		// The PPT driver lays the running kernel's ranges over the device data
		// (asus-armoury's 28–80 W PL1 where it exists). DeviceEnvelope is the
		// driver's view of the device data alone — the file's numbers with the
		// driver's limit wording filled in — which is also what the daemon
		// serves while the EC is not answering. guardWrites rebuilds the engine
		// over it.
		e := d.Power.DeviceEnvelope()
		env = &e
	}
	if c.Undervolt != nil && d.Undervolt == nil {
		// The ryzen-smu-co factory reports the capability unavailable when
		// the out-of-tree ryzen_smu module is not loaded, which is a fact
		// about the machine running the test. The document is what is tested.
		d.Undervolt = amd.NewUndervolter(c.Undervolt.Min, c.Undervolt.Max, c.Undervolt.Text())
		kept := d.Unavailable[:0]
		for _, u := range d.Unavailable {
			if u.Capability != "undervolt" {
				kept = append(kept, u)
			}
		}
		d.Unavailable = kept
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
		d.Fans = deviceFileFans{FanCurves: d.Fans, shape: c.Fans.Shape()}
	}
	if d.Telemetry != nil {
		// The driver drops sources whose hardware is absent and adds the
		// kernel's chart hints. Sample still reads the real machine, which the
		// declaration guard needs.
		d.Telemetry = deviceFileTelemetry{Telemetry: d.Telemetry, info: c.Telemetry.Info()}
	}
	guardWrites(d, env)
	return d
}

// deviceFileEnvelope is the test power limiter: the device file's envelope,
// the default profile's stock row as the reading, and no writes.
type deviceFileEnvelope struct {
	env driver.PowerEnvelope
}

func (p deviceFileEnvelope) Envelope() driver.PowerEnvelope       { return p.env }
func (p deviceFileEnvelope) DeviceEnvelope() driver.PowerEnvelope { return p.env }
func (p deviceFileEnvelope) Apply(api.TDPState) error             { return errTestWrite }
func (p deviceFileEnvelope) Read() (api.TDPState, error) {
	return p.env.StockProfilePPT["balanced"], nil
}

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
		if c.Device.ID == device.FallbackID {
			return c.Power.Envelope()
		}
	}
	panic("Z13 device file not found in embedded device data")
}()

// deviceFileFans reports the device file's fan shape in place of the driver's
// hwmon-resolved one.
type deviceFileFans struct {
	driver.FanCurves
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

// guardWrites replaces every capability's write methods with ones that fail,
// and rebuilds the engine over the guarded fans and a power limiter that
// serves the device file's envelope (env) and reads no hardware at all.
func guardWrites(d *device.Device, env *driver.PowerEnvelope) {
	var fans guardFans
	if d.Fans != nil {
		fans = guardFans{d.Fans}
		d.Fans = fans
	}
	if env != nil {
		d.Power = safety.NewEngine(fans, deviceFileEnvelope{env: *env})
	}
	if d.Profiles != nil {
		d.Profiles = guardProfiles{d.Profiles}
	}
	if d.Toggles != nil {
		d.Toggles = guardToggles{d.Toggles}
	}
	if d.Battery != nil {
		d.Battery = guardBattery{d.Battery}
	}
	if d.Undervolt != nil {
		d.Undervolt = guardUndervolter{d.Undervolt}
	}
	if d.CPUBoost != nil {
		d.CPUBoost = guardBoost{d.CPUBoost}
	}
	if d.Rumble != nil {
		d.Rumble = guardRumble{d.Rumble}
	}
	if d.Lighting != nil {
		d.Lighting = guardLighting{d.Lighting}
	}
}
