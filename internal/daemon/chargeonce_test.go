package daemon

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// chargeOnceRig is a fake daemon with an 80 % limit already set through the
// daemon, and the battery to steer.
func chargeOnceRig(t *testing.T) (*Daemon, *hwLog, *memBattery) {
	t.Helper()
	d, log := fakeDaemon(t)
	bat := d.hw.Battery.(*memBattery)
	if r := d.handleBatteryLimit(request{Set: "80"}); !r.OK {
		t.Fatal(r.Error)
	}
	return d, log, bat
}

func (b *memBattery) at(capacity int, state driver.BatteryState) {
	b.status = &driver.BatteryStatus{Capacity: capacity, State: state, OnAC: true, ACKnown: true}
}

func lastWrite(log *hwLog) string {
	w := log.all()
	if len(w) == 0 {
		return ""
	}
	return w[len(w)-1]
}

// TestChargeOnceLiftsTheLimitUntilFull: the limit goes to the maximum, the
// user's limit stays in state, nothing happens while the pack fills, and the
// limit comes back when it is full.
func TestChargeOnceLiftsTheLimitUntilFull(t *testing.T) {
	d, log, bat := chargeOnceRig(t)
	if r := d.handleChargeOnce(request{Set: "on"}); !r.OK {
		t.Fatal(r.Error)
	}
	if got := lastWrite(log); got != "battery.limit 100" {
		t.Errorf("start wrote %q, want the maximum", got)
	}
	if !d.state.ChargeOnce || d.state.Battery != 80 {
		t.Errorf("state: once %v, limit %d; want true, 80 (the limit to return to)", d.state.ChargeOnce, d.state.Battery)
	}
	n := len(log.all())
	if r := d.handleChargeOnce(request{Set: "on"}); !r.OK || len(log.all()) != n {
		t.Error("starting it again must be a no-op")
	}

	bat.at(95, driver.BatteryStateCharging)
	d.chargeOnceTick(time.Now())
	if len(log.all()) != n {
		t.Errorf("the tick wrote before the pack was full: %v", log.all())
	}

	bat.at(100, driver.BatteryStateFull)
	d.chargeOnceTick(time.Now())
	if got := lastWrite(log); got != "battery.limit 80" || d.state.ChargeOnce {
		t.Errorf("full: wrote %q, once %v; want the 80 %% limit back", got, d.state.ChargeOnce)
	}
}

