package daemon

// writepaths_test.go — the daemon's write paths over fakeDevice: what is
// written, in what order, and under hwMu.

import (
	"errors"
	"slices"
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

// TestTestDevCannotWrite: testDev's drivers refuse every write, so a test
// that gets past validation on it cannot reach the developer's hardware.
func TestTestDevCannotWrite(t *testing.T) {
	for name, err := range map[string]error{
		"profile":      testDev.Profiles.Set("quiet"),
		"fan curve":    testDev.Fans.ApplyCurve(nil),
		"fan release":  testDev.ReleaseFans(nil),
		"power":        testDev.Power.ApplyTDPSafely(api.TDPState{PL1SPL: 30, PL2SPPT: 30, FPPT: 30}, nil),
		"toggle":       testDev.Toggles.Set("boot_sound", 1),
		"charge limit": testDev.Battery.SetChargeLimit(80),
		"boost":        testDev.CPUBoost.Set(false),
		"undervolt":    testDev.Undervolt.Apply(-5),
		"lighting":     testDev.Lighting.Off("keyboard"),
	} {
		if !errors.Is(err, errTestWrite) {
			t.Errorf("%s: testDev wrote (err %v)", name, err)
		}
	}
}

// TestEveryECWriteHoldsHwMu: a single write is still a write into the EC, and
// without hwMu it can land in the middle of the reconcile watcher's or a
// resume's sequence. The log fails the test for any write made without it.
func TestEveryECWriteHoldsHwMu(t *testing.T) {
	for _, req := range []request{
		{Cmd: "batterylimit", Set: "80"},
		{Cmd: "feature", ID: "boot_sound", Set: "1"},
		{Cmd: "bootsound", Set: "1"},
		{Cmd: "paneloverdrive", Set: "1"},
		{Cmd: "cpuboost", Set: "0"},
		{Cmd: "profile", Set: "quiet"},
		{Cmd: "tdp", Set: "40"},
	} {
		t.Run(req.Cmd, func(t *testing.T) {
			d, log := fakeDaemon(t)
			if resp := d.dispatch(req); !resp.OK {
				t.Fatalf("%s: %s", req.Cmd, resp.Error)
			}
			if len(log.all()) == 0 {
				t.Errorf("%s wrote nothing", req.Cmd)
			}
		})
	}
}

// TestHighTDPWritesTheFloorFirst: above the safe maximum the fans get their
// floor before the power limit rises, through the handler, not just in the
// engine's own tests.
func TestHighTDPWritesTheFloorFirst(t *testing.T) {
	d, log := fakeDaemon(t)
	if resp := d.dispatch(request{Cmd: "tdp", Set: "85", Force: true}); !resp.OK {
		t.Fatalf("tdp 85: %s", resp.Error)
	}
	w := log.all()
	curve, power := slices.Index(w, "fans.curve"), slices.Index(w, "power.apply 85")
	if curve < 0 || power < 0 || curve > power {
		t.Errorf("writes %v: want the fan floor before the 85 W limit", w)
	}
}
