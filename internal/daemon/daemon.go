// Package daemon implements the voltaire long-running daemon: Unix socket server,
// hardware device management, state persistence, and Armoury Crate button watcher.
//
// Designed as a systemd user service using two units:
//   - voltaire.socket  — systemd manages the socket fd (socket activation)
//   - voltaire.service — Type=notify, Restart=on-failure
//
// Can also be run directly for development: voltaire daemon.
//
// The daemon socket client (Send*, Subscribe, SocketPath) lives in the public
// api package: github.com/dahui/voltaire/api/v2.
package daemon

// daemon.go — Daemon struct, Run function, socket listener, and subscriber management.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/activation"
	sddaemon "github.com/coreos/go-systemd/v22/daemon"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/telemetryring"
)

// Daemon holds the runtime state for the long-running voltaire process.
type Daemon struct {
	// hwMu serializes hardware *mutation sequences* — fan mode, PPT, profile —
	// against each other. d.mu guards state only, and every mutating handler
	// deliberately does its hardware I/O outside it, so without hwMu the
	// reconcile watcher could interleave its SetBothFanCurves with a handler's
	// ResetAllFanCurves and the fans would keep whichever mode landed last.
	//
	// Lock order is hwMu then d.mu. Never acquire hwMu while holding d.mu.
	hwMu sync.Mutex

	// hw is the assembled device: the per-class drivers the device data
	// selected for this machine, with power control wrapped in the safety
	// engine. Set once by Run before any watcher or handler can run (tests
	// inject it), read-only afterwards, so it needs no lock. A nil capability
	// field means the device does not have it; a nil hw altogether (bare test
	// Daemons) reads as a device with no capabilities at all.
	//
	// The one exception to "read-only" is the lighting driver's *internal*
	// state: Reopen swaps its HID handle, so every hw.Lighting call — reads
	// included — happens under d.mu, exactly as the old d.dev field did.
	hw *device.Device

	mu    sync.Mutex
	state api.State

	// suspending is set by releaseVolatileState and cleared by
	// restoreVolatileState. It stands the reconcile watcher down for the window
	// between PrepareForSleep(true) and the freeze, where re-enabling the custom
	// curve would undo the release that lets the EC stop the fans.
	suspending bool

	// suspendGen increments on every transition into suspending. The reconcile
	// watcher uses it to tell one suspend from the next, which a bool cannot do:
	// its stand-down budget is per-suspend, and on a machine that flaps
	// sleep→wake→sleep faster than the 2s poll the watcher may never observe an
	// idle tick to reset on. Without this the budget accumulated across suspends
	// and eventually expired *inside* a real pre-freeze window.
	suspendGen int

	// telemetry is the sampler's history ring, sized by Run from the device's
	// declared window. It carries its own lock and is never read directly —
	// history() substitutes an empty ring for the Daemons tests build as struct
	// literals.
	telemetry *telemetryring.Ring

	// prevEnergy is the last package-energy counter reading, for converting
	// the next one into power. Owned by the sampler goroutine alone and so
	// deliberately unguarded: it is written and read only in sampleOnce, which
	// runs on the single watchTelemetry timer. Anything else that wants to
	// read the counter must take its own baseline rather than sharing this one.
	prevEnergy energyReading

	// prevJiffies is the last CPU jiffie-counter reading, for deriving the
	// utilisation percentage. Owned by the sampler goroutine on exactly
	// prevEnergy's terms: unguarded because that goroutine is the only
	// toucher, so no handler may read or re-baseline it.
	prevJiffies jiffieReading

	// prevNet is the last network byte-counter reading, for deriving the
	// throughput rate. Same ownership terms as the two above.
	prevNet netReading

	subMu       sync.Mutex
	subscribers []subscriber // long-lived connections subscribed to events

	// buttonCh carries the moment of each hardware-button press. It is a
	// timestamp rather than a bare signal because the press *timing* is what
	// distinguishes a single press from a double one; see button.go.
	buttonCh chan time.Time

	// sleepRelease is false under --no-sleep-release: the fans keep the custom
	// curve through sleep. Set once by Run before any watcher starts, and read-only
	// afterwards, which is what makes it safe to read without a lock.
	sleepRelease bool
}

