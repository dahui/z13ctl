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
	"github.com/dahui/voltaire/v2/internal/driver"
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
	// Jiffie counters to utilisation — the same rate-needs-two-readings shape
	// as the energy counter, owned by this goroutine for the same reason.
	if s.CPUTotalJiffies != 0 {
		cur := jiffieReading{busy: s.CPUBusyJiffies, total: s.CPUTotalJiffies}
		if pct, ok := cpuUtilPct(d.prevJiffies, cur); ok {
			s.CPUUtilPct, s.CPUUtilKnown = pct, true
		}
		d.prevJiffies = cur
	}
	// Network byte counters to throughput — the energy counter's shape again,
	// wall-clock interval and all.
	if s.NetRxBytes != 0 || s.NetTxBytes != 0 {
		cur := netReading{at: now, rx: s.NetRxBytes, tx: s.NetTxBytes}
		if rx, tx, ok := netRateMBps(d.prevNet, cur); ok {
			s.NetRxMBps, s.NetTxMBps, s.NetRateKnown = rx, tx, true
		}
		d.prevNet = cur
	}

	d.history().Add(now, s)
}

// jiffieReading is one CPU jiffie-counter sample: cumulative busy and total
// jiffies since boot.
type jiffieReading struct {
	busy, total uint64
}

// cpuUtilPct converts two consecutive jiffie readings into the utilisation
// percentage over the interval between them.
//
// Unlike packagePowerW it needs no wall-clock interval — the ratio of the two
// deltas is the percentage whatever the elapsed time — and no gap ceiling: the
// counters only advance while the machine is awake, so a delta spanning a
// suspend still averages only awake time. ok is false with no previous
// reading, on a counter that went backwards (a reboot mid-daemon is the only
// cause, but the arithmetic must not wrap), and on a zero total delta.
func cpuUtilPct(prev, cur jiffieReading) (pct int, ok bool) {
	if prev.total == 0 || cur.total <= prev.total || cur.busy < prev.busy {
		return 0, false
	}
	pct = int((cur.busy - prev.busy) * 100 / (cur.total - prev.total))
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// netReading is one network byte-counter sample: cumulative received and
// transmitted bytes, and when they were read.
type netReading struct {
	at     time.Time
	rx, tx uint64
}

// netRateMBps converts two consecutive byte-counter readings into throughput
// in decimal megabytes per second over the interval between them.
//
// packagePowerW's refusals, for packagePowerW's reasons: no previous reading,
// a non-positive or over-long wall-clock interval (the same maxEnergyGap — a
// suspend-spanning average describes no moment inside it, and the awake-only
// counters divided by a wall-clock interval would understate it anyway), and
// a counter that went backwards, which here means an interface disappeared
// from the sum (an unplugged dock, a downed wlan) rather than a wrap —
// /proc/net/dev is 64-bit, so a wrap is decades away. ok false means record
// no rate, never zero: 0.0 MB/s is an idle link, a real reading.
func netRateMBps(prev, cur netReading) (rx, tx float64, ok bool) {
	if prev.at.IsZero() {
		return 0, 0, false
	}
	dt := cur.at.Sub(prev.at)
	if dt <= 0 || dt > maxEnergyGap {
		return 0, 0, false
	}
	if cur.rx < prev.rx || cur.tx < prev.tx {
		return 0, 0, false
	}
	secs := dt.Seconds()
	return float64(cur.rx-prev.rx) / 1e6 / secs, float64(cur.tx-prev.tx) / 1e6 / secs, true
}

// liveTelemetryMaxAge bounds how stale the sampler's most recent derived figure
// may be before get-state omits it instead of serving it as the current value.
//
// Three ticks: one miss is noise, and the sampler needs one tick after a resume
// before it has anything to say. Beyond that the likely causes are a sampler
// standing down across a suspend and a telemetry source that has started
// failing — and in both, a number labelled "now" that describes some other
// moment is worse than no number.
const liveTelemetryMaxAge = 3 * telemetrySampleInterval

// applyLiveTelemetry fills the live telemetry fields of a get-state reply from
// one fresh sample, plus — for package power alone — the sampler's history.
//
// Everything but package power is instantaneous and comes straight from the
// sample. Package power is a *rate*, and on any device whose hardware offers a
// cumulative energy counter (powercap RAPL, which is what x86 offers) a single
// reading cannot become one: it needs a previous reading and the interval since.
// This handler has neither, and must not acquire them — d.prevEnergy is owned by
// the sampler's goroutine and unguarded precisely because that goroutine is the
// only writer, so re-baselining it from a socket handler would be a data race
// *and* would corrupt the series the dashboard is drawing. Serving the sampler's
// own most recent figure is also the only way the header and the right-hand edge
// of the chart beside it can agree, and two numbers for one quantity that
// disagree is indistinguishable from a bug.
func (d *Daemon) applyLiveTelemetry(s *api.State, sample driver.Sample) {
	s.Temperature = sample.TempC
	if len(sample.RPM) > 0 {
		s.RPM = append([]int(nil), sample.RPM...)
		s.FanRPM = sample.RPM[0]
	}
	if sample.BatteryPowerKnown {
		watts := sample.BatteryPowerW
		s.BatteryPowerW = &watts
	}

	now := time.Now()
	latest, haveLatest := d.history().Latest()
	haveLatest = haveLatest && freshEnough(now, latest.At, liveTelemetryMaxAge)

	switch {
	case sample.PackagePowerW != 0:
		// A device reporting instantaneous power (OXP's pm-table) needs none
		// of the above.
		s.PackagePowerW = sample.PackagePowerW
	case sample.PackageEnergyUJ != 0:
		if haveLatest {
			s.PackagePowerW = latest.Sample.PackagePowerW
		}
	}

	// The full live edge, expanded quantities included. Built from the fresh
	// sample this handler just took; the two *derived* rates — package power
	// and CPU utilisation — are grafted from the sampler's own most recent
	// figure, because a single reading cannot become a rate and re-baselining
	// the sampler's counters from a socket handler is the data race the
	// paragraph above exists to forbid.
	t := apiTelemetrySample(now.Unix(), sample)
	t.PackagePowerW = s.PackagePowerW
	if haveLatest && latest.Sample.CPUUtilKnown {
		v := latest.Sample.CPUUtilPct
		t.CPUUtilPct = &v
	}
	if haveLatest && latest.Sample.NetRateKnown {
		rx := latest.Sample.NetRxMBps
		t.NetRxMBps = &rx
		tx := latest.Sample.NetTxMBps
		t.NetTxMBps = &tx
	}
	s.Telemetry = &t
}

// freshEnough reports whether a sample taken at `at` is recent enough, as of
// `now`, to serve as a current reading.
//
// # The comparison is on Unix seconds, and that is structural
//
// Both times come from time.Now() and so carry *monotonic* clock readings,
// which Sub and Since prefer over the wall clock. Go's monotonic clock is
// CLOCK_MONOTONIC, which does not advance while the machine is suspended — the
// same fact reconcileSuspendMaxTicks is counted in ticks for, and the reason
// telemetryring stores wall-clock timestamps at all. A monotonic comparison
// would measure a sample taken before a ten-hour suspend as seconds old and
// serve it as the current package draw, for the second between a resume and the
// sampler's next tick.
//
// Unix() reads the wall clock and nothing else, so there is no monotonic path
// through this function to remove by accident. That matters more than the
// precision it costs: **this property cannot be unit-tested.** Wall and
// monotonic readings only diverge across a real suspend or a clock step, and
// no in-process construction separates them — time.Now().Add(-10*time.Hour)
// moves both together, so a test written against it passes just as happily with
// the safeguard deleted. That was checked, not assumed. An equivalent written
// as at.Round(0) is correct today and one "simplify this" away from silently
// not being, with nothing to catch it.
//
// The cost is second granularity, which is nothing here: the sampler runs at
// 1 Hz and the bound is three of its ticks, so the answer can be off by at most
// one tick of a threshold that is itself a judgement call.
//
// A negative age means the wall clock stepped forward between the sample and
// now, which says nothing about how old the reading is, so it is refused on the
// same "absent beats stale" grounds.
func freshEnough(now, at time.Time, limit time.Duration) bool {
	age := time.Duration(now.Unix()-at.Unix()) * time.Second
	return age >= 0 && age <= limit
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
		out = append(out, apiTelemetrySample(e.At.Unix(), e.Sample))
	}
	// Non-nil so the wire carries [] rather than null for an empty history: a
	// daemon that has been up for less than a second has no samples yet, and
	// that is an empty list, not an absent field.
	return response{OK: true, History: out}
}

