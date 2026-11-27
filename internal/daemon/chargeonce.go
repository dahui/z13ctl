package daemon

// chargeonce.go — a one-time charge to full (G-Helper's "charge to 100 %
// once"): the charge limit is lifted to the device's maximum until the pack
// is full, and then the user's limit goes back.
//
// state.Battery stays the user's limit the whole time, so every client keeps
// showing the limit it will return to; state.ChargeOnce is what says the
// hardware is held at the maximum instead. That makes the startup restore and
// the end of the charge one rule: the hardware gets the maximum while
// ChargeOnce is set and state.Battery otherwise.
//
// As in G-Helper it lasts until the pack is full, not until the charger is
// unplugged: a charge cut short by moving the machine carries on at the next
// plug-in. Setting a limit cancels it (handleBatteryLimit), as G-Helper's
// slider does.

import (
	"context"
	"log/slog"
	"time"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// chargeOncePollInterval is how often the watcher checks whether the pack is
// full while a one-time charge is in progress. A charge takes an hour or
// more, so a minute late is nothing, and the read is a sysfs file.
const chargeOncePollInterval = time.Minute

// chargeOnceTopOff is how long a pack may sit at 99 % or more on mains, still
// saying it is charging, before the charge counts as done. The kernel's
// capacity is energy over full rounded down, so it reads 100 only when the
// two are equal, and some packs trickle the last fraction of a percent for
// as long as they are plugged in without ever reporting full. Without this
// the maximum would stay in place indefinitely, which is the opposite of
// what the user's limit is for.
const chargeOnceTopOff = 30 * time.Minute

// batteryFull reports whether a one-time charge has done its job. Capacity
// reads 100 on most packs; some firmware tops off at 99 % and reports Full or
// stops ("not charging") on mains there, which is as full as it will get.
func batteryFull(st driver.BatteryStatus) bool {
	switch {
	case st.Capacity >= 100, st.State == driver.BatteryStateFull:
		return true
	case st.Capacity >= 99 && st.State == driver.BatteryStateNotCharging && st.ACKnown && st.OnAC:
		return true
	}
	return false
}

// chargeLimitControl is the battery driver when the charge limit is ours to
// set right now: nil without one, and nil while it is yielded to Steam in
// Gaming Mode (the driver stays, the capability is off).
func (d *Daemon) chargeLimitControl() driver.Battery {
	if d.hw.Battery == nil || !d.hw.Battery.Caps().ChargeLimit {
		return nil
	}
	return d.hw.Battery
}

func (d *Daemon) handleChargeOnce(req request) response {
	bat := d.chargeLimitControl()
	if bat == nil {
		return response{OK: false, Error: "batterylimit: " + d.hw.Missing("charge-limit", "no battery charge control on this device")}
	}
	var on bool
	switch req.Set {
	case "on", "true", "1":
		on = true
	case "off", "false", "0":
	default:
		return response{OK: false, Error: `batterylimit-full-once: set must be "on" or "off"`}
	}

	d.hwMu.Lock()
	defer d.hwMu.Unlock()
	d.mu.Lock()
	active, limit := d.state.ChargeOnce, d.state.Battery
	d.mu.Unlock()

	if !on {
		if active {
			d.endChargeOnceLocked("cancelled")
		}
		return response{OK: true}
	}
	if active {
		return response{OK: true}
	}
	caps := bat.Caps()
	if limit == 0 {
		// No limit set through voltaire: the one in force is whatever the
		// hardware holds (another tool's, or none), and that is the one to
		// return to. The driver reports "never written" as the maximum.
		if l, err := bat.ChargeLimit(); err == nil {
			limit = l
		}
	}
	if limit == 0 || limit >= caps.ChargeLimitMax {
		return response{OK: false, Error: "batterylimit: no charge limit is set, so the battery already charges to full"}
	}
	if st, err := bat.Status(); err == nil && batteryFull(st) {
		return response{OK: false, Error: "batterylimit: the battery is already full"}
	}
	if err := bat.SetChargeLimit(caps.ChargeLimitMax); err != nil {
		return response{OK: false, Error: "batterylimit: " + err.Error()}
	}
	slog.Info("charge once: charging to full", "limit", caps.ChargeLimitMax, "then", limit)
	d.chargeOnceTopSince = time.Time{}
	d.mu.Lock()
	d.state.ChargeOnce = true
	d.state.Battery = limit
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
	return response{OK: true}
}

// endChargeOnceLocked puts the user's limit back and clears the flag. The
// caller holds hwMu and not d.mu. A failed write keeps the flag, so the
// watcher tries again on its next tick rather than leaving the limit lifted
// with nothing to say so.
func (d *Daemon) endChargeOnceLocked(why string) {
	d.mu.Lock()
	limit := d.state.Battery
	d.mu.Unlock()
	if bat := d.chargeLimitControl(); bat != nil && limit > 0 {
		if err := bat.SetChargeLimit(limit); err != nil {
			slog.Warn("charge once: could not restore the charge limit", "limit", limit, "err", err)
			return
		}
	}
	slog.Info("charge once: done", "why", why, "limit", limit)
	d.chargeOnceTopSince = time.Time{}
	d.mu.Lock()
	d.state.ChargeOnce = false
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
}

// watchChargeOnce runs until ctx is done, ending a one-time charge once the
// pack is full.
func (d *Daemon) watchChargeOnce(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(chargeOncePollInterval):
		}
		d.chargeOnceTick(time.Now())
	}
}

// chargeOnceTick is one check. It stands down while the machine is entering
// sleep and while a resume has left the EC unresponsive, as every path into
// the EC does (docs/decisions/0113, 0114), and while the limit is Steam's.
// now is the time of the check (the tests step it).
func (d *Daemon) chargeOnceTick(now time.Time) {
	d.mu.Lock()
	active := d.state.ChargeOnce
	d.mu.Unlock()
	bat := d.chargeLimitControl()
	if !active || bat == nil {
		return
	}
	d.hwMu.Lock()
	defer d.hwMu.Unlock()
	d.mu.Lock()
	active, suspending, wedged := d.state.ChargeOnce, d.suspending, d.ecWedged
	d.mu.Unlock()
	if !active || suspending || wedged {
		return
	}
	st, err := bat.Status()
	if err != nil {
		return
	}
	if batteryFull(st) {
		d.endChargeOnceLocked("full")
		return
	}
	if st.Capacity < 99 || !st.ACKnown || !st.OnAC {
		d.chargeOnceTopSince = time.Time{}
		return
	}
	if d.chargeOnceTopSince.IsZero() {
		d.chargeOnceTopSince = now
		return
	}
	if now.Sub(d.chargeOnceTopSince) >= chargeOnceTopOff {
		d.endChargeOnceLocked("topped off")
	}
}
