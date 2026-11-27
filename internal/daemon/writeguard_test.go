package daemon

// writeguard_test.go — testDev's drivers cannot write. testDev is the real
// Z13 assembly, because the device-document tests need what the real drivers
// describe; every method that would change hardware is replaced here with one
// that fails, so a test that gets past validation gets an error instead of
// writing the developer's machine. (Power needs no guard: testDev's engine runs
// over deviceFileEnvelope, which refuses writes itself.) That used to be a rule each test had to
// keep ("stay on rejection paths"); it is now a property of testDev. A test
// that needs a write to succeed uses fakeDevice (fakes_test.go).

import (
	"errors"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// errTestWrite is what a guarded driver returns instead of writing.
var errTestWrite = errors.New("test reached a hardware write on testDev (use fakeDevice for write paths)")

type guardFans struct{ driver.FanCurves }

func (guardFans) ApplyCurve([]api.FanCurvePoint) error { return errTestWrite }
func (guardFans) Release() error                       { return errTestWrite }

type guardProfiles struct{ driver.ProfileController }

func (guardProfiles) Set(string) error { return errTestWrite }

type guardToggles struct{ driver.Toggles }

func (guardToggles) Set(string, int) error { return errTestWrite }

type guardBattery struct{ driver.Battery }

func (guardBattery) SetChargeLimit(int) error { return errTestWrite }

type guardUndervolter struct{ driver.Undervolter }

func (guardUndervolter) Apply(int) error { return errTestWrite }
func (guardUndervolter) Reset() error    { return errTestWrite }

// ProbeAvailable is a Curve Optimizer write on the real driver (the probe
// sends offset 0), so it is guarded too.
func (guardUndervolter) ProbeAvailable() bool { return false }

type guardBoost struct{ driver.CPUBoost }

func (guardBoost) Set(bool) error { return errTestWrite }

type guardRumble struct{ driver.Rumble }

func (guardRumble) Set(int) error { return errTestWrite }

type guardLighting struct{ driver.Lighting }

func (guardLighting) Apply(string, api.LightingState) error { return errTestWrite }
func (guardLighting) Off(string) error                      { return errTestWrite }
func (guardLighting) SetBrightness(string, int) error       { return errTestWrite }
func (guardLighting) Reopen() error                         { return errTestWrite }

// The guards forward the optional interfaces the real drivers implement, so
// wrapping a driver does not change what the daemon finds when it asks. None
// of these writes hardware: they describe it, or drop a cache.

func (g guardProfiles) PolicyWritePaths() []string {
	if n, ok := g.ProfileController.(driver.PolicyWriteNotifier); ok {
		return n.PolicyWritePaths()
	}
	return nil
}

func (g guardToggles) PendingReboot() (bool, error) {
	if r, ok := g.Toggles.(driver.RebootPending); ok {
		return r.PendingReboot()
	}
	return false, errors.New("no pending_reboot")
}

func (g guardUndervolter) Text() driver.UndervoltText {
	if d, ok := g.Undervolter.(driver.UndervoltDescriber); ok {
		return d.Text()
	}
	return driver.UndervoltText{}
}

func (g guardUndervolter) Command() (string, error) {
	if c, ok := g.Undervolter.(driver.UndervoltCommander); ok {
		return c.Command()
	}
	return "", errors.New("no command")
}
