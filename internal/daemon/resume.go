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

	"github.com/dahui/z13ctl/internal/cli"
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
	d.mu.Lock()
	state := d.state
	d.mu.Unlock()

	// Restore lighting (lost on sleep regardless of profile).
	if d.dev != nil {
		if err := d.applyLightingState(); err != nil {
			slog.Warn("resume: failed to restore lighting", "err", err)
		} else {
			slog.Info("resume: lighting restored")
		}
	}

	if state.Profile != "custom" {
		slog.Info("skipping volatile state restore (stock profile active)", "profile", state.Profile)
		return
	}

	// Restore fan curve.
	if fc := state.FanCurve; fc != nil && fc.Mode == 1 && len(fc.Points) == 8 {
		if err := cli.SetBothFanCurves(fc.Points); err != nil {
			slog.Warn("resume: failed to restore fan curve", "err", err)
		} else {
			slog.Info("resume: fan curve restored")
		}
	}

	// Restore TDP.
	if t := state.TDP; t != nil {
		if t.PL1SPL > cli.TDPMaxSafe {
			if err := cli.SetBothFanCurves(cli.HighTDPFanCurve()); err != nil {
				slog.Warn("resume: failed to set high-TDP fan curve", "err", err)
			}
		}
		if err := cli.SetTDP(0, t.PL1SPL, t.PL2SPPT, t.FPPT); err != nil {
			slog.Warn("resume: failed to restore TDP", "err", err)
		} else {
			slog.Info("resume: TDP restored", "pl1", t.PL1SPL, "pl2", t.PL2SPPT, "pl3", t.FPPT)
		}
	}

	// Restore undervolt.
	if uv := state.Undervolt; uv != nil && cli.SMUAvailable() {
		if err := cli.SetCurveOptimizer(uv.CPUCO, uv.IGPUCO); err != nil {
			slog.Warn("resume: failed to restore undervolt", "err", err)
		} else {
			slog.Info("resume: undervolt restored", "cpu", uv.CPUCO, "igpu", uv.IGPUCO)
		}
	}
}
