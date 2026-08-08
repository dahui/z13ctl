// Package device turns device data into an assembled set of drivers. It reads
// the machine's DMI identity, matches it against the embedded devices/*.toml
// files, and builds a Device whose fields are the per-class driver interfaces
// — nil where the device has no such capability. Adding support for a machine
// whose hardware methods already exist is a data PR: one TOML file, no code.
//
// The one deliberate asymmetry: power control is exposed as a safety.Engine,
// never as the raw driver.PowerLimiter. The engine owns the fans-then-power /
// power-then-fans orderings and the fail-closed floor rule, and holding the
// only reference to the raw limiter is what turns that from a convention into
// a property of the type system.
package device

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/dahui/z13ctl/internal/driver"
	"github.com/dahui/z13ctl/internal/safety"
)

//go:embed devices/*.toml
var deviceFiles embed.FS

// ErrUnknownDevice reports that no embedded device file matches this machine's
// DMI identity. Callers decide policy — the daemon will run a conservative
// generic device rather than refusing to start, but that is its decision, not
// this package's.
var ErrUnknownDevice = errors.New("device: no device file matches this machine")

// Device is one machine's assembled capabilities. A nil field is a capability
// the device does not have; every consumer from the daemon's handlers to the
// socket API's capability listing reads absence the same way.
type Device struct {
	ID    string // device file id, for logs and the capability API
	Model string // short hardware name, for display

	Fans      driver.FanController
	Power     *safety.Engine // power limits, reachable only through the engine
	Profiles  driver.ProfileController
	Lighting  driver.Lighting
	Toggles   driver.Toggles
	Battery   driver.Battery
	Undervolt driver.Undervolter
	Telemetry driver.Telemetry
	Buttons   driver.Buttons
}

var (
	configsOnce sync.Once
	configsAll  []Config
	configsErr  error
)

// Configs returns every embedded device config, parsed and validated, in
// filename order (which makes match order deterministic). The set is fixed at
// build time, so parse failures are build defects: they fail every call, and
// the test suite pins them to fail CI first.
func Configs() ([]Config, error) {
	configsOnce.Do(func() {
		entries, err := deviceFiles.ReadDir("devices")
		if err != nil {
			configsErr = err
			return
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			raw, err := deviceFiles.ReadFile("devices/" + name)
			if err != nil {
				configsErr = fmt.Errorf("%s: %w", name, err)
				return
			}
			var c Config
			if err := toml.Unmarshal(raw, &c); err != nil {
				configsErr = fmt.Errorf("%s: %w", name, err)
				return
			}
			if err := c.Validate(); err != nil {
				configsErr = fmt.Errorf("%s: %w", name, err)
				return
			}
			configsAll = append(configsAll, c)
		}
	})
	return configsAll, configsErr
}

// Detect identifies this machine and assembles its Device. It returns
// ErrUnknownDevice (wrapped with the DMI strings, so a bug report contains
// exactly what a new device file needs) when no device file matches.
func Detect() (*Device, error) {
	configs, err := Configs()
	if err != nil {
		return nil, err
	}
	vendor, product, err := readDMI()
	if err != nil {
		return nil, fmt.Errorf("reading DMI identity: %w", err)
	}
	for _, c := range configs {
		if c.matches(vendor, product) {
			return Assemble(c)
		}
	}
	return nil, fmt.Errorf("%w (vendor %q, product %q)", ErrUnknownDevice, vendor, product)
}

// Assemble builds a Device from a validated config by running each present
// capability block's factory. An unregistered method is an error, not a nil
// capability: the data file names a driver this binary does not carry, and
// silently dropping the capability would misreport the hardware to every
// client.
func Assemble(c Config) (*Device, error) {
	d := &Device{ID: c.Device.ID, Model: c.Device.Model}

	if c.Fans != nil {
		f, err := lookup("fans", fansFactories, c.Fans.Method)
		if err != nil {
			return nil, err
		}
		if d.Fans, err = f(*c.Fans); err != nil {
			return nil, fmt.Errorf("fans (%s): %w", c.Fans.Method, err)
		}
	}
	if c.Power != nil {
		f, err := lookup("power", powerFactories, c.Power.Method)
		if err != nil {
			return nil, err
		}
		raw, err := f(*c.Power)
		if err != nil {
			return nil, fmt.Errorf("power (%s): %w", c.Power.Method, err)
		}
		// The raw limiter goes into the engine and nowhere else.
		d.Power = &safety.Engine{Fans: d.Fans, Power: raw}
	}
	if c.Profiles != nil {
		f, err := lookup("profiles", profilesFactories, c.Profiles.Method)
		if err != nil {
			return nil, err
		}
		if d.Profiles, err = f(*c.Profiles); err != nil {
			return nil, fmt.Errorf("profiles (%s): %w", c.Profiles.Method, err)
		}
	}
	if c.Lighting != nil {
		f, err := lookup("lighting", lightingFactories, c.Lighting.Method)
		if err != nil {
			return nil, err
		}
		if d.Lighting, err = f(*c.Lighting); err != nil {
			return nil, fmt.Errorf("lighting (%s): %w", c.Lighting.Method, err)
		}
	}
	if c.Toggles != nil {
		f, err := lookup("toggles", togglesFactories, c.Toggles.Method)
		if err != nil {
			return nil, err
		}
		if d.Toggles, err = f(*c.Toggles); err != nil {
			return nil, fmt.Errorf("toggles (%s): %w", c.Toggles.Method, err)
		}
	}
	if c.Battery != nil {
		f, err := lookup("battery", batteryFactories, c.Battery.Method)
		if err != nil {
			return nil, err
		}
		if d.Battery, err = f(*c.Battery); err != nil {
			return nil, fmt.Errorf("battery (%s): %w", c.Battery.Method, err)
		}
	}
	if c.Undervolt != nil {
		f, err := lookup("undervolt", undervoltFactories, c.Undervolt.Method)
		if err != nil {
			return nil, err
		}
		if d.Undervolt, err = f(*c.Undervolt); err != nil {
			return nil, fmt.Errorf("undervolt (%s): %w", c.Undervolt.Method, err)
		}
	}
	if c.Telemetry != nil {
		f, err := lookup("telemetry", telemetryFactories, c.Telemetry.Method)
		if err != nil {
			return nil, err
		}
		if d.Telemetry, err = f(*c.Telemetry); err != nil {
			return nil, fmt.Errorf("telemetry (%s): %w", c.Telemetry.Method, err)
		}
	}
	if c.Button != nil {
		f, err := lookup("buttons", buttonsFactories, c.Button.Method)
		if err != nil {
			return nil, err
		}
		if d.Buttons, err = f(*c.Button); err != nil {
			return nil, fmt.Errorf("buttons (%s): %w", c.Button.Method, err)
		}
	}
	return d, nil
}
