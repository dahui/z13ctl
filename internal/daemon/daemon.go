// Package daemon implements the z13ctl long-running daemon: Unix socket server,
// hardware device management, state persistence, and Armoury Crate button watcher.
//
// Designed as a systemd user service using two units:
//   - z13ctl.socket  — systemd manages the socket fd (socket activation)
//   - z13ctl.service — Type=notify, Restart=on-failure
//
// Can also be run directly for development: z13ctl daemon.
//
// The daemon socket client (Send*, Subscribe, SocketPath) lives in the public
// api package: github.com/dahui/z13ctl/api.
package daemon

// daemon.go — Daemon struct, Run function, socket listener, and subscriber management.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/activation"
	sddaemon "github.com/coreos/go-systemd/v22/daemon"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/cli"
	"github.com/dahui/z13ctl/internal/hid"
)

// Daemon holds the runtime state for the long-running z13ctl process.
type Daemon struct {
	// hwMu serializes hardware *mutation sequences* — fan mode, PPT, profile —
	// against each other. d.mu guards state only, and every mutating handler
	// deliberately does its hardware I/O outside it, so without hwMu the
	// reconcile watcher could interleave its SetBothFanCurves with a handler's
	// ResetAllFanCurves and the fans would keep whichever mode landed last.
	//
	// Lock order is hwMu then d.mu. Never acquire hwMu while holding d.mu.
	hwMu sync.Mutex

	mu    sync.Mutex
	dev   *hid.Device // nil if no HID device was found at startup
	state api.State

	subMu       sync.Mutex
	subscribers []subscriber // long-lived connections subscribed to events

	buttonCh chan struct{}
}

