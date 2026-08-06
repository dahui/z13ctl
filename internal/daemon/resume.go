package daemon

// resume.go — sleep/resume watcher via systemd-logind DBus signals.
//
// Listens for org.freedesktop.login1.Manager.PrepareForSleep(false) on the
// system bus. When the system resumes from sleep, lighting and all volatile
// settings (undervolt, TDP, fan curves) are reapplied from daemon state.

import (
	"context"
	"log/slog"

	"github.com/godbus/dbus/v5"

	"github.com/dahui/z13ctl/internal/aura"
)

// watchResume connects to the system DBus and listens for resume events.
// When PrepareForSleep(false) is received, restoreVolatileState is called.
// Blocks until ctx is cancelled.
func (d *Daemon) watchResume(ctx context.Context) {
	conn, err := dbus.SystemBus()
	if err != nil {
		slog.Warn("cannot connect to system DBus for resume watcher", "err", err)
		return
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		slog.Warn("failed to add DBus match rule for PrepareForSleep", "err", err)
		return
	}

	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	slog.Info("resume watcher started (listening for PrepareForSleep)")

	for {
		select {
		case <-ctx.Done():
			conn.RemoveSignal(ch)
			return
		case sig := <-ch:
			if sig == nil {
				continue
			}
			if sig.Name != "org.freedesktop.login1.Manager.PrepareForSleep" {
				continue
			}
			if len(sig.Body) < 1 {
				continue
			}
			sleeping, ok := sig.Body[0].(bool)
			if !ok {
				continue
			}
			if sleeping {
				slog.Info("system entering sleep")
				// d.dev is guarded by d.mu: the hotplug watcher closes and
				// replaces it on keyboard reattach, so an unlocked read here
				// races with that swap and can write to a closed descriptor.
				d.mu.Lock()
				if d.dev != nil {
					if err := aura.TurnOff(d.dev); err != nil {
						slog.Warn("failed to turn off lighting before sleep", "err", err)
					}
				}
				d.mu.Unlock()
				continue
			}
			slog.Info("system resumed from sleep, restoring volatile state")
			d.restoreVolatileState()
		}
	}
}

// restoreVolatileState reapplies all settings that are lost on sleep/resume:
// lighting, fan curves, TDP, and Curve Optimizer offsets.
func (d *Daemon) restoreVolatileState() {
	// hwMu before d.mu: the fan/TDP block below writes the same attributes the
	// socket handlers and the reconcile watcher do.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	// Both d.dev and d.state are guarded by d.mu, and applyLightingState reads
	// them directly, so hold the lock across it — the same discipline the socket
	// handlers use. cloneState is required because the plain struct copy would
	// alias the Devices map and the pointer fields still owned by d.state.
	d.mu.Lock()
	state := cloneState(d.state)
	if d.dev != nil {
		if err := d.applyLightingState(); err != nil {
			slog.Warn("resume: failed to restore lighting", "err", err)
		} else {
			slog.Info("resume: lighting restored")
		}
	}
	d.mu.Unlock()

	// The power source may have changed while the machine was asleep. There is
	// deliberately no autoswitch hook here: Go timers use CLOCK_MONOTONIC and do
	// not advance across suspend, so the power-source watcher's armed timer
	// fires promptly after resume and handles it. Duplicating the logic here
	// would race that watcher over hwMu and apply twice for one event. The cost
	// is a few seconds during which the pre-sleep profile is back in force,
	// which ApplyTDPSafely still keeps within the thermal floor.
	active, ok := state.ActiveCustomProfile()
	if !ok || active.Empty() {
		slog.Info("skipping volatile state restore (no custom profile active)", "profile", state.Profile)
		return
	}

	// Through the same helper as the socket command, the autoswitch watcher and
	// daemon startup. Resume used to carry its own copy of the apply sequence,
	// which is how the four drifted apart in the first place; every fix to the
	// ordering or the clearing rules now lands here too. hwMu is already held,
	// which is what applyCustomHW requires.
	slog.Info("resume: restoring custom profile", "profile", active.Name)
	d.applyCustomHW(active)
}
