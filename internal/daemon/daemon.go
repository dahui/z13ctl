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

	"z13ctl/internal/aura"
	"z13ctl/internal/cli"
	"z13ctl/internal/hid"
)

// Daemon holds the runtime state for the long-running z13ctl process.
type Daemon struct {
	mu    sync.Mutex
	dev   *hid.Device // nil if no HID device was found at startup
	state State

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
			slog.Info("lighting state restored")
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

	sock := socketPath()
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

// applyLightingState restores lighting from the saved state. d.dev must be non-nil.
func (d *Daemon) applyLightingState() error {
	ls := d.state.Lighting
	if !ls.Enabled {
		return aura.TurnOff(d.dev)
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
	return aura.Apply(d.dev, mode, r, g, b, r2, g2, b2, speed, uint8(ls.Brightness))
}