func TestChargeOnceCancel(t *testing.T) {
	d, log, _ := chargeOnceRig(t)
	if r := d.handleChargeOnce(request{Set: "on"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := d.handleChargeOnce(request{Set: "off"}); !r.OK {
		t.Fatal(r.Error)
	}
	if got := lastWrite(log); got != "battery.limit 80" || d.state.ChargeOnce {
		t.Errorf("cancel: wrote %q, once %v", got, d.state.ChargeOnce)
	}
	n := len(log.all())
	if r := d.handleChargeOnce(request{Set: "off"}); !r.OK || len(log.all()) != n {
		t.Error("cancelling with nothing in progress must be a no-op")
	}
}

// TestNewLimitCancelsChargeOnce: setting a limit ends a one-time charge, and
// the new limit is the one written.
func TestNewLimitCancelsChargeOnce(t *testing.T) {
	d, log, _ := chargeOnceRig(t)
	if r := d.handleChargeOnce(request{Set: "on"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := d.handleBatteryLimit(request{Set: "70"}); !r.OK {
		t.Fatal(r.Error)
	}
	if got := lastWrite(log); got != "battery.limit 70" || d.state.ChargeOnce || d.state.Battery != 70 {
		t.Errorf("wrote %q, once %v, limit %d", got, d.state.ChargeOnce, d.state.Battery)
	}
}

func TestChargeOnceRefusals(t *testing.T) {
	d, log := fakeDaemon(t)
	bat := d.hw.Battery.(*memBattery)

	// No limit anywhere: the battery charges to full already.
	if r := d.handleChargeOnce(request{Set: "on"}); r.OK || !strings.Contains(r.Error, "already charges to full") {
		t.Errorf("no limit: %+v", r)
	}
	if slices.Contains(log.all(), "battery.limit 100") {
		t.Errorf("the no-limit refusal wrote: %v", log.all())
	}
	// A limit another tool set is the one to return to.
	bat.limit = 75
	if r := d.handleChargeOnce(request{Set: "on"}); !r.OK || d.state.Battery != 75 {
		t.Errorf("hardware limit 75: %+v, saved %d", r, d.state.Battery)
	}
	if r := d.handleChargeOnce(request{Set: "off"}); !r.OK {
		t.Fatal(r.Error)
	}

	n := len(log.all())
	bat.at(100, driver.BatteryStateFull)
	if r := d.handleChargeOnce(request{Set: "on"}); r.OK || !strings.Contains(r.Error, "already full") {
		t.Errorf("full pack: %+v", r)
	}
	if r := d.handleChargeOnce(request{Set: "maybe"}); r.OK {
		t.Error("a bad set value was accepted")
	}
	if len(log.all()) != n || d.state.ChargeOnce {
		t.Errorf("a refusal wrote: %v", log.all()[n:])
	}
}

// TestChargeOnceStandsDown: no write while the machine is entering sleep or
// the EC is wedged, and nothing at all while the limit is Steam's.
func TestChargeOnceStandsDown(t *testing.T) {
	d, log, bat := chargeOnceRig(t)
	if r := d.handleChargeOnce(request{Set: "on"}); !r.OK {
		t.Fatal(r.Error)
	}
	bat.at(100, driver.BatteryStateFull)
	n := len(log.all())

	d.setSuspending(true)
	d.chargeOnceTick(time.Now())
	d.setSuspending(false)
	d.setECWedged(true)
	d.chargeOnceTick(time.Now())
	d.setECWedged(false)
	if len(log.all()) != n {
		t.Errorf("wrote while suspending or wedged: %v", log.all()[n:])
	}

	// Gaming Mode: the capability is off and the driver stays.
	bat.caps.ChargeLimit = false
	d.chargeOnceTick(time.Now())
	if r := d.handleChargeOnce(request{Set: "off"}); r.OK {
		t.Error("a cancel was accepted while the limit is Steam's")
	}
	if len(log.all()) != n || !d.state.ChargeOnce {
		t.Errorf("acted while the limit is Steam's: writes %v, once %v", log.all()[n:], d.state.ChargeOnce)
	}

	bat.caps.ChargeLimit = true
	d.chargeOnceTick(time.Now())
	if got := lastWrite(log); got != "battery.limit 80" {
		t.Errorf("back on the desktop: wrote %q, want the limit restored", got)
	}
}

// TestChargeOnceTopsOff: a pack that trickles at 99 % on mains and never
// says it is full still gets the limit back, after chargeOnceTopOff of it;
// dropping below 99 % or unplugging restarts the wait.
func TestChargeOnceTopsOff(t *testing.T) {
	d, log, bat := chargeOnceRig(t)
	if r := d.handleChargeOnce(request{Set: "on"}); !r.OK {
		t.Fatal(r.Error)
	}
	n := len(log.all())
	t0 := time.Unix(1_800_000_000, 0)

	bat.at(99, driver.BatteryStateCharging)
	d.chargeOnceTick(t0)
	d.chargeOnceTick(t0.Add(chargeOnceTopOff - time.Minute))
	if len(log.all()) != n {
		t.Fatalf("ended before the top-off wait: %v", log.all()[n:])
	}

	// A dip below 99 %, then an unplug at 99 %: each restarts the wait.
	bat.at(98, driver.BatteryStateCharging)
	d.chargeOnceTick(t0.Add(chargeOnceTopOff))
	bat.at(99, driver.BatteryStateCharging)
	t1 := t0.Add(chargeOnceTopOff + time.Minute)
	d.chargeOnceTick(t1)
	bat.status.OnAC = false
	d.chargeOnceTick(t1.Add(chargeOnceTopOff))
	if len(log.all()) != n || !d.state.ChargeOnce {
		t.Fatalf("ended across a dip or off mains: %v", log.all()[n:])
	}

	bat.status.OnAC = true
	t2 := t1.Add(chargeOnceTopOff + time.Minute)
	d.chargeOnceTick(t2)
	d.chargeOnceTick(t2.Add(chargeOnceTopOff - time.Second))
	if len(log.all()) != n {
		t.Fatalf("the wait did not restart after the unplug: %v", log.all()[n:])
	}
	d.chargeOnceTick(t2.Add(chargeOnceTopOff))
	if got := lastWrite(log); got != "battery.limit 80" || d.state.ChargeOnce {
		t.Errorf("topped off: wrote %q, once %v; want the 80 %% limit back", got, d.state.ChargeOnce)
	}
}

// TestStartupRestoresTheChargeLimit: the user's limit normally, the maximum
// while a one-time charge is in progress, and nothing while it is Steam's
// (the driver stays in Gaming Mode and would write regardless).
func TestStartupRestoresTheChargeLimit(t *testing.T) {
	d, log := fakeDaemon(t)
	bat := d.hw.Battery.(*memBattery)
	d.hwMu.Lock() // the fake log requires it; at startup nothing else runs
	defer d.hwMu.Unlock()

	d.state.Battery = 80
	d.restoreChargeLimit()
	d.state.ChargeOnce = true
	d.restoreChargeLimit()
	bat.caps.ChargeLimit = false
	d.restoreChargeLimit()
	if got := strings.Join(log.all(), ", "); got != "battery.limit 80, battery.limit 100" {
		t.Errorf("startup writes: %s", got)
	}
}

func TestBatteryFull(t *testing.T) {
	for _, c := range []struct {
		st   driver.BatteryStatus
		want bool
	}{
		{driver.BatteryStatus{Capacity: 100}, true},
		{driver.BatteryStatus{Capacity: 97, State: driver.BatteryStateFull}, true},
		{driver.BatteryStatus{Capacity: 99, State: driver.BatteryStateNotCharging, OnAC: true, ACKnown: true}, true},
		{driver.BatteryStatus{Capacity: 99, State: driver.BatteryStateNotCharging}, false}, // source unknown
		{driver.BatteryStatus{Capacity: 99, State: driver.BatteryStateCharging, OnAC: true, ACKnown: true}, false},
		{driver.BatteryStatus{Capacity: 98, State: driver.BatteryStateNotCharging, OnAC: true, ACKnown: true}, false},
	} {
		if got := batteryFull(c.st); got != c.want {
			t.Errorf("batteryFull(%+v) = %v", c.st, got)
		}
	}
}