// Run starts the daemon and blocks until ctx is cancelled. It opens HID devices,
// restores the last-saved state, starts the button watcher, and serves the
// Unix socket.
func Run(ctx context.Context, watchBtn bool) error {
	d := &Daemon{
		buttonCh: make(chan struct{}, 4),
	}

	d.state = loadState()

	dev, err := hid.FindDevice("")
	if err != nil {
		slog.Warn("HID device not found; lighting commands will be unavailable", "err", err)
	} else {
		d.dev = dev
		// reopenAndRestore may replace d.dev on keyboard hotplug, so close
		// whatever the current device is at shutdown rather than the original.
		defer func() {
			d.mu.Lock()
			if d.dev != nil {
				d.dev.Close()
			}
			d.mu.Unlock()
		}()
		if applyErr := d.applyLightingState(); applyErr != nil {
			slog.Warn("failed to restore lighting state", "err", applyErr)
		}
	}

	// Resolve the power source before restoring anything, so a machine that was
	// on AC when the daemon stopped and is on battery now lands on the battery
	// profile directly. The watcher deliberately does not act on its first
	// observation: the restore below already skips a redundant platform_profile
	// write, and that write costs a WMI fan-controller reset (see the comment on
	// the restore).
	leftCustom := false
	if onAC, acErr := cli.OnACPower(); acErr == nil {
		if target := autoswitchTarget(d.state, onAC); target != "" {
			source := "battery"
			if onAC {
				source = "AC"
			}
			slog.Info("autoswitch: selecting startup profile", "source", source, "profile", target)
			leftCustom = d.state.InCustomProfile() && !d.state.IsCustomProfile(target)
			d.state.Profile = target
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
		if cli.SMUProbeUndervolt() {
			if uvErr := cli.ResetCurveOptimizer(); uvErr != nil {
				slog.Warn("failed to reset undervolt leaving the custom profile", "err", uvErr)
			}
		}
		if fanErr := cli.ResetAllFanCurves(); fanErr != nil {
			slog.Warn("failed to release fans leaving the custom profile", "err", fanErr)
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
	if cli.IsStockProfile(d.state.Profile) {
		current := ""
		if data, readErr := os.ReadFile(cli.FindProfilePath()); readErr == nil {
			current = strings.TrimSpace(string(data))
		}
		if current != d.state.Profile {
			if profileErr := cli.SetProfile(d.state.Profile); profileErr != nil {
				slog.Warn("failed to restore profile", "err", profileErr)
			} else {
				slog.Info("profile restored", "profile", d.state.Profile)
			}
		}
		// Write the profile's stock PPT even when platform_profile already
		// matches: the kernel's PPT attributes come up holding a stale 5W cache
		// after boot, and nothing else restores them. Unlike SetProfile this is
		// not a WMI call, so it does not disturb the fan controller.
		restoreStockPPT(d.state.Profile)
	}

	// Restore battery charge limit if saved.
	if d.state.Battery > 0 {
		path := cli.FindBatteryThresholdPath()
		if batErr := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", d.state.Battery)), 0o644); batErr != nil {
			slog.Warn("failed to restore battery limit", "err", batErr)
		} else {
			slog.Info("battery limit restored", "limit", d.state.Battery)
		}
	}

	// Restore panel overdrive if enabled (firmware may not persist this across reboot).
	if d.state.PanelOverdrive != 0 {
		if poErr := cli.SetPanelOverdrive(d.state.PanelOverdrive); poErr != nil {
			slog.Warn("failed to restore panel overdrive", "err", poErr)
		} else {
			slog.Info("panel overdrive restored", "value", d.state.PanelOverdrive)
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
	}

	if watchBtn {
		go watchButton(ctx, d.buttonCh)
	} else {
		slog.Info("Armoury Crate button watcher disabled")
	}

	go d.watchResume(ctx)

	go d.watchHotplug(ctx)

	// State-driven, so it is a no-op on a machine that never uses a custom
	// profile; register it unconditionally.
	go d.watchReconcile(ctx)

	// Likewise inert until autoswitch is configured.
	go d.watchPowerSource(ctx)

	ln, err := d.getListener()
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	defer func() { _ = ln.Close() }()

	if _, err := sddaemon.SdNotify(false, sddaemon.SdNotifyReady); err != nil {
		slog.Warn("sd_notify READY failed", "err", err)
	}
	slog.Info("z13ctl daemon ready", "socket", ln.Addr())

	go d.broadcastLoop(ctx)

	go func() {
		<-ctx.Done()
		_, _ = sddaemon.SdNotify(false, sddaemon.SdNotifyStopping)
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go d.handleConn(conn)
	}
}

// getListener returns a net.Listener from systemd socket activation if available,
// otherwise creates a new Unix socket at socketPath().
func (d *Daemon) getListener() (net.Listener, error) {
	listeners, err := activation.Listeners()
	if err == nil && len(listeners) > 0 && listeners[0] != nil {
		slog.Info("using systemd socket activation")
		return listeners[0], nil
	}

	sock := api.SocketPath()
	if mkdirErr := os.MkdirAll(filepath.Dir(sock), 0o750); mkdirErr != nil {
		return nil, mkdirErr
	}
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	slog.Info("listening on Unix socket", "path", sock)
	return ln, nil
}

// broadcastLoop forwards Armoury Crate button presses to all subscribers
// until ctx is done, then closes all subscriber connections.
func (d *Daemon) broadcastLoop(ctx context.Context) {
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
		case <-d.buttonCh:
			// OK must be true: `ok` has no omitempty, so leaving it zero ships
			// {"ok":false,...} on a perfectly good event, and any client that
			// checks ok before dispatching — the documented contract — silently
			// drops every button press.
			d.broadcast(response{OK: true, Event: api.EventGUIToggle})
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

// applyZone applies a LightingState to a specific HID device or zone.
func applyZone(dev *hid.Device, ls api.LightingState) error {
	if !ls.Enabled {
		return aura.TurnOff(dev)
	}
	mode, err := aura.ModeFromString(ls.Mode)
	if err != nil {
		return err
	}
	speed, err := aura.SpeedFromString(ls.Speed)
	if err != nil {
		return err
	}
	r, g, b, err := cli.ParseColor(ls.Color)
	if err != nil {
		return err
	}
	r2, g2, b2, err := cli.ParseColor(ls.Color2)
	if err != nil {
		return err
	}
	return aura.Apply(dev, mode, r, g, b, r2, g2, b2, speed, uint8(ls.Brightness))
}

// reopenAndRestore re-enumerates the HID device and re-applies the saved lighting
// state. It is called after the detachable keyboard is reattached, where the
// keyboard appears as a new hidraw node that the old d.dev no longer references.
// Returns true on success; false (with a logged warning) if the device cannot be
// reopened yet — e.g. udev has not finished applying hidraw permissions — so the
// caller can retry.
func (d *Daemon) reopenAndRestore() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	dev, err := hid.FindDevice("")
	if err != nil {
		slog.Warn("hotplug: failed to reopen HID device", "err", err)
		return false
	}
	if d.dev != nil {
		d.dev.Close()
	}
	d.dev = dev
	if err := d.applyLightingState(); err != nil {
		slog.Warn("hotplug: failed to restore lighting", "err", err)
		return false
	}
	slog.Info("keyboard reattached; lighting restored")
	return true
}

// applyLightingState restores lighting from the saved state. d.dev must be non-nil.
// If per-device states are saved (d.state.Devices), each zone is restored independently;
// otherwise the all-device state (d.state.Lighting) is applied to all zones.
//
// The caller must hold d.mu: this reads d.dev (which the hotplug watcher closes
// and replaces) and d.state.Devices (which socket handlers mutate). Reading the
// map unlocked while a handler writes it is a concurrent map access, which the
// Go runtime turns into an unrecoverable crash.
func (d *Daemon) applyLightingState() error {
	if len(d.state.Devices) > 0 {
		var firstErr error
		for _, name := range []string{"keyboard", "lightbar"} {
			ls := d.state.Lighting
			if dl, ok := d.state.Devices[name]; ok {
				ls = normalizeLightingState(dl, d.state.Lighting)
			}
			target, ferr := d.dev.FilteredView(name)
			if ferr != nil {
				continue // zone not present on this system
			}
			// Keep going after a failure: the zones are independent, and
			// returning here meant one bad or unwritable zone silently left the
			// other one dark.
			if err := applyZone(target, ls); err != nil {
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
	if err := applyZone(d.dev, normalizeLightingState(d.state.Lighting, api.LightingState{})); err != nil {
		return err
	}
	if d.state.Lighting.Enabled {
		slog.Info("lighting restored", "zone", "all", "mode", d.state.Lighting.Mode, "brightness", d.state.Lighting.Brightness)
	} else {
		slog.Info("lighting restored (off)", "zone", "all")
	}
	return nil
}
