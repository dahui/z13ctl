package device

// registry.go — the method-keyed factory registries. A capability block's
// method string selects a compiled-in factory; drivers register themselves
// from init(), so adding a driver is one import and adding a device is one
// data file naming methods that already exist.

import (
	"fmt"

	"github.com/dahui/z13ctl/internal/driver"
)

// Factory function types, one per hardware class. Each receives its capability
// config so a driver can be parameterized by device data (hwmon names, input
// device names, keycodes) instead of compiling in per-device constants.
type (
	// FansFactory builds a fan controller from its config block.
	FansFactory func(FansConfig) (driver.FanController, error)
	// PowerFactory builds a raw power limiter; Assemble wraps it in the engine.
	PowerFactory func(PowerConfig) (driver.PowerLimiter, error)
	// ProfilesFactory builds a platform-profile controller.
	ProfilesFactory func(ProfilesConfig) (driver.ProfileController, error)
	// LightingFactory builds a lighting driver.
	LightingFactory func(LightingConfig) (driver.Lighting, error)
	// TogglesFactory builds a firmware-toggles driver.
	TogglesFactory func(TogglesConfig) (driver.Toggles, error)
	// BatteryFactory builds a battery driver.
	BatteryFactory func(BatteryConfig) (driver.Battery, error)
	// UndervoltFactory builds an undervolter.
	UndervoltFactory func(UndervoltConfig) (driver.Undervolter, error)
	// TelemetryFactory builds a telemetry source.
	TelemetryFactory func(TelemetryConfig) (driver.Telemetry, error)
	// ButtonsFactory builds a hardware-buttons watcher.
	ButtonsFactory func(ButtonConfig) (driver.Buttons, error)
)

// The registries. Written only from init() functions (RegisterX panics on a
// duplicate method — that is a programmer error, not a runtime condition), so
// no locking: by the time Assemble reads them, all writes have happened.
var (
	fansFactories      = map[string]FansFactory{}
	powerFactories     = map[string]PowerFactory{}
	profilesFactories  = map[string]ProfilesFactory{}
	lightingFactories  = map[string]LightingFactory{}
	togglesFactories   = map[string]TogglesFactory{}
	batteryFactories   = map[string]BatteryFactory{}
	undervoltFactories = map[string]UndervoltFactory{}
	telemetryFactories = map[string]TelemetryFactory{}
	buttonsFactories   = map[string]ButtonsFactory{}
)

func register[F any](kind string, m map[string]F, method string, f F) {
	if method == "" {
		panic(kind + ": empty method")
	}
	if _, dup := m[method]; dup {
		panic(kind + ": duplicate method " + method)
	}
	m[method] = f
}

// RegisterFans registers a fan-controller factory under a method name.
func RegisterFans(method string, f FansFactory) { register("fans", fansFactories, method, f) }

// RegisterPower registers a power-limiter factory under a method name.
func RegisterPower(method string, f PowerFactory) { register("power", powerFactories, method, f) }

// RegisterProfiles registers a profile-controller factory under a method name.
func RegisterProfiles(method string, f ProfilesFactory) {
	register("profiles", profilesFactories, method, f)
}

// RegisterLighting registers a lighting factory under a method name.
func RegisterLighting(method string, f LightingFactory) {
	register("lighting", lightingFactories, method, f)
}

// RegisterToggles registers a firmware-toggles factory under a method name.
func RegisterToggles(method string, f TogglesFactory) {
	register("toggles", togglesFactories, method, f)
}

// RegisterBattery registers a battery factory under a method name.
func RegisterBattery(method string, f BatteryFactory) {
	register("battery", batteryFactories, method, f)
}

// RegisterUndervolt registers an undervolter factory under a method name.
func RegisterUndervolt(method string, f UndervoltFactory) {
	register("undervolt", undervoltFactories, method, f)
}

// RegisterTelemetry registers a telemetry factory under a method name.
func RegisterTelemetry(method string, f TelemetryFactory) {
	register("telemetry", telemetryFactories, method, f)
}

// RegisterButtons registers a hardware-buttons factory under a method name.
func RegisterButtons(method string, f ButtonsFactory) {
	register("buttons", buttonsFactories, method, f)
}

// lookup returns the factory for a method or a uniform "who forgot what"
// error: a validated config naming an unregistered method means the binary was
// built without the driver the data file expects, which is a build/packaging
// defect worth naming precisely.
func lookup[F any](kind string, m map[string]F, method string) (F, error) {
	f, ok := m[method]
	if !ok {
		return f, fmt.Errorf("%s method %q is not compiled into this binary", kind, method)
	}
	return f, nil
}
