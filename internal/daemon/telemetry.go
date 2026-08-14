package daemon

// telemetry.go — the 1 Hz sampler that fills the history ring, and the
// telemetry-history command that reads it back.
//
// The sampler is the daemon's fourth watcher, and it is the only one that
// writes no hardware: it reads what driver.Telemetry reports and appends it to
// a bounded ring. That makes its stand-down rule *different* from the others'
// even though it looks the same, and the difference is worth stating rather
// than inheriting by analogy — see telemetryTick.

import (
	"context"
	"log/slog"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/telemetryring"
)

// telemetrySampleInterval is the sampler's period. 1 Hz is what the dashboard
// graphs were specified against, and it is also what makes a ring capacity in
// samples equal a window in seconds under normal running — a correspondence
// nothing may rely on, since the sampler stands down and the ring filters on
// timestamps for exactly that reason.
const telemetrySampleInterval = time.Second

// telemetryObs is one tick's view of the world.
type telemetryObs struct {
	Suspending bool // between PrepareForSleep(true) and the resume
	HasSource  bool // the device has a telemetry driver
	RingCap    int  // how many samples the ring retains
}

// telemetryAction is what a tick decided.
type telemetryAction struct {
	Sample bool
	Reason string
}

// telemetryTick decides whether to take a sample. Pure, so the table in
// telemetry_test.go covers the stand-down without a clock or a machine.
//
// The suspend gate is here for a **different reason than the other watchers'**,
// and reading it as "same lesson as powersource" would be wrong in a way that
// matters if anyone ever relaxes it. reconcileTick and powerTick stand down
// because they *write hardware*, and a write landing in the window between
// PrepareForSleep(true) and the freeze undoes the fan release that lets the EC
// stop the fans overnight. This watcher writes nothing, so it cannot do that
// harm. It stands down because the samples would be *misleading*: the pre-sleep
// release has already lowered the PPT and handed the fans back to firmware
// auto, so a sample taken there records the released machine and the graph
// shows a thermal cliff the user never asked for, seconds before a suspend that
// explains nothing. Cosmetic, not a safety property — but the graph is the
// whole feature.
//
// It needs no staleness ceiling for the same reason. reconcileTick counts
// awake ticks against reconcileSuspendMaxTicks because a lost
// PrepareForSleep(false) would otherwise leave the fans undefended forever;
// here a lost resume signal costs a gap in a graph until the next suspend
// clears the flag, and re-arming on a guess would put samples back exactly
// where they are least trustworthy.
func telemetryTick(obs telemetryObs) telemetryAction {
	switch {
	case !obs.HasSource:
		return telemetryAction{Reason: "device has no telemetry source"}
	case obs.RingCap <= 0:
		return telemetryAction{Reason: "no history retained for this device"}
	case obs.Suspending:
		return telemetryAction{Reason: "suspending"}
	}
	return telemetryAction{Sample: true}
}

// watchTelemetry runs until ctx is done, appending one sample per tick.
func (d *Daemon) watchTelemetry(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(telemetrySampleInterval):
		}
		d.sampleOnce()
	}
}

// sampleOnce performs one observe-decide-apply cycle.
//
// energyReading is one package-energy counter sample: the value, when it was
// taken, and what it wraps at.
type energyReading struct {
	at    time.Time
	uj    uint64
	maxUJ uint64
}

// maxEnergyGap is the longest interval two readings may span and still yield a
// power figure. Beyond it the average says nothing useful about any moment in
// between, and the likely cause is a suspend — during which the sampler stands
// down and the counter may also have been reset by firmware. It matches
// telemetryplot.DefaultMaxGap for the same reason that value was chosen: four
// consecutive misses is a stall worth noticing, one is noise.
const maxEnergyGap = 5 * time.Second

// implausiblePackageW discriminates a counter *wrap* from a counter *reset*.
// It is not a measurement bound — no judgement is made about whether a laptop
// can really draw 900 W — it exists because the two are indistinguishable from
// the values alone: both present as cur < prev. Read as a wrap, a reset
// produces (max - prev + cur), which on this hardware is ~262 kJ over one
// second, or 262 kW. Anything in that territory is arithmetic, not power.
const implausiblePackageW = 1000

// packagePowerW converts two consecutive energy-counter readings into the
// average power over the interval between them.
//
// ok is false whenever no trustworthy figure can be derived, and the caller
// must then record *no* power rather than zero — 0 W is a claim the package
// drew nothing, which is never true of a running machine. The cases: no
// previous reading (the first sample after start or after a re-baseline), a
// non-positive interval (the ring timestamps with wall clock, which can step
// backwards), an interval longer than maxEnergyGap, and a counter that went
// backwards in a way no wrap explains.
func packagePowerW(prev, cur energyReading) (watts float64, ok bool) {
	if prev.at.IsZero() || cur.uj == 0 {
		return 0, false
	}
	dt := cur.at.Sub(prev.at)
	if dt <= 0 || dt > maxEnergyGap {
		return 0, false
	}

	delta := cur.uj - prev.uj
	if cur.uj < prev.uj {
		// Wrap or reset. Without a declared range there is nothing to add, so
		// it can only be treated as a reset.
		if cur.maxUJ == 0 || prev.uj > cur.maxUJ {
			return 0, false
		}
		delta = cur.maxUJ - prev.uj + cur.uj
	}

	watts = float64(delta) / 1e6 / dt.Seconds()
	if watts > implausiblePackageW {
		return 0, false
	}
	return watts, true
}

