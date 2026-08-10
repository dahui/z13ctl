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
	d.history().Add(time.Now(), s)
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
		out = append(out, api.TelemetrySample{
			At:            e.At.Unix(),
			TempC:         e.Sample.TempC,
			RPM:           e.Sample.RPM,
			PackagePowerW: e.Sample.PackagePowerW,
		})
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
