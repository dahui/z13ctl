package amd

// power.go — package power limits through the SMU (the "amd-smu" power method).
//
// voltaire's generic AMD power driver, for machines with no vendor interface
// for their limits (no asus-armoury, no firmware attributes): STAPM, fast and
// slow PPT set directly over the MP1 mailbox, the messages ryzenadj sends.
// Measured on the OneXPlayer X2 Mini Pro (2026-11-17): 25/25/25 held 25.0 W
// under load and read back exactly; amd-pmf's low-power caps below any SMU
// limit; and the EC writes its own preset on every charger edge.
//
// Two rules from oxp-tdpd shape it. Every command goes through
// SendSMUCommand's lock, the same one the Curve Optimizer uses: the driver
// shares one argument buffer across mailboxes. And reading back is a cache,
// never an SMU transaction per request — so Read models the hardware from what
// is known (the last write, the EC's presets, the power source) instead of
// reading the PM table.

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// The MP1 messages that set each limit, in milliwatts, on every CPU in
// coPlatforms (ryzenadj 0.19.0 lib/api.c set_stapm_limit, set_fast_limit and
// set_slow_limit: one case covers Renoir through Strix Halo). Raven-class
// parts (0x1a–0x1c) and Dragon/Fire Range are not in coPlatforms, so they are
// refused here too.
const (
	pptCmdSTAPM = 0x14
	pptCmdFast  = 0x15
	pptCmdSlow  = 0x16
)

// pptMinGap is the shortest interval between two limit commits (oxp-tdpd:
// never more often than every 150 ms). Var so tests can shorten it.
var pptMinGap = 150 * time.Millisecond

// setPPT writes the three limits, in watts. A CPU with no known command is
// refused before any mailbox write; a command the SMU does not accept stops
// the sequence and is reported.
func setPPT(stapm, fast, slow int) error {
	p, err := coPlatformFor()
	if err != nil {
		return fmt.Errorf("power limits: %w", err)
	}
	for _, c := range []struct {
		name string
		cmd  uint32
		w    int
	}{{"STAPM", pptCmdSTAPM, stapm}, {"fast PPT", pptCmdFast, fast}, {"slow PPT", pptCmdSlow, slow}} {
		code, _, err := sendSMUCommand(MailboxMP1, c.cmd, [6]uint32{uint32(c.w) * 1000})
		if err != nil {
			return fmt.Errorf("setting %s on %s: %w", c.name, p.Name, err)
		}
		if code != SMUReturnOK {
			return fmt.Errorf("setting %s on %s: %w", c.name, p.Name, smuResponseError(code))
		}
	}
	return nil
}

// SMUMissingReason says why SMU power control is unavailable, or "" when the
// ryzen_smu module is loaded. A stat and a cpuinfo read; nothing reaches the
// SMU. The fork a CPU needs is named where it is known (ForkNote), since
// installing the wrong one looks like success until the first write.
func SMUMissingReason() string {
	return SMUMissingReasonFor("TDP control")
}

// SMUMissingReasonFor is SMUMissingReason for any capability that goes
// through the SMU, named by what ("Undervolting").
func SMUMissingReasonFor(what string) string {
	if SMUAvailable() {
		return ""
	}
	reason := what + " needs the ryzen_smu kernel module, which is not loaded"
	if p, err := coPlatformFor(); err == nil && p.ForkNote != "" {
		reason += "; " + p.ForkNote
	}
	return reason
}

// SMUPower is driver.PowerLimiter over the SMU. PL1 is STAPM, PL2 the slow
// PPT and PL3 (FPPT) the fast PPT; there are no APU/Platform sPPT mirrors.
type SMUPower struct {
	ac, battery driver.PowerEnvelope
	profile     string                  // the firmware profile whose stock row the firmware holds
	onAC        func() (bool, error)    // the power source; injected (linux.OnACPower)
	write       func(s, f, l int) error // setPPT; a field so tests replace it

	mu        sync.Mutex
	last      *api.TDPState // what voltaire last wrote, nil before the first write
	lastAC    bool          // the source it was written on
	lastWrite time.Time
}

// NewSMUPower returns the limiter. ac and battery are the device data's
// envelopes for each source (the battery one capped at power.tdp_max_battery,
// its stock rows the firmware's battery preset); profile names the row the
// firmware holds when voltaire has written nothing. Pure: nothing is read or
// written until it is used.
func NewSMUPower(ac, battery driver.PowerEnvelope, profile string, onAC func() (bool, error)) *SMUPower {
	for _, e := range []*driver.PowerEnvelope{&ac, &battery} {
		e.Interface = "ryzen_smu"
		e.NoSPPTMirrors = true
		// The fast limit may go as high as the firmware's own preset boosts
		// (the OXP's EC uses 100 W fast on AC), so a reset can write it back
		// exactly; the sustained limits keep the device's range.
		pl3 := driver.PowerRange{Min: e.TDPMin, Max: e.TDPMaxForced}
		if row, ok := e.StockProfilePPT[profile]; ok && row.FPPT > pl3.Max {
			pl3.Max = row.FPPT
		}
		e.PL2 = driver.PowerRange{Min: e.TDPMin, Max: e.TDPMaxForced}
		e.PL3 = pl3
	}
	return &SMUPower{ac: ac, battery: battery, profile: profile, onAC: onAC, write: setPPT}
}

