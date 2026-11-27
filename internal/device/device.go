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

	Fans      driver.FanCurves
	Power     *safety.Engine // power limits, reachable only through the engine
	Profiles  driver.ProfileController
	Lighting  driver.Lighting
	Toggles   driver.Toggles
	Battery   driver.Battery
	Undervolt driver.Undervolter
	CPUBoost  driver.CPUBoost
	Rumble    driver.Rumble
	Telemetry driver.Telemetry
	Buttons   driver.Buttons

	// ButtonLabel is the default button's name from device data, "" when the
	// device declares no default. Data, not a driver: nothing reads hardware
	// to learn it. Served as device-get's button_label for older clients.
	ButtonLabel string

	// PowerSlider is power.slider: a TDP set while no custom profile is active
	// is a remembered device setting, not a profile (see PowerConfig.Slider).
	PowerSlider bool

	// PowerPerSource says the power envelope differs between AC and battery in
	// device data (power.tdp_max_battery or power.stock_ppt_battery), so a
	// cached envelope is only good for the source it was read on.
	PowerPerSource bool

	// ProfilePin is profiles.pin, the firmware profile the daemon holds; "" for
	// none.
	ProfilePin string

	// Quickbar is quickbar.controls: the drawer's default controls on this
	// device, in order; nil for the GUI's built-in default. Data for clients
	// only, served as device-get's quickbar.
	Quickbar []string

	// ButtonList is every button the device declares, in file order: the
	// choices a user has for opening voltaire. Buttons watches all of them,
	// each press carrying its ID.
	ButtonList []ButtonInfo

	// DefaultButton is the ID of the button marked default, "" for none.
	DefaultButton string

	// Unavailable lists the capabilities the device data declares whose
	// interface this machine does not have (a kernel module not installed),
	// each with the reason. The capability's field is nil, as for one the
	// device does not have, and the reason is what every surface says instead
	// of "no such control".
	Unavailable []Unavailable

	// releaseFans is the fan controller's raw Release, for ReleaseFans on a
	// device with no power control (and so no engine). Unexported: a release
	// anywhere else would drop the custom power limit without restoring it.
	releaseFans func() error

	// YieldsInGamingMode says the device data leaves something to Steam in
	// Gaming Mode, and GamingMode whether this Device was assembled for such
	// a session. The daemon re-assembles (restarts) when the session kind
	// changes under a device that yields.
	YieldsInGamingMode, GamingMode bool
}

// Unavailable is one declared capability this machine cannot offer.
type Unavailable struct {
	Capability string // the device-file section: "power"
	Reason     string // a sentence fragment naming what is missing and how to get it
}

// UnavailableError is a factory's answer when the interface its driver needs
// is missing on this machine but could be installed: an optional kernel
// module, as ryzen_smu is for amd-smu. Assemble leaves the capability nil and
// records the reason, where any other factory error fails the device.
type UnavailableError struct{ Reason string }

func (e *UnavailableError) Error() string { return e.Reason }

// Missing returns why the capability is unavailable, or fallback when the
// device data simply does not declare it.
func (d *Device) Missing(capability, fallback string) string {
	if d != nil {
		for _, u := range d.Unavailable {
			if u.Capability == capability {
				return u.Reason
			}
		}
	}
	return fallback
}

