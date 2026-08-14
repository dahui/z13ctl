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

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/telemetryplot"
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

// packagePowerW's table. RAPL publishes a cumulative energy counter, so every
// power figure is a difference — and the failure modes are all about the cases
// where a difference is meaningless: no baseline, a clock that moved backwards,
// a gap across a suspend, and the two ways a counter can go down.
func TestPackagePowerFromTheEnergyCounter(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	const maxUJ = 262_143_328_850

	cases := []struct {
		name  string
		prev  energyReading
		cur   energyReading
		want  float64
		wantK bool
	}{
		{
			name:  "one second at 20 W",
			prev:  energyReading{at: base, uj: 1_000_000, maxUJ: maxUJ},
			cur:   energyReading{at: base.Add(time.Second), uj: 21_000_000, maxUJ: maxUJ},
			want:  20,
			wantK: true,
		},
		{
			name:  "half a second doubles the rate",
			prev:  energyReading{at: base, uj: 0, maxUJ: maxUJ},
			cur:   energyReading{at: base.Add(500 * time.Millisecond), uj: 10_000_000, maxUJ: maxUJ},
			want:  20,
			wantK: true,
		},
		{
			// The counter wrapped: 262 kJ is ~73 minutes at 60 W, so this is
			// rare but real, and reading it as a reset would lose a sample
			// every hour or so.
			name:  "wrap is arithmetic, not a reset",
			prev:  energyReading{at: base, uj: maxUJ - 5_000_000, maxUJ: maxUJ},
			cur:   energyReading{at: base.Add(time.Second), uj: 5_000_000, maxUJ: maxUJ},
			want:  10,
			wantK: true,
		},
		{
			// Indistinguishable from a wrap by the values alone, which is why
			// the sanity ceiling exists: read as a wrap this is ~262 kW.
			name: "a reset is refused, not reported as 262 kW",
			prev: energyReading{at: base, uj: 200_000_000_000, maxUJ: maxUJ},
			cur:  energyReading{at: base.Add(time.Second), uj: 5, maxUJ: maxUJ},
		},
		{
			name: "backwards with no declared range is unknowable",
			prev: energyReading{at: base, uj: 9_000_000},
			cur:  energyReading{at: base.Add(time.Second), uj: 1_000_000},
		},
		{
			name: "no baseline yet",
			cur:  energyReading{at: base, uj: 1_000_000, maxUJ: maxUJ},
		},
		{
			// The ring timestamps with wall clock deliberately (it must survive
			// a suspend), and wall clock can step backwards.
			name: "clock stepped backwards",
			prev: energyReading{at: base.Add(time.Second), uj: 1_000_000, maxUJ: maxUJ},
			cur:  energyReading{at: base, uj: 21_000_000, maxUJ: maxUJ},
		},
		{
			name: "same instant",
			prev: energyReading{at: base, uj: 1_000_000, maxUJ: maxUJ},
			cur:  energyReading{at: base, uj: 21_000_000, maxUJ: maxUJ},
		},
		{
			// Across a suspend the average says nothing about any moment in it,
			// and firmware may have reset the counter as well.
			name: "gap longer than maxEnergyGap",
			prev: energyReading{at: base, uj: 1_000_000, maxUJ: maxUJ},
			cur:  energyReading{at: base.Add(time.Hour), uj: 500_000_000, maxUJ: maxUJ},
		},
		{
			name: "an unreadable counter is not a reading",
			prev: energyReading{at: base, uj: 1_000_000, maxUJ: maxUJ},
			cur:  energyReading{at: base.Add(time.Second), uj: 0, maxUJ: maxUJ},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := packagePowerW(tc.prev, tc.cur)
			if ok != tc.wantK {
				t.Fatalf("ok = %v, want %v (got %v W)", ok, tc.wantK, got)
			}
			if ok && got != tc.want {
				t.Errorf("= %v W, want %v W", got, tc.want)
			}
			if !ok && got != 0 {
				t.Errorf("= %v W with ok=false; a refused conversion must report nothing, "+
					"and 0 W is a claim the package drew nothing", got)
			}
		})
	}
}