// onACNow is the source the limits are judged against. An unknown source
// counts as battery: its cap is the lower one.
func (p *SMUPower) onACNow() bool {
	if p.onAC == nil {
		return true
	}
	on, err := p.onAC()
	return err == nil && on
}

func (p *SMUPower) envFor(onAC bool) driver.PowerEnvelope {
	if onAC {
		return p.ac
	}
	return p.battery
}

// Envelope is the current source's envelope: the battery cap applies the
// moment the charger is unplugged. Reads only the power source.
func (p *SMUPower) Envelope() driver.PowerEnvelope { return p.envFor(p.onACNow()) }

// DeviceEnvelope is the AC envelope from device data, touching nothing
// (driver.DeviceEnvelope).
func (p *SMUPower) DeviceEnvelope() driver.PowerEnvelope { return p.ac }

// Read reports the limits in force without an SMU transaction: what voltaire
// last wrote, unless the power source has changed since — the EC writes its
// own preset on every charger edge (measured: 25/25/25 became 50/60/50 on
// unplug and 80/100/85 on replug, ~2 s before ACAD/online changed) — in which
// case, as before the first write, the firmware's row for the current source.
// So the daemon's drift check sees a charger edge as the drift it is.
func (p *SMUPower) Read() (api.TDPState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	onAC := p.onACNow()
	if p.last != nil && p.lastAC == onAC {
		return *p.last, nil
	}
	row, ok := p.envFor(onAC).StockProfilePPT[p.profile]
	if !ok {
		return api.TDPState{}, fmt.Errorf("power limits: no firmware row for profile %q", p.profile)
	}
	return row, nil
}

// PlanWrites is driver.WritePlanner: the three SMU messages Apply(t) would
// send, clamped as Apply clamps.
func (p *SMUPower) PlanWrites(t api.TDPState) []string {
	p.mu.Lock()
	env := p.envFor(p.onACNow())
	p.mu.Unlock()
	return []string{
		fmt.Sprintf("send SMU message 0x%02X (STAPM) = %d W", pptCmdSTAPM, clamp(t.PL1SPL, env.TDPMin, env.TDPMaxForced)),
		fmt.Sprintf("send SMU message 0x%02X (fast PPT) = %d W", pptCmdFast, clamp(t.FPPT, env.PL3.Min, env.PL3.Max)),
		fmt.Sprintf("send SMU message 0x%02X (slow PPT) = %d W", pptCmdSlow, clamp(t.PL2SPPT, env.PL2.Min, env.PL2.Max)),
	}
}

// Apply writes the limits, clamped into the current source's ranges (a value
// saved on AC is applied at the battery cap on battery). A write identical to
// the one already in force on this source is skipped, and commits are spaced
// at least pptMinGap apart: every SMU command races the GPU driver for the
// mailbox, and oxp-tdpd froze a game by writing on every slider event.
func (p *SMUPower) Apply(t api.TDPState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	onAC := p.onACNow()
	env := p.envFor(onAC)
	want := api.TDPState{
		PL1SPL:  clamp(t.PL1SPL, env.TDPMin, env.TDPMaxForced),
		PL2SPPT: clamp(t.PL2SPPT, env.PL2.Min, env.PL2.Max),
		FPPT:    clamp(t.FPPT, env.PL3.Min, env.PL3.Max),
	}
	if want.PL1SPL != t.PL1SPL || want.PL2SPPT != t.PL2SPPT || want.FPPT != t.FPPT {
		slog.Info("power limits clamped to this power source's range",
			"requested", fmt.Sprintf("%d/%d/%d", t.PL1SPL, t.PL2SPPT, t.FPPT),
			"applied", fmt.Sprintf("%d/%d/%d", want.PL1SPL, want.PL2SPPT, want.FPPT), "ac", onAC)
	}
	if p.last != nil && p.lastAC == onAC && *p.last == want {
		return nil
	}
	if wait := pptMinGap - time.Since(p.lastWrite); wait > 0 {
		time.Sleep(wait)
	}
	err := p.write(want.PL1SPL, want.FPPT, want.PL2SPPT)
	p.lastWrite = time.Now()
	if err != nil {
		p.last = nil // unknown now: report the firmware's row until a write succeeds
		return err
	}
	p.last, p.lastAC = &want, onAC
	return nil
}

// Forget drops what voltaire knows it wrote, so the next Apply writes even
// an unchanged value and Read reports the firmware's row until then
// (driver.PowerForgetter). The daemon calls it on resume: whether the SMU
// keeps written limits through s2idle is the hardware's to say, and a skipped
// re-apply after a resume that lost them would leave the firmware's limits in
// force while voltaire reported its own.
func (p *SMUPower) Forget() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = nil
}

func clamp(v, lo, hi int) int {
	switch {
	case hi > 0 && v > hi:
		return hi
	case v < lo:
		return lo
	}
	return v
}