// Options configures the daemon. A zero value disables both watchers, so callers
// state what they want rather than relying on positional bools — Run(ctx, true,
// false) said nothing about which flag was which.
type Options struct {
	// WatchButton starts the Armoury Crate button watcher (--no-button clears it).
	WatchButton bool
	// SleepRelease hands the fans back to the firmware before sleep, so the EC
	// stops them (--no-sleep-release clears it).
	SleepRelease bool
}

// fallbackDeviceID is the device assumed when no device file matches this
// machine. Transitional: voltaire (né z13ctl) has only ever supported the Z13 and every
// earlier version ran best-effort on anything else, so the Z13 config —
// whose drivers all fail soft on absent sysfs — preserves that exactly. The
// multi-device milestone replaces this with a conservative generic device.
const fallbackDeviceID = "asus-rog-flow-z13-2025"

// assembleDevice matches this machine against the embedded device data and
// assembles its drivers. Assembly can only fail on a build defect — a device
// file naming a factory this binary does not register — so an error here is
// worth refusing to start over.
func assembleDevice() (*device.Device, error) {
	c, err := device.MatchConfig()
	if err != nil {
		slog.Warn("no device file matches this machine; assuming the ASUS ROG Flow Z13", "err", err)
		configs, cfgErr := device.Configs()
		if cfgErr != nil {
			return nil, cfgErr
		}
		found := false
		for _, cand := range configs {
			if cand.Device.ID == fallbackDeviceID {
				c, found = cand, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("fallback device %q is not in the embedded device data", fallbackDeviceID)
		}
	}
	return device.Assemble(c)
}

// Run starts the daemon and blocks until ctx is cancelled. It opens HID devices,
// restores the last-saved state, starts the button watcher, and serves the
// Unix socket.
func Run(ctx context.Context, opts Options) error {
	d := &Daemon{
		buttonCh:     make(chan time.Time, 4),
		sleepRelease: opts.SleepRelease,
	}

	if !opts.SleepRelease {
		slog.Info("pre-sleep fan release disabled; the custom curve stays in force through sleep")
	}

	hw, err := assembleDevice()
	if err != nil {
		return fmt.Errorf("assembling device drivers: %w", err)
	}
	d.hw = hw
	d.telemetry = newTelemetryRing(hw)
	slog.Info("device assembled", "id", hw.ID, "model", hw.Model)

	d.state = loadState()
	// Same class of 2.0 migration as loadState's own: the daemon is the only
	// part of voltaire that runs as each user, so it is the only one that can
	// tidy a per-user enable of the pre-2.0 units.
	cleanStaleUnitLinks()

	lightingOpened := false
	if d.hw.Lighting != nil {
		// The driver may hold an open HID handle (its own, or one swapped in by
		// the hotplug watcher later), so close whatever it holds at shutdown.
		defer func() {
			d.mu.Lock()
			if c, ok := d.hw.Lighting.(io.Closer); ok {
				_ = c.Close()
			}
			d.mu.Unlock()
		}()
		d.mu.Lock()
		reopenErr := d.hw.Lighting.Reopen()
		if reopenErr == nil {
			lightingOpened = true
			if applyErr := d.applyLightingState(); applyErr != nil {
				slog.Warn("failed to restore lighting state", "err", applyErr)
			}
		}
		d.mu.Unlock()
		if reopenErr != nil {
			slog.Warn("lighting device not opened; the hotplug watcher will retry", "err", reopenErr)
		}
	}

	// Resolve the power source before restoring anything, so a machine that was
	// on AC when the daemon stopped and is on battery now lands on the battery
	// profile directly. The watcher deliberately does not act on its first
	// observation: the restore below already skips a redundant platform_profile
	// write, and that write costs a WMI fan-controller reset (see the comment on
	// the restore).
	leftCustom, autoswitched := false, false
	if onAC, known := d.acPower(); known {
		if target := autoswitchTarget(d.state, onAC); target != "" {
			slog.Info("autoswitch: selecting startup profile", "source", sourceName(onAC), "profile", target)
			leftCustom = d.state.InCustomProfile() && !d.state.IsCustomProfile(target)
			d.state.Profile = target
			autoswitched = true
		}
	}

	// A daemon restart does not reset the fan controller or the Curve Optimizer
	// — both are hardware state that outlives the process — so a custom profile
	// that was in force is still in force now. If autoswitch has just moved us
	// off it onto a firmware profile, release them, exactly as applyStockHW
	// would. Without this the machine keeps running the old profile's fan curve
	// and undervolt while the daemon reports a firmware profile, and the
	// reconcile watcher stays inert because the profile is no longer custom.
	if leftCustom {
		if d.uvAvailable() {
			if uvErr := d.hw.Undervolt.Reset(); uvErr != nil {
				slog.Warn("failed to reset undervolt leaving the custom profile", "err", uvErr)
			}
		}
		if d.hw.Fans != nil {
			if fanErr := d.hw.Fans.Release(); fanErr != nil {
				slog.Warn("failed to release fans leaving the custom profile", "err", fanErr)
			}
		}
	}

	// Restore stock profile if saved, but only if it differs from the
	// kernel's current profile. Writing the same value to platform_profile
	// still triggers a WMI call that resets the fan controller, briefly
	// stopping fans — harmful on daemon restart where the profile hasn't
	// changed. Skip custom profiles — they are never written to
	// platform_profile; their fan curves and TDP are restored separately below.
	// Gated on IsStockProfile, not on "not custom": a state file naming a profile
	// that is neither — one deleted by hand, or lost in a downgrade — would
	// otherwise be written straight to platform_profile, where the kernel rejects
	// it. Only a firmware profile name may ever reach that attribute.
	if api.IsStockProfileName(d.state.Profile) && d.hw.Profiles != nil {
		if current := d.profileHW(); current != d.state.Profile {
			if profileErr := d.hw.Profiles.Set(d.state.Profile); profileErr != nil {
				slog.Warn("failed to restore profile", "err", profileErr)
			} else {
				slog.Info("profile restored", "profile", d.state.Profile)
			}
		}
		// Write the profile's stock PPT even when platform_profile already
		// matches: the kernel's PPT attributes come up holding a stale 5W cache
		// after boot, and nothing else restores them. Unlike a profile write this
		// is not a WMI call, so it does not disturb the fan controller.
		d.restoreStockPPT(d.state.Profile)
	}

	// Restore battery charge limit if saved.
	if d.state.Battery > 0 && d.hw.Battery != nil {
		if batErr := d.hw.Battery.SetChargeLimit(d.state.Battery); batErr != nil {
			slog.Warn("failed to restore battery limit", "err", batErr)
		} else {
			slog.Info("battery limit restored", "limit", d.state.Battery)
		}
	}

	// Restore panel overdrive if enabled (firmware may not persist this across reboot).
	if d.state.PanelOverdrive != 0 && d.hw.Toggles != nil {
		if poErr := d.hw.Toggles.Set("panel_overdrive", d.state.PanelOverdrive); poErr != nil {
			slog.Warn("failed to restore panel overdrive", "err", poErr)
		} else {
			slog.Info("panel overdrive restored", "value", d.state.PanelOverdrive)
		}
	}

	// Restore CPU boost. cpufreq comes up boosting on every boot regardless of
	// what the user last chose, so this is the whole reason the setting is kept
	// in state at all — without the replay, "boost off" is a decision that
	// silently expires overnight. Only a stored *false* is acted on: a nil
	// means the user never expressed a preference, and writing the default back
	// would be voltaire claiming a setting it was never given.
	if d.state.CPUBoost != nil && !*d.state.CPUBoost && d.hw.CPUBoost != nil {
		if bErr := d.hw.CPUBoost.Set(false); bErr != nil {
			slog.Warn("failed to restore cpu boost", "err", bErr)
		} else {
			slog.Info("cpu boost restored", "on", false)
		}
	}

	// Restore fan curve + TDP + undervolt if the last profile was a custom one.
	// This goes through the same helper the socket command and the autoswitch
	// watcher use, so startup cannot drift from them — in particular it clears
	// the subsystems the profile does not set, which matters after a daemon
	// restart where the previous profile's curve and offset are still live in
	// hardware. ApplyTDPSafely still raises the fan floor before raising power
	// and declines the TDP entirely if that write fails, so a machine can never
	// come up above the safe sustained max without a floor.
	if active, ok := d.state.ActiveCustomProfile(); ok && !active.Empty() {
		slog.Info("restoring custom profile", "profile", active.Name)
		d.applyCustomHW(active)
	} else if autoswitched {
		// Persist the startup autoswitch decision. A custom target is saved by
		// applyCustomHW above, but a firmware target had no such path, so the
		// daemon acted on a decision it never recorded: the state file kept naming
		// the profile from the previous session. That is self-correcting only while
		// autoswitch stays enabled and configured the same way — disable it, or
		// clear one side, and the next start restores a profile the machine had
		// already been switched away from.
		if saveErr := saveState(cloneState(d.state)); saveErr != nil {
			slog.Warn("failed to save the startup autoswitch profile", "err", saveErr)
		}
	}

	switch {
	case !opts.WatchButton:
		slog.Info("hardware button watcher disabled")
	case d.hw.Buttons == nil:
		slog.Info("no hardware button on this device")
	default:
		go d.watchButtons(ctx)
	}

	go d.watchResume(ctx)

	go d.watchHotplug(ctx, lightingOpened)

	// State-driven, so it is a no-op on a machine that never uses a custom
	// profile; register it unconditionally.
	go d.watchReconcile(ctx)

	// Likewise inert until autoswitch is configured.
	go d.watchPowerSource(ctx)

	// Inert on a device with no telemetry source or no declared history; the
	// tick decides, so there is one place to read the rule.
	go d.watchTelemetry(ctx)

	lns, err := d.getListeners()
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	closeAll := func() {
		for _, ln := range lns {
			_ = ln.Close()
		}
	}
	defer closeAll()

	if _, notifyErr := sddaemon.SdNotify(false, sddaemon.SdNotifyReady); notifyErr != nil {
		slog.Warn("sd_notify READY failed", "err", notifyErr)
	}
	addrs := make([]string, len(lns))
	for i, ln := range lns {
		addrs[i] = ln.Addr().String()
	}
	slog.Info("voltaire daemon ready", "sockets", strings.Join(addrs, ", "))

	go d.broadcastLoop(ctx)

	go func() {
		<-ctx.Done()
		_, _ = sddaemon.SdNotify(false, sddaemon.SdNotifyStopping)
		closeAll()
	}()

	// One accept loop per listener; every connection is answered identically,
	// so a client cannot tell the canonical socket from the legacy one. The
	// first accept error wins and takes the daemon down (the deferred closeAll
	// unblocks the sibling loops), except during shutdown, where the closes
	// above surface here as expected errors.
	errc := make(chan error, len(lns))
	for _, ln := range lns {
		go func(ln net.Listener) {
			for {
				conn, acceptErr := ln.Accept()
				if acceptErr != nil {
					errc <- acceptErr
					return
				}
				go d.handleConn(conn)
			}
		}(ln)
	}
	err = <-errc
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("accept: %w", err)
}

// getListeners returns every socket the daemon serves: all fds handed over by
// systemd socket activation (voltaire.socket carries one ListenStream per
// path), or self-created sockets at each api.SocketPaths() entry otherwise.
//
// The legacy z13ctl path is served through the whole 2.x line — the Decky
// plugin and pre-2.0 clients hardcode it. On the self-created path its
// failure is a warning, not an error: the daemon must come up on the
// canonical socket even if something is squatting on the old one.
func (d *Daemon) getListeners() ([]net.Listener, error) {
	if activated, err := activation.Listeners(); err == nil {
		var lns []net.Listener
		for _, ln := range activated {
			if ln != nil {
				lns = append(lns, ln)
			}
		}
		if len(lns) > 0 {
			slog.Info("using systemd socket activation", "sockets", len(lns))
			return lns, nil
		}
	}

	var lns []net.Listener
	for i, sock := range api.SocketPaths() {
		if mkdirErr := os.MkdirAll(filepath.Dir(sock), 0o750); mkdirErr != nil {
			if i == 0 {
				return nil, mkdirErr
			}
			slog.Warn("cannot create legacy socket directory", "path", sock, "err", mkdirErr)
			continue
		}
		_ = os.Remove(sock)
		ln, err := net.Listen("unix", sock)
		if err != nil {
			if i == 0 {
				for _, l := range lns {
					_ = l.Close()
				}
				return nil, err
			}
			slog.Warn("cannot listen on legacy socket", "path", sock, "err", err)
			continue
		}
		slog.Info("listening on Unix socket", "path", sock)
		lns = append(lns, ln)
	}
	return lns, nil
}

// broadcastLoop turns Armoury Crate button presses into subscriber events until
// ctx is done, then closes all subscriber connections. What a press means is
// pressTick's decision; this only carries the state between presses.
func (d *Daemon) broadcastLoop(ctx context.Context) {
	var presses pressState
	for {
		select {
		case <-ctx.Done():
			d.subMu.Lock()
			for _, s := range d.subscribers {
				_ = s.conn.Close()
			}
			d.subscribers = nil
			d.subMu.Unlock()
			return
		case at := <-d.buttonCh:
			var act pressAction
			presses, act = pressTick(presses, at)
			d.emitPress(act)
		}
	}
}

// broadcastWriteTimeout bounds a single write to one subscriber.
//
// Broadcasts are emitted from handlers that hold hwMu, so an unbounded write to
// a subscriber that has stopped reading — a hung GUI, a suspended process —
// would block every hardware operation in the daemon behind a socket buffer.
// A subscriber that cannot take a notification in this long is dropped. Declared
// as a var so tests can shorten it.
var broadcastWriteTimeout = 2 * time.Second

// subscriber is one long-lived event connection and the events it asked for.
type subscriber struct {
	conn net.Conn
	// events is the set the client subscribed to. Empty means every event: that
	// is what a client sending no list gets, and it is what the daemon did for
	// every subscriber before the filter existed.
	events map[string]bool
}

// wants reports whether this subscriber asked for the named event.
func (s subscriber) wants(event string) bool {
	return len(s.events) == 0 || s.events[event]
}

// broadcast delivers an event to the subscribers that asked for it.
//
// The filter matters as soon as there is more than one event name: a client
// that subscribed to "gui-toggle" and reasonably wrote `for range ch { toggle() }`
// — the obvious loop when only one event existed — would otherwise toggle its
// window on every power-source change.
func (d *Daemon) broadcast(r response) {
	data, _ := json.Marshal(r)
	data = append(data, '\n')

	d.subMu.Lock()
	alive := d.subscribers[:0:0]
	for _, s := range d.subscribers {
		if !s.wants(r.Event) {
			alive = append(alive, s)
			continue
		}
		_ = s.conn.SetWriteDeadline(time.Now().Add(broadcastWriteTimeout))
		_, err := s.conn.Write(data)
		_ = s.conn.SetWriteDeadline(time.Time{})
		if err == nil {
			alive = append(alive, s)
		} else {
			_ = s.conn.Close()
		}
	}
	d.subscribers = alive
	d.subMu.Unlock()
}

// addSubscriber registers a connection for the named events. An empty or nil
// list subscribes to everything.
func (d *Daemon) addSubscriber(conn net.Conn, events []string) {
	set := make(map[string]bool, len(events))
	for _, e := range events {
		if e != "" {
			set[e] = true
		}
	}
	d.subMu.Lock()
	d.subscribers = append(d.subscribers, subscriber{conn: conn, events: set})
	d.subMu.Unlock()
}

// notifyStateChanged tells subscribers that profile or thermal state moved, so
// anything displaying it can re-read. Callers pass the state they just saved
// only to make the call sites read as "persist, then announce"; the event
// carries no payload by design (see api/events.go).
func (d *Daemon) notifyStateChanged() {
	d.broadcast(response{OK: true, Event: api.EventStateChanged})
}

// saveAndNotify persists a state snapshot and announces the change. Every
// handler that mutates profile or thermal state goes through this rather than
// calling saveState directly, so a new handler cannot silently leave clients
// showing stale values.
func (d *Daemon) saveAndNotify(s api.State) {
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	d.notifyStateChanged()
}

// setSuspending records whether the machine is on its way into sleep, bumping the
// generation on each entry so the reconcile watcher can tell suspends apart. See
// the field comments on Daemon.suspending and Daemon.suspendGen.
func (d *Daemon) setSuspending(v bool) {
	d.mu.Lock()
	if v && !d.suspending {
		d.suspendGen++
	}
	d.suspending = v
	d.mu.Unlock()
}

// env returns the device's power envelope, or the zero envelope when the
// device has no power control. The zero value declares no limits and no floor
// curve, which makes every safety check a pass-through — the correct reading
// of "this device imposes nothing".
func (d *Daemon) env() driver.PowerEnvelope {
	if d.hw == nil || d.hw.Power == nil {
		return driver.PowerEnvelope{}
	}
	return d.hw.Power.Envelope()
}

// acPower reports the power source, with known=false when it cannot be
// observed — no battery capability, or no readable Mains supply. Callers must
// treat unknown as "do nothing", never as "on battery": a machine with no
// mains device (a VM, a desktop, a driver not yet bound) would otherwise run
// the battery profile forever.
func (d *Daemon) acPower() (onAC, known bool) {
	st, ok := d.batteryStatus()
	if !ok {
		return false, false
	}
	return st.OnAC, st.ACKnown
}

// batteryStatus reads the battery once, for callers that want more from it
// than the power source. ok is false when the device has no battery capability
// or the read failed; get-state wants the charge level, the power source and
// state of health together, and a helper per field would read sysfs three
// times for one answer.
func (d *Daemon) batteryStatus() (driver.BatteryStatus, bool) {
	if d.hw == nil || d.hw.Battery == nil {
		return driver.BatteryStatus{}, false
	}
	st, err := d.hw.Battery.Status()
	if err != nil {
		return driver.BatteryStatus{}, false
	}
	return st, true
}

// uvAvailable reports whether the Curve Optimizer path actually works on this
// machine. The underlying probe may write hardware, but the driver caches its
// answer for the process lifetime, so after the first call — made on the apply
// path at startup, never speculatively — this is a plain bool read.
func (d *Daemon) uvAvailable() bool {
	return d.hw != nil && d.hw.Undervolt != nil && d.hw.Undervolt.ProbeAvailable()
}

// profileHW reads the platform profile from hardware, or "" when the device
// has no profile control or the read fails.
func (d *Daemon) profileHW() string {
	if d.hw == nil || d.hw.Profiles == nil {
		return ""
	}
	p, err := d.hw.Profiles.Get()
	if err != nil {
		return ""
	}
	return p
}

// hasApplicableCurve reports whether fc carries a curve the device's fan
// controller can apply: custom mode, and exactly the point count the fan shape
// declares. A device with no fan control can apply nothing.
func (d *Daemon) hasApplicableCurve(fc *api.FanCurveState) bool {
	if fc == nil || fc.Mode != 1 || d.hw == nil || d.hw.Fans == nil {
		return false
	}
	return len(fc.Points) == d.hw.Fans.Shape().Points
}

// checkFanFloorRelease reports whether the fans may be released to firmware
// auto, judged against the limit hardware reports for the effective profile.
// A device without power control imposes nothing; a PPT read failure is
// deliberately not a refusal (see safety.Engine.CheckFanFloorRelease).
func (d *Daemon) checkFanFloorRelease(profile string) error {
	if d.hw == nil || d.hw.Power == nil {
		return nil
	}
	return d.hw.Power.CheckFanFloorRelease(profile)
}

// normalizeLightingState fills in any field left empty by a partial update,
// preferring fallback and then the built-in defaults.
//
// A per-device entry can legitimately be stored with only some fields set:
// handleOff saves {Enabled: false} for a named zone, and a later brightness
// command on that same zone reuses the entry, so the result is enabled with an
// empty mode, colour and speed. That state is unappliable — ModeFromString("")
// is an error — which made every subsequent restore fail, on daemon start, on
// resume, and on keyboard hotplug. Normalising on the way out repairs the state
// files users already have on disk, not just newly written ones.
func normalizeLightingState(ls, fallback api.LightingState) api.LightingState {
	def := defaultState().Lighting
	pick := func(vals ...string) string {
		for _, v := range vals {
			if v != "" {
				return v
			}
		}
		return ""
	}
	ls.Mode = pick(ls.Mode, fallback.Mode, def.Mode)
	ls.Speed = pick(ls.Speed, fallback.Speed, def.Speed)
	ls.Color = pick(ls.Color, fallback.Color, def.Color)
	// Colour 2 only matters for breathe, and "000000" is a meaningful value
	// there, so fall back to the default rather than treating empty as unset.
	ls.Color2 = pick(ls.Color2, fallback.Color2, def.Color2)
	return ls
}

// reopenAndRestore re-opens the lighting device and re-applies the saved
// lighting state. It is called after the detachable keyboard is reattached,
// where the keyboard appears as a new hidraw node the driver's stale handle no
// longer references. Returns true on success; false (with a logged warning) if
// the device cannot be reopened yet — e.g. udev has not finished applying
// hidraw permissions — so the caller can retry.
func (d *Daemon) reopenAndRestore() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.hw.Lighting.Reopen(); err != nil {
		slog.Warn("hotplug: failed to reopen HID device", "err", err)
		return false
	}
	if err := d.applyLightingState(); err != nil {
		slog.Warn("hotplug: failed to restore lighting", "err", err)
		return false
	}
	slog.Info("keyboard reattached; lighting restored")
	return true
}