// apiTelemetrySample projects one driver sample onto the wire. The single
// place the Known-flag → pointer and copy rules live, shared by the history
// handler and the get-state live edge so the two cannot express the same
// sample differently.
//
// Every pointer boxes a fresh variable — taking the address of a loop's own
// copy would hand every entry the same pointer — and the RPM slice is copied
// because the live-edge caller passes a sample the driver still owns.
func apiTelemetrySample(at int64, s driver.Sample) api.TelemetrySample {
	out := api.TelemetrySample{
		At:            at,
		TempC:         s.TempC,
		PackagePowerW: s.PackagePowerW,
		GPUTempC:      s.GPUTempC,
		CPUClockMHz:   s.CPUClockMHz,
		GPUClockMHz:   s.GPUClockMHz,
		MemClockMHz:   s.MemClockMHz,
		NPUClockMHz:   s.NPUClockMHz,
		VRAMUsedMB:    s.VRAMUsedMB,
		VRAMTotalMB:   s.VRAMTotalMB,
		MemUsedMB:     s.MemUsedMB,
		MemTotalMB:    s.MemTotalMB,
	}
	if s.RPM != nil {
		out.RPM = append([]int(nil), s.RPM...)
	}
	if s.BatteryPowerKnown {
		w := s.BatteryPowerW
		out.BatteryPowerW = &w
	}
	if s.BatteryLevelKnown {
		v := s.BatteryLevelPct
		out.BatteryLevelPct = &v
	}
	if s.CPUUtilKnown {
		v := s.CPUUtilPct
		out.CPUUtilPct = &v
	}
	if s.GPUBusyKnown {
		v := s.GPUBusyPct
		out.GPUUtilPct = &v
	}
	if s.GPUPowerKnown {
		w := s.GPUPowerW
		out.GPUPowerW = &w
	}
	if s.NPUKnown {
		w := s.NPUPowerW
		out.NPUPowerW = &w
		v := s.NPUBusyPct
		out.NPUUtilPct = &v
	}
	if s.NetRateKnown {
		rx := s.NetRxMBps
		out.NetRxMBps = &rx
		tx := s.NetTxMBps
		out.NetTxMBps = &tx
	}
	return out
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
