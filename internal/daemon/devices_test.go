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
	_ "github.com/dahui/voltaire/v2/internal/drivers/asusz13/register"
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
			d, err := device.Assemble(c)
			if err != nil {
				panic(err)
			}
			// The PPT driver lays the running kernel's ranges over the device
			// data (asus-armoury's 28–80 W PL1 where it exists), which would make
			// every envelope-reading test depend on the machine running it. Reads
			// and writes still go to the real driver, so the rule that a daemon
			// test must stay on rejection paths is unchanged.
			d.Power.Power = deviceFileEnvelope{Power: d.Power.Power, env: c.Power.Envelope()}
			return d
		}
	}
	panic("Z13 device file not found in embedded device data")
}()

// deviceFileEnvelope reports the device file's envelope in place of the
// driver's live one.
type deviceFileEnvelope struct {
	safety.Power
	env driver.PowerEnvelope
}

func (p deviceFileEnvelope) Envelope() driver.PowerEnvelope { return p.env }

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