// TestRefusedConversionIsNotZeroWatts states the rule the table's !ok cases
// depend on, because it is the one a future caller is most likely to get wrong:
// zero is a measurement, and a running machine never draws it. The sampler must
// leave PackagePowerW unset rather than record the refusal as a reading.
func TestRefusedConversionIsNotZeroWatts(t *testing.T) {
	t.Parallel()

	// A first sample has no baseline, which is the commonest refusal.
	watts, ok := packagePowerW(energyReading{}, energyReading{
		at: time.Unix(1_700_000_000, 0), uj: 1_000_000, maxUJ: 262_143_328_850,
	})
	if ok {
		t.Fatalf("a first sample yielded %v W; there is nothing to difference it against", watts)
	}
}

// TestEnergyGapMatchesThePlotsGapThreshold ties the two constants together.
// A conversion that produced a figure across an interval the dashboard already
// draws as a break would put a line segment where the chart says there is no
// data.
func TestEnergyGapMatchesThePlotsGapThreshold(t *testing.T) {
	t.Parallel()
	if maxEnergyGap != telemetryplot.DefaultMaxGap {
		t.Errorf("maxEnergyGap = %v but telemetryplot.DefaultMaxGap = %v; "+
			"a power figure would span an interval the chart breaks the line across",
			maxEnergyGap, telemetryplot.DefaultMaxGap)
	}
}

// TestApplyLiveTelemetryCarriesEveryMeasuredQuantity is the regression guard for
// the gap this closed: Sample() reported temperature, both fans, the energy
// counter and battery flow, and get-state published the temperature and one fan.
// The dashboard charted four series while the readouts beside it showed two.
func TestApplyLiveTelemetryCarriesEveryMeasuredQuantity(t *testing.T) {
	t.Parallel()

	d := &Daemon{telemetry: telemetryring.New(8)}
	now := time.Now()
	// The sampler's most recent figure, which is where a counter device's
	// package power has to come from.
	d.telemetry.Add(now, driver.Sample{PackagePowerW: 42.5})

	var st api.State
	d.applyLiveTelemetry(&st, driver.Sample{
		TempC:             61,
		RPM:               []int{2400, 2600},
		PackageEnergyUJ:   1_000_000,
		BatteryPowerW:     -18.5,
		BatteryPowerKnown: true,
	})

	if st.Temperature != 61 {
		t.Errorf("Temperature = %d, want 61", st.Temperature)
	}
	if len(st.RPM) != 2 || st.RPM[0] != 2400 || st.RPM[1] != 2600 {
		t.Errorf("RPM = %v, want [2400 2600]; both fans cool the same die and a "+
			"reader quoting one of them describes neither", st.RPM)
	}
	if st.FanRPM != 2400 {
		t.Errorf("FanRPM = %d, want 2400 — it is RPM[0] and every pre-2.0 client reads it", st.FanRPM)
	}
	if st.PackagePowerW != 42.5 {
		t.Errorf("PackagePowerW = %v, want the sampler's 42.5", st.PackagePowerW)
	}
	if st.BatteryPowerW == nil || *st.BatteryPowerW != -18.5 {
		t.Errorf("BatteryPowerW = %v, want -18.5 (charging)", st.BatteryPowerW)
	}
}

// TestLiveBatteryZeroIsAReading is the pointer's whole reason for existing, one
// layer down from telemetryplot's version of the same case: a full pack on mains
// moves no energy, and that must not read as "this machine has no battery".
func TestLiveBatteryZeroIsAReading(t *testing.T) {
	t.Parallel()

	d := &Daemon{telemetry: telemetryring.New(8)}

	var known api.State
	d.applyLiveTelemetry(&known, driver.Sample{BatteryPowerW: 0, BatteryPowerKnown: true})
	if known.BatteryPowerW == nil {
		t.Error("a measured 0 W was dropped; zero is a reading here, not an absence")
	} else if *known.BatteryPowerW != 0 {
		t.Errorf("BatteryPowerW = %v, want 0", *known.BatteryPowerW)
	}

	var absent api.State
	d.applyLiveTelemetry(&absent, driver.Sample{})
	if absent.BatteryPowerW != nil {
		t.Errorf("BatteryPowerW = %v on a device reporting none, want nil", *absent.BatteryPowerW)
	}
}

