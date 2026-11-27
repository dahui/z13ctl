package daemon

// fakes_test.go — fakeDevice: the Z13's device data over in-memory drivers,
// for tests of write paths. Every write is logged in order, so a test can
// assert sequences (fans before power when raising), and is checked against
// d.hwMu: a hardware write sequence the daemon does not serialise can
// interleave with the reconcile watcher or a resume.

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"
)

// hwLog records writes, and fails the test for any made without hwMu.
type hwLog struct {
	t      *testing.T
	d      *Daemon // set once the daemon exists
	mu     sync.Mutex
	writes []string
}

func (l *hwLog) write(format string, a ...any) {
	op := fmt.Sprintf(format, a...)
	if l.d != nil && l.d.hwMu.TryLock() {
		l.d.hwMu.Unlock()
		l.t.Errorf("hardware write %q made without hwMu", op)
	}
	l.mu.Lock()
	l.writes = append(l.writes, op)
	l.mu.Unlock()
}

func (l *hwLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.writes)
}

type memFans struct {
	log   *hwLog
	shape driver.FanShape
	mode  driver.FanMode
	curve []api.FanCurvePoint
}

func (f *memFans) Shape() driver.FanShape                  { return f.shape }
func (f *memFans) ReadRPM() ([]int, error)                 { return []int{2000, 2100}, nil }
func (f *memFans) ReadMode() (driver.FanMode, error)       { return f.mode, nil }
func (f *memFans) LiveCurve() ([]api.FanCurvePoint, error) { return slices.Clone(f.curve), nil }
func (f *memFans) ApplyCurve(pts []api.FanCurvePoint) error {
	f.log.write("fans.curve")
	f.curve, f.mode = slices.Clone(pts), driver.FanModeCustom
	return nil
}
func (f *memFans) Release() error {
	f.log.write("fans.release")
	f.mode = driver.FanModeAuto
	return nil
}

type memPower struct {
	log   *hwLog
	env   driver.PowerEnvelope
	state api.TDPState
}

func (p *memPower) Read() (api.TDPState, error)    { return p.state, nil }
func (p *memPower) Envelope() driver.PowerEnvelope { return p.env }
func (p *memPower) Apply(s api.TDPState) error {
	p.log.write("power.apply %d", s.PL1SPL)
	p.state = s
	return nil
}

type memProfiles struct {
	log     *hwLog
	names   []string
	def     string
	current string
}

func (p *memProfiles) Names() []string          { return slices.Clone(p.names) }
func (p *memProfiles) Get() (string, error)     { return p.current, nil }
func (p *memProfiles) Default() string          { return p.def }
func (p *memProfiles) Label(name string) string { return name }
func (p *memProfiles) Set(name string) error {
	p.log.write("profile %s", name)
	p.current = name
	return nil
}

type memToggles struct {
	log    *hwLog
	specs  []driver.ToggleSpec
	values map[string]int
}

func (t *memToggles) List() []driver.ToggleSpec { return slices.Clone(t.specs) }
func (t *memToggles) Get(id string) (int, error) {
	v, ok := t.values[id]
	if !ok {
		return 0, fmt.Errorf("no toggle %q", id)
	}
	return v, nil
}
func (t *memToggles) Set(id string, v int) error {
	t.log.write("toggle %s=%d", id, v)
	t.values[id] = v
	return nil
}

type memBattery struct {
	log    *hwLog
	caps   driver.BatteryCaps
	limit  int
	status *driver.BatteryStatus // nil: 80 % on mains
}

func (b *memBattery) Caps() driver.BatteryCaps  { return b.caps }
func (b *memBattery) ChargeLimit() (int, error) { return b.limit, nil }
func (b *memBattery) SetChargeLimit(p int) error {
	b.log.write("battery.limit %d", p)
	b.limit = p
	return nil
}
func (b *memBattery) Status() (driver.BatteryStatus, error) {
	if b.status != nil {
		return *b.status, nil
	}
	return driver.BatteryStatus{Capacity: 80, OnAC: true, ACKnown: true}, nil
}

type memBoost struct {
	log *hwLog
	on  bool
}

func (b *memBoost) Get() (bool, error) { return b.on, nil }
func (b *memBoost) Set(on bool) error {
	b.log.write("boost %t", on)
	b.on = on
	return nil
}

// fakeDevice is the Z13's device data over in-memory drivers, on firmware
// auto at the balanced profile's stock limits. The returned log records every
// write; attach the daemon with log.d = d to check hwMu.
func fakeDevice(t *testing.T) (*device.Device, *hwLog) {
	t.Helper()
	c, err := device.ConfigByID(device.FallbackID)
	if err != nil {
		t.Fatal(err)
	}
	log := &hwLog{t: t}
	env := c.Power.Envelope()
	fans := &memFans{log: log, shape: c.Fans.Shape(), mode: driver.FanModeAuto}
	power := &memPower{log: log, env: env, state: env.StockProfilePPT["balanced"]}
	values := map[string]int{}
	for _, s := range declaredToggles(c) {
		values[s.ID] = 0
	}
	d := &device.Device{
		ID: c.Device.ID, Model: c.Device.Model, Name: c.Device.Name,
		Fans:     fans,
		Power:    safety.NewEngine(fans, power),
		Profiles: &memProfiles{log: log, names: c.Profiles.Names, def: c.Profiles.Default, current: "balanced"},
		Toggles:  &memToggles{log: log, specs: declaredToggles(c), values: values},
		Battery:  &memBattery{log: log, caps: c.Battery.Caps(), limit: 100},
		CPUBoost: &memBoost{log: log, on: true},
	}
	return d, log
}

// fakeDaemon is a daemon over fakeDevice with its state file in a temporary
// directory, and the log attached so writes are checked against hwMu.
func fakeDaemon(t *testing.T) (*Daemon, *hwLog) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	hw, log := fakeDevice(t)
	d := &Daemon{hw: hw, state: defaultState()}
	log.d = d
	return d, log
}
