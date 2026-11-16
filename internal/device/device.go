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
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"
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
	Name  string // display name from device data ("" when it gives none)

	Fans      driver.FanController
	Power     *safety.Engine // power limits, reachable only through the engine
	Profiles  driver.ProfileController
	Lighting  driver.Lighting
	Toggles   driver.Toggles
	Battery   driver.Battery
	Undervolt driver.Undervolter
	CPUBoost  driver.CPUBoost
	Telemetry driver.Telemetry
	Buttons   driver.Buttons

	// ButtonLabel is the default button's name from device data, "" when the
	// device declares no default. Data, not a driver: nothing reads hardware
	// to learn it. Served as device-get's button_label for older clients.
	ButtonLabel string

	// ButtonList is every button the device declares, in file order: the
	// choices a user has for opening voltaire. Buttons watches all of them,
	// each press carrying its ID.
	ButtonList []ButtonInfo

	// DefaultButton is the ID of the button marked default, "" for none.
	DefaultButton string
}

// ButtonInfo names one declared hardware button.
type ButtonInfo struct {
	ID    string
	Label string
}

// ReleaseFans hands the fans back to firmware auto and leaves keep in force. It
// is the one release entry point for the daemon and the CLI — a source scan in
// both packages rejects a direct Fans.Release() — because on the Z13 a release
// resets the power limits to the firmware profile's own (issue #22), and only
// safety.Engine.ReleaseFans puts keep back afterwards. A device with no power
// control has no limit to lose, so it releases directly. nil keep means the
// firmware profile's own limits; a device with no fan control is a no-op.
func (d *Device) ReleaseFans(keep *api.TDPState) error {
	if d.Power != nil {
		return d.Power.ReleaseFans(keep)
	}
	if d.Fans == nil {
		return nil
	}
	return d.Fans.Release()
}

// HandBackToFirmware puts a firmware profile's own power limits back in force:
// the stock row first, then the fan release that re-applies the firmware's
// limits (see safety.Engine.HandBackToFirmware). A device with no power control
// has no limits to hand back and only releases.
func (d *Device) HandBackToFirmware(profile string) error {
	if d.Power != nil {
		return d.Power.HandBackToFirmware(profile)
	}
	return d.ReleaseFans(nil)
}

var (
	configsOnce sync.Once
	configsAll  []Config
	configsErr  error
)

// ParseConfig decodes one device file and validates it. A key the format does
// not define is an error, not ignored: a misspelled key in hand-written device
// data would otherwise silently fall back to a default — tdp_max_sfae leaving
// the safe maximum at zero — and the data would read as correct.
func ParseConfig(raw []byte) (Config, error) {
	var c Config
	md, err := toml.Decode(string(raw), &c)
	if err != nil {
		return Config{}, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

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
			c, err := ParseConfig(raw)
			if err != nil {
				configsErr = fmt.Errorf("%s: %w", name, err)
				return
			}
			configsAll = append(configsAll, c)
		}
	})
	return configsAll, configsErr
}

// MatchConfig identifies this machine and returns its device config,
// unassembled. It returns ErrUnknownDevice (wrapped with the DMI strings, so a
// bug report contains exactly what a new device file needs) when no device
// file matches.
//
// Most callers want Detect. MatchConfig exists for the transitional daemon,
// which strips the capabilities whose drivers still live inside it before
// assembling; it goes away when that gap closes.
func MatchConfig() (Config, error) {
	configs, err := Configs()
	if err != nil {
		return Config{}, err
	}
	vendor, product, err := readDMI()
	if err != nil {
		return Config{}, fmt.Errorf("reading DMI identity: %w", err)
	}
	for _, c := range configs {
		if c.matches(vendor, product) {
			return c, nil
		}
	}
	return Config{}, fmt.Errorf("%w (vendor %q, product %q)", ErrUnknownDevice, vendor, product)
}

// Detect identifies this machine and assembles its Device.
func Detect() (*Device, error) {
	c, err := MatchConfig()
	if err != nil {
		return nil, err
	}
	return Assemble(c)
}

// Assemble builds a Device from a validated config by running each present
// capability block's factory. An unregistered method is an error, not a nil
// capability: the data file names a driver this binary does not carry, and
// silently dropping the capability would misreport the hardware to every
// client.
func Assemble(c Config) (*Device, error) {
	d := &Device{ID: c.Device.ID, Model: c.Device.Model, Name: c.Device.Name}

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
	if c.CPU != nil && c.CPU.Boost != "" {
		f, err := lookup("cpu boost", cpuBoostFactories, c.CPU.Boost)
		if err != nil {
			return nil, err
		}
		if d.CPUBoost, err = f(*c.CPU); err != nil {
			return nil, fmt.Errorf("cpu boost (%s): %w", c.CPU.Boost, err)
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
	var watchers mergedButtons
	for _, b := range c.Buttons {
		f, err := lookup("buttons", buttonsFactories, b.Method)
		if err != nil {
			return nil, err
		}
		w, err := f(b)
		if err != nil {
			return nil, fmt.Errorf("button %q (%s): %w", b.ID, b.Method, err)
		}
		watchers = append(watchers, w)
		d.ButtonList = append(d.ButtonList, ButtonInfo{ID: b.ID, Label: b.Label})
		if b.Default {
			d.DefaultButton, d.ButtonLabel = b.ID, b.Label
		}
	}
	switch len(watchers) {
	case 0:
	case 1:
		d.Buttons = watchers[0]
	default:
		d.Buttons = watchers
	}
	return d, nil
}

// mergedButtons watches several buttons as one driver.Buttons, so the daemon
// keeps a single watcher however many buttons a device declares. Each press
// carries its button's ID.
type mergedButtons []driver.Buttons

// Watch runs every watcher until ctx is done. One that stops early — a node
// the keyboard guard refused, say — is logged and does not take the others
// with it; Watch returns once all have stopped.
func (m mergedButtons) Watch(ctx context.Context, ch chan<- driver.ButtonEvent) error {
	var wg sync.WaitGroup
	for _, w := range m {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Watch(ctx, ch); err != nil {
				slog.Warn("button watcher stopped", "err", err)
			}
		}()
	}
	wg.Wait()
	return nil
}