// It deliberately takes neither hwMu nor d.mu for the read itself: sampling is
// the same class of hardware access as a *-get handler, and blocking the
// dashboard's data behind a fan write sequence would be the regression those
// handlers avoid by the same reasoning. The ring carries its own lock.
func (d *Daemon) sampleOnce() {
	d.mu.Lock()
	suspending := d.suspending
	d.mu.Unlock()

	act := telemetryTick(telemetryObs{
		Suspending: suspending,
		HasSource:  d.hw != nil && d.hw.Telemetry != nil,
		RingCap:    d.history().Cap(),
	})
	if !act.Sample {
		return
	}

	s, err := d.hw.Telemetry.Sample()
	if err != nil {
		// A failed read is a gap, not a zero. Recording zeros would draw a
		// temperature of 0°C, which reads as a measurement rather than as
		// missing data; the timestamps already let a client see the gap.
		slog.Debug("telemetry sample failed", "err", err)
		return
	}

	now := time.Now()
	// Energy counter to power. This is the sampler's job rather than the
	// driver's because it needs the previous reading, and Sample is also
	// called by every get-state handler — a driver remembering one would be
	// mutable state shared across concurrent callers. Here there is exactly
	// one goroutine, so prevEnergy needs no lock.
	if s.PackageEnergyUJ != 0 {
		cur := energyReading{at: now, uj: s.PackageEnergyUJ, maxUJ: s.PackageEnergyMaxUJ}
		if watts, ok := packagePowerW(d.prevEnergy, cur); ok {
			s.PackagePowerW = watts
		}
		// Re-baselined even when the conversion was refused, so one bad
		// interval costs one sample instead of every sample after it.
		d.prevEnergy = cur
	}

	d.history().Add(now, s)
}

// handleTelemetryHistory answers with the samples taken within the requested
// window, oldest first.
//
// A missing or non-positive "seconds" means the whole retained window, which
// is deliberately *not* what telemetryring.Since does with the same value. The
// ring cannot tell a caller that meant "everything" from one that failed to
// parse its own field, so it refuses; the protocol can, because every other
// read command here answers with everything it has when given no argument, and
// a telemetry-history that returned nothing for the obvious hand-typed request
// would be the surprising one.
func (d *Daemon) handleTelemetryHistory(req request) response {
	if d.hw == nil || d.hw.Telemetry == nil {
		return response{OK: false, Error: "telemetry-history: no telemetry source on this device"}
	}

	window := time.Duration(req.Seconds) * time.Second
	if req.Seconds <= 0 {
		window = time.Duration(d.history().Cap()) * telemetrySampleInterval
	}

	entries := d.history().Since(time.Now(), window)
	out := make([]api.TelemetrySample, 0, len(entries))
	for _, e := range entries {
		s := api.TelemetrySample{
			At:            e.At.Unix(),
			TempC:         e.Sample.TempC,
			RPM:           e.Sample.RPM,
			PackagePowerW: e.Sample.PackagePowerW,
		}
		if e.Sample.BatteryPowerKnown {
			// A fresh variable per sample: taking the address of the loop's
			// own copy would give every entry the same pointer.
			watts := e.Sample.BatteryPowerW
			s.BatteryPowerW = &watts
		}
		out = append(out, s)
	}
	// Non-nil so the wire carries [] rather than null for an empty history: a
	// daemon that has been up for less than a second has no samples yet, and
	// that is an empty list, not an absent field.
	return response{OK: true, History: out}
}

// newTelemetryRing sizes the history ring from the device's declared window.
// A device with no telemetry source, or one that declares no history, gets a
// zero-capacity ring rather than a nil pointer.
func newTelemetryRing(hw *device.Device) *telemetryring.Ring {
	if hw == nil || hw.Telemetry == nil {
		return telemetryring.New(0)
	}
	secs := hw.Telemetry.Info().HistorySeconds
	return telemetryring.New(int(time.Duration(secs) * time.Second / telemetrySampleInterval))
}

// emptyTelemetryRing backs history() for a Daemon that never had a ring
// assigned — every test builds one as a struct literal, and Run is the only
// place that sizes one. It retains nothing, so sharing a single instance is
// safe: Add is a no-op and every read answers empty, under its own lock.
var emptyTelemetryRing = telemetryring.New(0)

// history returns the sample ring, never nil. Without this every reader would
// carry a nil check whose failure mode is a panic in a socket handler.
func (d *Daemon) history() *telemetryring.Ring {
	if d.telemetry == nil {
		return emptyTelemetryRing
	}
	return d.telemetry
}
