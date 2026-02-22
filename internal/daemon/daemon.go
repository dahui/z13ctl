// Package daemon implements the z13ctl long-running daemon: Unix socket server,
// hardware device management, state persistence, and Armoury Crate button watcher.
//
// Designed as a systemd user service using two units:
//   - z13ctl.socket  — systemd manages the socket fd (socket activation)
//   - z13ctl.service — Type=notify, Restart=on-failure
//
// Can also be run directly for development: z13ctl daemon.
package daemon

// daemon.go — Daemon struct, Run function, socket listener, and subscriber management.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
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
func Run(ctx context.Context) error {
	d := &Daemon{
		buttonCh: make(chan struct{}, 4),
	}

	d.state = loadState()

	dev, err := hid.FindDevice("")
	if err != nil {
		slog.Warn("HID device not found; lighting commands will be unavailable", "err", err)
	} else {
		d.dev = dev
		defer dev.Close()
		if applyErr := d.applyLightingState(); applyErr != nil {
			slog.Warn("failed to restore lighting state", "err", applyErr)
		} else {
			ls := d.state.Lighting
			if ls.Enabled {
				slog.Info("lighting state restored", "mode", ls.Mode, "brightness", ls.Brightness)
			} else {
				slog.Info("lighting state restored (off)")
			}
		}
	}

	// Restore profile if saved.
	if d.state.Profile != "" {
		if profileErr := cli.SetProfile(d.state.Profile); profileErr != nil {
			slog.Warn("failed to restore profile", "err", profileErr)
		} else {
			slog.Info("profile restored", "profile", d.state.Profile)
		}
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

	go watchButton(ctx, d.buttonCh)

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
	if mkdirErr := os.MkdirAll(sock[:len(sock)-len("z13ctl.sock")-1], 0o750); mkdirErr != nil {
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
			d.broadcast(response{Event: "gui-toggle"})
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

// applyLightingState restores lighting from the saved state. d.dev must be non-nil.
// If per-device states are saved (d.state.Devices), each zone is restored independently;
// otherwise the all-device state (d.state.Lighting) is applied to all zones.
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
		}
		return nil
	}
	return applyZone(d.dev, d.state.Lighting)
}