// TestLivePackagePowerPrefersAnInstantaneousReading: a device whose hardware
// reports watts directly (OXP's pm-table) needs no history at all, and must not
// be served a stale figure from a ring it never filled.
func TestLivePackagePowerPrefersAnInstantaneousReading(t *testing.T) {
	t.Parallel()

	d := &Daemon{telemetry: telemetryring.New(8)}
	d.telemetry.Add(time.Now(), driver.Sample{PackagePowerW: 9})

	var st api.State
	d.applyLiveTelemetry(&st, driver.Sample{PackagePowerW: 31})
	if st.PackagePowerW != 31 {
		t.Errorf("PackagePowerW = %v, want the sample's own 31", st.PackagePowerW)
	}
}

// TestLivePackagePowerIsAbsentWhenStale covers the two ways there is no
// trustworthy current figure. Serving either as "now" is worse than serving
// nothing: a number labelled current that describes a moment before a suspend
// is a wrong reading, where an absent one is an honest gap.
func TestLivePackagePowerIsAbsentWhenStale(t *testing.T) {
	t.Parallel()

	counter := driver.Sample{PackageEnergyUJ: 1_000_000}

	t.Run("nothing sampled yet", func(t *testing.T) {
		t.Parallel()
		d := &Daemon{telemetry: telemetryring.New(8)}
		var st api.State
		d.applyLiveTelemetry(&st, counter)
		if st.PackagePowerW != 0 {
			t.Errorf("PackagePowerW = %v with an empty ring, want absent", st.PackagePowerW)
		}
	})

	t.Run("older than liveTelemetryMaxAge", func(t *testing.T) {
		t.Parallel()
		d := &Daemon{telemetry: telemetryring.New(8)}
		d.telemetry.Add(time.Now().Add(-time.Hour), driver.Sample{PackagePowerW: 42.5})
		var st api.State
		d.applyLiveTelemetry(&st, counter)
		if st.PackagePowerW != 0 {
			t.Errorf("PackagePowerW = %v from an hour-old sample, want absent", st.PackagePowerW)
		}
	})
}

// TestFreshEnoughBound covers the staleness window itself.
//
// It does **not** cover the monotonic-versus-wall-clock hazard freshEnough's
// doc comment is mostly about, and no test can: the two readings diverge only
// across a real suspend or a clock step, and nothing in-process separates them
// — time.Now().Add(-10*time.Hour) moves both together. That was verified by
// deleting the safeguard and watching an earlier version of this test pass
// anyway. The protection there is structural (Unix seconds have no monotonic
// path), not this table, and saying so here is the point: a future reader must
// not take a green run as evidence the hazard is covered.
func TestFreshEnoughBound(t *testing.T) {
	t.Parallel()

	// Wall-clock-only times, which is the form freshEnough compares in.
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"the current tick", now, true},
		{"one tick ago", now.Add(-telemetrySampleInterval), true},
		{"exactly at the bound", now.Add(-liveTelemetryMaxAge), true},
		{"one second past the bound", now.Add(-liveTelemetryMaxAge - time.Second), false},
		{"from before a suspend", now.Add(-10 * time.Hour), false},
		// A clock step forward puts a sample in the future. That says nothing
		// about how old the reading is, so it is refused rather than guessed at.
		{"timestamped in the future", now.Add(time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := freshEnough(now, tc.at, liveTelemetryMaxAge); got != tc.want {
				t.Errorf("freshEnough = %v, want %v", got, tc.want)
			}
		})
	}
}
