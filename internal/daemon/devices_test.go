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
	"github.com/dahui/z13ctl/internal/device"
	"github.com/dahui/z13ctl/internal/driver"

	// Registers the Z13 driver factories, as the z13ctl binary does.
	_ "github.com/dahui/z13ctl/internal/drivers/asusz13/register"
)

// testDev is the Z13 device assembled exactly as Run's transitional assembly
// does — lighting and buttons stripped — from the embedded device file, with
// no DMI matching involved. Drivers are stateless, so sharing one instance
// across tests is safe.
var testDev = func() *device.Device {
	configs, err := device.Configs()
	if err != nil {
		panic(err)
	}
	for _, c := range configs {
		if c.Device.ID == fallbackDeviceID {
			c.Lighting, c.Button = nil, nil
			d, err := device.Assemble(c)
			if err != nil {
				panic(err)
			}
			return d
		}
	}
	panic("Z13 device file not found in embedded device data")
}()

// testEnv is testDev's power envelope — the same envelope Run hands the
// watchers and handlers — for the pure tick functions' tables.
var testEnv driver.PowerEnvelope = testDev.Power.Envelope()