// applyLightingState restores lighting from the saved state. If per-device
// states are saved (d.state.Devices), each zone is restored independently;
// otherwise the all-device state (d.state.Lighting) is applied to all zones.
//
// The caller must hold d.mu: this drives the lighting driver (whose HID handle
// the hotplug watcher swaps via Reopen) and reads d.state.Devices (which
// socket handlers mutate). Reading the map unlocked while a handler writes it
// is a concurrent map access, which the Go runtime turns into an unrecoverable
// crash.
func (d *Daemon) applyLightingState() error {
	if d.hw == nil || d.hw.Lighting == nil {
		return nil
	}
	if len(d.state.Devices) > 0 {
		var firstErr error
		for _, name := range d.hw.Lighting.Zones() {
			ls := d.state.Lighting
			if dl, ok := d.state.Devices[name]; ok {
				ls = normalizeLightingState(dl, d.state.Lighting)
			}
			// Keep going after a failure: the zones are independent, and
			// returning early meant one bad or unwritable zone silently left
			// the other one dark. A zone that is simply not present (the
			// detached keyboard) is skipped, not failed.
			if err := d.hw.Lighting.Apply(name, ls); err != nil {
				if errors.Is(err, driver.ErrUnsupported) {
					continue // zone not present on this system
				}
				slog.Warn("failed to restore lighting", "zone", name, "err", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if ls.Enabled {
				slog.Info("lighting restored", "zone", name, "mode", ls.Mode, "brightness", ls.Brightness)
			} else {
				slog.Info("lighting restored (off)", "zone", name)
			}
		}
		return firstErr
	}
	if err := d.hw.Lighting.Apply("", normalizeLightingState(d.state.Lighting, api.LightingState{})); err != nil {
		if errors.Is(err, driver.ErrUnsupported) {
			return nil // no lighting device open; nothing to restore
		}
		return err
	}
	if d.state.Lighting.Enabled {
		slog.Info("lighting restored", "zone", "all", "mode", d.state.Lighting.Mode, "brightness", d.state.Lighting.Brightness)
	} else {
		slog.Info("lighting restored (off)", "zone", "all")
	}
	return nil
}
