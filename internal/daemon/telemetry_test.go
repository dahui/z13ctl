package daemon

// telemetry_test.go — the sampler's decision table and the telemetry-history
// handler.
//
// The sampler is the one watcher that writes no hardware, so unlike the
// reconcile and power-source tables these cases can drive sampleOnce itself:
// the worst it can do on the developer's machine is read hwmon. The handler
// cases stay on the ring, which is where the behaviour worth pinning is.

import (
	"testing"
	"time"

	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/telemetryring"
)

func TestTelemetryTick(t *testing.T) {
	cases := []struct {
		name string
		obs  telemetryObs
		want bool
	}{
		{"normal running", telemetryObs{HasSource: true, RingCap: 300}, true},
		{"no telemetry driver", telemetryObs{RingCap: 300}, false},
		{"device keeps no history", telemetryObs{HasSource: true}, false},
		// The gate this test exists for. It is not the powersource lesson —
		// this watcher writes nothing — but a sample taken after the pre-sleep
		// release records the released machine, so the graph would show a
		// thermal cliff seconds before a suspend that explains nothing.
		{"suspending", telemetryObs{Suspending: true, HasSource: true, RingCap: 300}, false},
		{"suspending with nothing to sample anyway", telemetryObs{Suspending: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			act := telemetryTick(tc.obs)
			if act.Sample != tc.want {
				t.Errorf("telemetryTick(%+v).Sample = %v, want %v (reason %q)",
					tc.obs, act.Sample, tc.want, act.Reason)
			}
			if !act.Sample && act.Reason == "" {
				t.Error("a tick that declines to sample must say why")
			}
		})
	}
}

func TestSamplerStandsDownWhileSuspending(t *testing.T) {
	// End to end through sampleOnce rather than the tick alone, because the
	// gate is only worth anything if the watcher consults it — powerSourceOnce
	// was the watcher that had the flag available and did not.
	d := &Daemon{hw: testDev, telemetry: telemetryring.New(10)}

	d.mu.Lock()
	d.suspending = true
	d.mu.Unlock()
	d.sampleOnce()
	if got := d.history().Len(); got != 0 {
		t.Fatalf("sampled %d times while suspending, want 0", got)
	}

	d.mu.Lock()
	d.suspending = false
	d.mu.Unlock()
	d.sampleOnce()
	if got := d.history().Len(); got != 1 {
		t.Errorf("after the resume the ring holds %d samples, want 1", got)
	}
}

func TestHistoryIsNeverNil(t *testing.T) {
	// Every test in this package builds a Daemon as a struct literal, so the
	// ring is nil far more often than not; a nil *Ring would panic inside a
	// socket handler.
	d := &Daemon{hw: testDev}
	if d.history() == nil {
		t.Fatal("history() = nil")
	}
	d.history().Add(time.Now(), driver.Sample{TempC: 50})
	if got := d.history().Len(); got != 0 {
		t.Errorf("the substitute ring retained %d samples, want 0", got)
	}
}

func TestTelemetryHistoryHandler(t *testing.T) {
	ring := telemetryring.New(10)
	// Samples land on half-seconds so that no sample sits *on* a window
	// boundary. The handler reads the real clock — it must — so it is always a
	// few microseconds later than this `now`, and a sample seeded exactly at
	// the cutoff falls in or out depending on scheduling. The inclusive
	// boundary itself is pinned in telemetryring's own tests, against a fixed
	// instant, which is the only place it can be tested without a flake.
	now := time.Now()
	for i := range 6 {
		ring.Add(now.Add(time.Duration(i-5)*time.Second-500*time.Millisecond), driver.Sample{
			TempC: 40 + i,
			RPM:   []int{1000 + i, 2000 + i},
		})
	}
	d := &Daemon{hw: testDev, telemetry: ring}

	t.Run("window selects the newest samples", func(t *testing.T) {
		// -2.5s, -1.5s and -0.5s fall inside a 3s window; -3.5s and older do not.
		resp := d.handleTelemetryHistory(request{Cmd: "telemetry-history", Seconds: 3})
		if !resp.OK {
			t.Fatalf("handler failed: %s", resp.Error)
		}
		if len(resp.History) != 3 {
			t.Fatalf("got %d samples, want 3", len(resp.History))
		}
		if resp.History[0].TempC != 43 || resp.History[2].TempC != 45 {
			t.Errorf("history = %+v, want 43..45 oldest first", resp.History)
		}
		if resp.History[0].At >= resp.History[2].At {
			t.Error("timestamps are not increasing; samples must be oldest first")
		}
	})

	t.Run("no seconds means the whole retained history", func(t *testing.T) {
		// Deliberately different from telemetryring.Since, which refuses a
		// non-positive window: the ring cannot tell "everything" from a failed
		// parse, the protocol can, and every other read command here answers
		// with all it has when given no argument.
		resp := d.handleTelemetryHistory(request{Cmd: "telemetry-history"})
		if !resp.OK || len(resp.History) != 6 {
			t.Errorf("got %d samples, want all 6", len(resp.History))
		}
	})

	t.Run("empty history is a list, not null", func(t *testing.T) {
		empty := &Daemon{hw: testDev, telemetry: telemetryring.New(10)}
		resp := empty.handleTelemetryHistory(request{Cmd: "telemetry-history", Seconds: 60})
		if !resp.OK {
			t.Fatalf("handler failed: %s", resp.Error)
		}
		if resp.History == nil {
			t.Error("History = nil; a daemon with no samples yet has an empty list, not an absent field")
		}
	})

	t.Run("no telemetry capability is an error", func(t *testing.T) {
		bare := &Daemon{}
		resp := bare.handleTelemetryHistory(request{Cmd: "telemetry-history", Seconds: 60})
		if resp.OK {
			t.Error("a device with no telemetry source answered ok")
		}
	})
}

func TestTelemetryHistoryDispatches(t *testing.T) {
	// dispatch routing, so a handler that exists but is unreachable is caught.
	d := &Daemon{hw: testDev, telemetry: telemetryring.New(4)}
	if resp := d.dispatch(request{Cmd: "telemetry-history", Seconds: 5}); !resp.OK {
		t.Errorf("dispatch(telemetry-history) = %+v, want ok", resp)
	}
}

func TestNewTelemetryRingSizesFromDeviceData(t *testing.T) {
	// One number, one place: the ring's capacity and the window the document
	// advertises both come from the device's declared history_seconds.
	want := testDev.Telemetry.Info().HistorySeconds
	if got := newTelemetryRing(testDev).Cap(); got != want {
		t.Errorf("ring capacity = %d, want %d (1 Hz over the declared window)", got, want)
	}
	if got := newTelemetryRing(nil).Cap(); got != 0 {
		t.Errorf("a nil device got a ring of %d, want 0", got)
	}
}