// ButtonInfo names one declared hardware button.
type ButtonInfo struct {
	ID    string
	Label string
	// InputPlumber is the capability InputPlumber gives the button, "" when
	// the device data names none (see ButtonConfig.InputPlumber).
	InputPlumber string
	// Node is the node the button's own driver would open now, "" when it is
	// absent or the driver does not read one node. Sysfs only.
	Node func() string
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
	if d.releaseFans == nil {
		return nil
	}
	return d.releaseFans()
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

// ConfigByID returns the embedded device config with the given id, for a
// caller told which device file to use rather than matching DMI (setup's
// --device). The error lists the ids there are.
func ConfigByID(id string) (Config, error) {
	configs, err := Configs()
	if err != nil {
		return Config{}, err
	}
	ids := make([]string, 0, len(configs))
	for _, c := range configs {
		if c.Device.ID == id {
			return c, nil
		}
		ids = append(ids, c.Device.ID)
	}
	return Config{}, fmt.Errorf("device: no device file %q (there are: %s)", id, strings.Join(ids, ", "))
}

// MatchConfig identifies this machine and returns its device config,
// unassembled. It returns ErrUnknownDevice (wrapped with the DMI strings, so a
// bug report contains exactly what a new device file needs) when no device
// file matches.
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

// FallbackID is the device file the daemon and CLI assume when none matches
// this machine. Every earlier z13ctl ran best-effort on any machine as a Z13,
// and the Z13's drivers all fail soft on absent sysfs, so this keeps that.
// voltaire setup deliberately does not use it: granting another machine's
// attributes is not best-effort.
const FallbackID = "asus-rog-flow-z13-2025"

// MatchOrFallback returns this machine's device config, or FallbackID's when
// no device file matches; unmatched is then the match error (it names the DMI
// strings), for the caller to say so. err is a build defect only.
func MatchOrFallback() (c Config, unmatched, err error) {
	c, unmatched = MatchConfig()
	if unmatched == nil {
		return c, nil, nil
	}
	if !errors.Is(unmatched, ErrUnknownDevice) {
		return Config{}, nil, unmatched // the device files themselves are broken
	}
	c, err = ConfigByID(FallbackID)
	return c, unmatched, err
}

// GamingMode reports whether a Steam Gaming Mode (gamescope) session is
// running. A var so the daemon and CLI tests can answer without a session;
// the real answer is internal/session's, wired in by register.
var GamingMode = func() bool { return false }

// Reasons the capabilities a device yields to Steam give on every surface.
const (
	yieldPowerReason  = "Steam sets the power limits in Gaming Mode (its performance overlay); switch to the desktop to set them here"
	yieldChargeReason = "Steam sets the charge limit in Gaming Mode; switch to the desktop to set it here"
)

// YieldToSteam returns c with the capabilities its device data yields to
// Steam in Gaming Mode taken out, and why each one went. Pure: the caller
// decides whether a gamescope session is running.
//
// Only what steamos-manager writes on such a device is yielded — the power
// limits and the charge limit. Fans, profiles, lighting and the rest stay.
func YieldToSteam(c Config) (Config, []Unavailable) {
	var gone []Unavailable
	if c.Power != nil && c.Power.YieldInGamingMode {
		c.Power = nil
		gone = append(gone, Unavailable{Capability: "power", Reason: yieldPowerReason})
	}
	if c.Battery != nil && c.Battery.YieldInGamingMode && c.Battery.Caps().ChargeLimit {
		b := *c.Battery
		off := false
		b.ChargeLimit = &off
		c.Battery = &b
		gone = append(gone, Unavailable{Capability: "charge-limit", Reason: yieldChargeReason})
	}
	return c, gone
}

// AssembleForSession assembles c, first yielding to Steam what the device
// data says to when a Gaming Mode session is running. gamingMode is asked
// only when the device yields anything: answering it costs a system-bus
// connection, which every CLI call on every other machine would pay for
// nothing.
func AssembleForSession(c Config, gamingMode func() bool) (*Device, error) {
	yields := (c.Power != nil && c.Power.YieldInGamingMode) ||
		(c.Battery != nil && c.Battery.YieldInGamingMode)
	inGaming := yields && gamingMode()
	var gone []Unavailable
	if inGaming {
		c, gone = YieldToSteam(c)
	}
	d, err := Assemble(c)
	if err != nil {
		return nil, err
	}
	d.Unavailable = append(d.Unavailable, gone...)
	d.YieldsInGamingMode, d.GamingMode = yields, inGaming
	return d, nil
}

// Assemble builds a Device from a validated config by running each present
// capability block's factory. An unregistered method is an error, not a nil
// capability: the data file names a driver this binary does not carry, and
// silently dropping the capability would misreport the hardware to every
// client.
func Assemble(c Config) (*Device, error) {
	d := &Device{ID: c.Device.ID, Model: c.Device.Model, Name: c.Device.Name}
	var fans driver.FanController // with Release: for the engine and releaseFans only
	if c.Quickbar != nil {
		d.Quickbar = append([]string(nil), c.Quickbar.Controls...)
	}

	if c.Fans != nil {
		f, err := lookup("fans", fansFactories, c.Fans.Method)
		if err != nil {
			return nil, err
		}
		if fans, err = f(*c.Fans); err != nil {
			return nil, fmt.Errorf("fans (%s): %w", c.Fans.Method, err)
		}
		d.Fans, d.releaseFans = fans, fans.Release
	}
	if c.Power != nil {
		f, err := lookup("power", powerFactories, c.Power.Method)
		if err != nil {
			return nil, err
		}
		raw, err := f(*c.Power)
		var missing *UnavailableError
		switch {
		case errors.As(err, &missing):
			d.Unavailable = append(d.Unavailable, Unavailable{Capability: "power", Reason: missing.Reason})
		case err != nil:
			return nil, fmt.Errorf("power (%s): %w", c.Power.Method, err)
		default:
			// The raw limiter goes into the engine and nowhere else.
			d.Power = safety.NewEngine(fans, raw)
			d.PowerSlider = c.Power.Slider
			d.PowerPerSource = c.Power.TDPMaxBattery > 0 || len(c.Power.StockPPTBattery) > 0
		}
	}
	if c.Profiles != nil {
		pc := *c.Profiles
		// The pin holds only while the power control it protects exists: it
		// keeps a firmware profile from capping power below the TDP slider.
		// With the power method unavailable (ryzen_smu not installed on the
		// OXP), the firmware profiles are the only power control left, so
		// every listed one is offered instead.
		if pc.Pin != "" && d.Power != nil {
			d.ProfilePin = pc.Pin
			pc.Names = []string{pc.Pin}
		}
		f, err := lookup("profiles", profilesFactories, pc.Method)
		if err != nil {
			return nil, err
		}
		if d.Profiles, err = f(pc); err != nil {
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
		uv, err := f(*c.Undervolt)
		var missing *UnavailableError
		switch {
		case errors.As(err, &missing):
			d.Unavailable = append(d.Unavailable, Unavailable{Capability: "undervolt", Reason: missing.Reason})
		case err != nil:
			return nil, fmt.Errorf("undervolt (%s): %w", c.Undervolt.Method, err)
		default:
			d.Undervolt = uv
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
	if c.Rumble != nil {
		f, err := lookup("rumble", rumbleFactories, c.Rumble.Method)
		if err != nil {
			return nil, err
		}
		if d.Rumble, err = f(*c.Rumble); err != nil {
			return nil, fmt.Errorf("rumble (%s): %w", c.Rumble.Method, err)
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
	viaInputPlumber := false
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
		info := ButtonInfo{ID: b.ID, Label: b.Label, InputPlumber: b.InputPlumber, Node: func() string { return "" }}
		if n, ok := w.(driver.ButtonNoder); ok {
			info.Node = n.Node
		}
		d.ButtonList = append(d.ButtonList, info)
		if b.InputPlumber != "" {
			viaInputPlumber = true
		}
		if b.Default {
			d.DefaultButton, d.ButtonLabel = b.ID, b.Label
		}
	}
	// One watcher for every button InputPlumber may carry: presses arrive on
	// the system bus however many buttons there are.
	if viaInputPlumber && routeButtons != nil {
		watchers = append(watchers, routeButtons())
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

// routeButtons builds the watcher for presses that arrive through
// InputPlumber, registered by the drivers package (device imports no driver).
var routeButtons func() driver.Buttons

// RegisterInputPlumberButtons sets the factory for the InputPlumber button
// watcher, added to a device whose buttons name an InputPlumber capability.
func RegisterInputPlumberButtons(f func() driver.Buttons) { routeButtons = f }

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
