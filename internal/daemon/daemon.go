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

	"github.com/coreos/go-systemd/v22/activation"
	sddaemon "github.com/coreos/go-systemd/v22/daemon"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/cli"
	"github.com/dahui/z13ctl/internal/hid"
)

// Daemon holds the runtime state for the long-running z13ctl process.
type Daemon struct {
	mu    sync.Mutex
	dev   *hid.Device // nil if no HID device was found at startup
	state api.State

	subMu       sync.Mutex
	subscribers []net.Conn // long-lived connections subscribed to events

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

	// Restore stock profile if saved, but only if it differs from the
	// kernel's current profile. Writing the same value to platform_profile
	// still triggers a WMI call that resets the fan controller, briefly
	// stopping fans — harmful on daemon restart where the profile hasn't
	// changed. Skip "custom" — it's a virtual profile that the kernel
	// rejects; custom fan curves and TDP are restored separately below.
	if d.state.Profile != "" && d.state.Profile != "custom" {
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

	// Restore fan curve + TDP if last profile was "custom".
	if d.state.Profile == "custom" {
		if fc := d.state.FanCurve; fc != nil && fc.Mode == 1 && len(fc.Points) == 8 {
			if fcErr := cli.SetBothFanCurves(fc.Points); fcErr != nil {
				slog.Warn("failed to restore fan curve", "err", fcErr)
			} else {
				slog.Info("fan curve restored")
			}
		}
		if t := d.state.TDP; t != nil {
			// Set fans to 80% minimum if sustained TDP exceeds safe max.
			if t.PL1SPL > cli.TDPMaxSafe {
				if fsErr := cli.SetBothFanCurves(cli.HighTDPFanCurve()); fsErr != nil {
					slog.Warn("failed to set high-TDP fan curve for TDP restore", "err", fsErr)
				}
			}
			if tdpErr := cli.SetTDPState(*t); tdpErr != nil {
				slog.Warn("failed to restore TDP", "err", tdpErr)
			} else {
				slog.Info("TDP restored", "pl1", t.PL1SPL, "pl2", t.PL2SPPT, "pl3", t.FPPT)
			}
		}
		if uv := d.state.Undervolt; uv != nil && cli.SMUProbeUndervolt() {
			if uvErr := cli.SetCurveOptimizer(uv.CPUCO); uvErr != nil {
				slog.Warn("failed to restore undervolt", "err", uvErr)
			} else {
				d.state.Undervolt.Active = true
				slog.Info("undervolt restored", "cpu", uv.CPUCO)
			}
		}
	}

	if watchBtn {
		go watchButton(ctx, d.buttonCh)
	} else {
		slog.Info("Armoury Crate button watcher disabled")
	}

	go d.watchResume(ctx)

	go d.watchHotplug(ctx)

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
			for _, c := range d.subscribers {
				_ = c.Close()
			}
			d.subscribers = nil
			d.subMu.Unlock()
			return
		case <-d.buttonCh:
			// OK must be true: `ok` has no omitempty, so leaving it zero ships
			// {"ok":false,...} on a perfectly good event, and any client that
			// checks ok before dispatching — the documented contract — silently
			// drops every button press.
			d.broadcast(response{OK: true, Event: "gui-toggle"})
		}
	}
}

func (d *Daemon) broadcast(r response) {
	data, _ := json.Marshal(r)
	data = append(data, '\n')

	d.subMu.Lock()
	var alive []net.Conn
	for _, c := range d.subscribers {
		if _, err := c.Write(data); err == nil {
			alive = append(alive, c)
		} else {
			_ = c.Close()
		}
	}
	d.subscribers = alive
	d.subMu.Unlock()
}

func (d *Daemon) addSubscriber(conn net.Conn) {
	d.subMu.Lock()
	d.subscribers = append(d.subscribers, conn)
	d.subMu.Unlock()
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
		for _, name := range []string{"keyboard", "lightbar"} {
			ls := d.state.Lighting
			if dl, ok := d.state.Devices[name]; ok {
				ls = dl
			}
			target, ferr := d.dev.FilteredView(name)
			if ferr != nil {
				continue // zone not present on this system
			}
			if err := applyZone(target, ls); err != nil {
				return err
			}
			if ls.Enabled {
				slog.Info("lighting restored", "zone", name, "mode", ls.Mode, "brightness", ls.Brightness)
			} else {
				slog.Info("lighting restored (off)", "zone", name)
			}
		}
		return nil
	}
	if err := applyZone(d.dev, d.state.Lighting); err != nil {
		return err
	}
	if d.state.Lighting.Enabled {
		slog.Info("lighting restored", "zone", "all", "mode", d.state.Lighting.Mode, "brightness", d.state.Lighting.Brightness)
	} else {
		slog.Info("lighting restored (off)", "zone", "all")
	}
	return nil
}
