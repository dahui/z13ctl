package asusz13

// tdp.go — which kernel interface holds the PPT power limits, what it accepts,
// and the sysfs I/O for it.
//
// Two interfaces write the same firmware settings. asus-armoury's
// firmware-attributes (mainline since 6.19, with GZ302EA bounds in 7.x) is the
// supported one. The asus-nb-wmi platform-device attributes are deprecated:
// they exist only under CONFIG_ASUS_WMI_DEPRECATED_ATTRS, so a distribution
// can drop them outright, and reading them logs a deprecation notice (z13ctl
// issue #22). armoury is used whenever it exposes the limits; asus-nb-wmi is
// the fallback for kernels that do not.
//
// The two are never mixed within one read or write. They write the same WMI
// device IDs, so the last write wins, and each keeps its own cache of what was
// written to it — mixing them would leave both caches wrong.
//
// This is deliberately just the sysfs layer. The safety limits, the stock
// per-profile PPT table, and the high-TDP floor curve live in the device data
// (internal/device/devices/asus-rog-flow-z13-2025.toml), and every rule about
// them — fail-closed apply ordering, the stale-cache substitution, the floor,
// resolving a request against the ranges — lives in internal/safety, reached
// through the engine the device registry assembles around NewPowerLimiter.

import (
	"fmt"
	"log/slog"
	"os"

	"golang.org/x/sys/unix"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// PPT interface names, as reported in driver.PowerEnvelope.Interface.
const (
	PPTInterfaceArmoury = "asus-armoury"
	PPTInterfaceLegacy  = "asus-nb-wmi"
)

// pptLimit maps one api.TDPState field to its attribute on each interface.
type pptLimit struct {
	name    string // for messages
	legacy  string // asus-nb-wmi attribute file
	armoury string // asus-armoury attribute directory
	field   func(*api.TDPState) *int
}

// pptLimits lists the five limits in write order. armoury names PL3
// ppt_pl3_fppt where asus-nb-wmi says ppt_fppt; the GZ302EA exposes no APU or
// Platform sPPT through armoury at all.
var pptLimits = []pptLimit{
	{"PL1", "ppt_pl1_spl", "ppt_pl1_spl", func(s *api.TDPState) *int { return &s.PL1SPL }},
	{"PL2", "ppt_pl2_sppt", "ppt_pl2_sppt", func(s *api.TDPState) *int { return &s.PL2SPPT }},
	{"PL3", "ppt_fppt", "ppt_pl3_fppt", func(s *api.TDPState) *int { return &s.FPPT }},
	{"APU sPPT", "ppt_apu_sppt", "ppt_apu_sppt", func(s *api.TDPState) *int { return &s.APUSPPT }},
	{"Platform sPPT", "ppt_platform_sppt", "ppt_platform_sppt", func(s *api.TDPState) *int { return &s.PlatformSPPT }},
}

// pptBound is one limit on the active interface. A limit the interface does not
// expose has present == false and is neither read nor written. min, max and def
// are armoury's min_value, max_value and default_value; asus-nb-wmi reports no
// bounds, so they are 0 there and nothing is clamped.
type pptBound struct {
	min, max, def int
	present       bool
}

// pptBackend is the interface in use and each limit's bounds on it, indexed
// like pptLimits.
type pptBackend struct {
	name   string
	bounds [5]pptBound
}

// activePPT picks the interface: asus-armoury when its PL1 attribute has
// readable bounds and a current_value this process may write, else asus-nb-wmi
// when it has ppt_pl1_spl, else an error. Only PL1's bounds are read here; the
// rest load on demand (withBounds), because on armoury every read is a live
// ACPI call — the kernel evaluates the AC adapter's _PSR to choose its AC or
// battery table — and the reconcile watcher reads the limits every two seconds.
//
// It is decided on every call rather than once per process: armoury answers
// from whichever table matches the power source now, and a stat per call is
// cheap next to the WMI write it guards.
//
// Writability is part of the choice so an upgrade does not break TDP control
// before the new permission grant is applied: an install that has not re-run
// `voltaire setup` keeps writing asus-nb-wmi's attributes, which it can, rather
// than failing on armoury's, which it cannot. Root can write either.
func activePPT() (pptBackend, error) {
	if b, ok := armouryPPT(); ok {
		return b, nil
	}
	if _, err := os.Stat(pptBasePath + "/ppt_pl1_spl"); err == nil {
		b := pptBackend{name: PPTInterfaceLegacy}
		for i, l := range pptLimits {
			_, statErr := os.Stat(pptBasePath + "/" + l.legacy)
			b.bounds[i] = pptBound{present: statErr == nil}
		}
		return b, nil
	}
	return pptBackend{}, fmt.Errorf("no PPT power limit interface found (neither %s nor %s)",
		PPTInterfaceArmoury, PPTInterfaceLegacy)
}

// armouryPPT decides whether armoury is the interface: PL1's current_value
// exists and is writable, and its min_value and max_value read as a sane
// range — a kernel that registers the attributes without calibration data for
// this model has nothing to offer the fallback lacks. The other limits are
// present when their current_value exists; their bounds are not read here.
func armouryPPT() (pptBackend, bool) {
	b := pptBackend{name: PPTInterfaceArmoury}
	dir := sysFirmwareAttrDir + "/" + pptLimits[0].armoury
	if unix.Access(dir+"/current_value", unix.W_OK) != nil {
		return b, false
	}
	lo, loErr := readIntFile(dir + "/min_value")
	hi, hiErr := readIntFile(dir + "/max_value")
	if loErr != nil || hiErr != nil || lo <= 0 || hi < lo {
		return b, false
	}
	b.bounds[0] = pptBound{min: lo, max: hi, present: true}
	for i := 1; i < len(pptLimits); i++ {
		if _, err := os.Stat(sysFirmwareAttrDir + "/" + pptLimits[i].armoury + "/current_value"); err == nil {
			b.bounds[i].present = true
		}
	}
	return b, true
}

// withBounds reads the rest of armoury's bounds — every present limit's range
// and every default_value — for the callers that need them: the envelope, and
// clamping a write. A limit whose bounds cannot be read is treated as absent.
// A no-op on asus-nb-wmi, which reports no bounds.
func (b pptBackend) withBounds() pptBackend {
	if b.name != PPTInterfaceArmoury {
		return b
	}
	for i, l := range pptLimits {
		if !b.bounds[i].present {
			continue
		}
		dir := sysFirmwareAttrDir + "/" + l.armoury
		if i > 0 {
			lo, loErr := readIntFile(dir + "/min_value")
			hi, hiErr := readIntFile(dir + "/max_value")
			if loErr != nil || hiErr != nil || lo <= 0 || hi < lo {
				b.bounds[i] = pptBound{}
				continue
			}
			b.bounds[i].min, b.bounds[i].max = lo, hi
		}
		b.bounds[i].def, _ = readIntFile(dir + "/default_value")
	}
	return b
}

// path returns the file holding limit i on this interface.
func (b pptBackend) path(i int) string {
	if b.name == PPTInterfaceArmoury {
		return sysFirmwareAttrDir + "/" + pptLimits[i].armoury + "/current_value"
	}
	return pptBasePath + "/" + pptLimits[i].legacy
}

// clamp returns s as this interface would hold it: each limit clamped into the
// range the kernel reports for it (where it reports one), each absent one
// zeroed.
func (b pptBackend) clamp(s api.TDPState) api.TDPState {
	var out api.TDPState
	for i, l := range pptLimits {
		bd := b.bounds[i]
		if !bd.present {
			continue
		}
		v := *l.field(&s)
		if bd.max > 0 {
			v = max(bd.min, min(bd.max, v))
		}
		*l.field(&out) = v
	}
	return out
}

// envelope returns base with what this interface reports laid over it. On
// armoury that is the kernel's own ranges — PL1's replacing the device file's
// TDPMin/TDPMaxForced — its defaults as the initial cache, and the absence of
// APU/Platform sPPT. asus-nb-wmi reports no bounds, so the device file's stand,
// and its initial cache is PL1 at TDPMin (the 5 W the attributes come up at).
func (b pptBackend) envelope(base driver.PowerEnvelope) driver.PowerEnvelope {
	env := base
	env.Interface = b.name
	if b.name != PPTInterfaceArmoury {
		env.Initial = api.TDPState{PL1SPL: base.TDPMin}
		return env
	}
	env.TDPMin, env.TDPMaxForced = b.bounds[0].min, b.bounds[0].max
	if bd := b.bounds[1]; bd.present {
		env.PL2 = driver.PowerRange{Min: bd.min, Max: bd.max}
	}
	if bd := b.bounds[2]; bd.present {
		env.PL3 = driver.PowerRange{Min: bd.min, Max: bd.max}
	}
	env.NoSPPTMirrors = !b.bounds[3].present && !b.bounds[4].present
	env.Initial = api.TDPState{}
	for i, l := range pptLimits {
		if b.bounds[i].present {
			*l.field(&env.Initial) = b.bounds[i].def
		}
	}
	return env
}

// FindPPTBasePath returns the sysfs path to the asus-nb-wmi platform device.
func FindPPTBasePath() string {
	return pptBasePath
}

// FindPPTPath returns the full sysfs path for an asus-nb-wmi PPT attribute.
func FindPPTPath(attr string) string {
	return FindPPTBasePath() + "/" + attr
}

// ReadPPT reads a single asus-nb-wmi PPT value (watts) from sysfs.
func ReadPPT(attr string) (int, error) {
	return readIntFile(FindPPTPath(attr))
}

// ReadAllPPT reads the power limits from the active interface. A limit the
// interface does not expose reads as 0. On either interface the values are the
// driver's cache of what was last written — never proof that a limit is in
// force (issue #22).
func ReadAllPPT() (api.TDPState, error) {
	b, err := activePPT()
	if err != nil {
		return api.TDPState{}, err
	}
	var s api.TDPState
	for i, l := range pptLimits {
		if !b.bounds[i].present {
			continue
		}
		v, err := readIntFile(b.path(i))
		if err != nil {
			return s, fmt.Errorf("reading %s (%s): %w", l.name, b.name, err)
		}
		*l.field(&s) = v
	}
	return s, nil
}

// SetTDPState writes s to the active interface, with no mirroring or
// derivation: restoring a stock row needs the exact values (measured
// APU/Platform sPPT do not equal PL2), and everything above the safe maximum
// must go through safety.Engine.ApplyTDPSafely rather than calling this
// directly.
//
// On armoury each limit is clamped into the kernel's range first, which would
// otherwise refuse the write, and a limit it does not expose is skipped.
// Clamping rather than refusing is for values that were valid when stored — a
// custom profile saved under asus-nb-wmi's 5 W floor still applies, at
// armoury's minimum, and the stored profile is left as the user saved it. New
// values are validated up front by safety.ResolveTDP.
func SetTDPState(s api.TDPState) error {
	b, err := activePPT()
	if err != nil {
		return err
	}
	b = b.withBounds()
	out := b.clamp(s)
	for i, l := range pptLimits {
		if b.bounds[i].present && *l.field(&out) != *l.field(&s) {
			slog.Info("power limits clamped to the kernel's range", "interface", b.name,
				"requested", fmt.Sprintf("%d/%d/%d", s.PL1SPL, s.PL2SPPT, s.FPPT),
				"written", fmt.Sprintf("%d/%d/%d", out.PL1SPL, out.PL2SPPT, out.FPPT))
			break
		}
	}
	for i, l := range pptLimits {
		if !b.bounds[i].present {
			continue
		}
		if err := writeIntFile(b.path(i), *l.field(&out)); err != nil {
			return fmt.Errorf("writing %s (%s): %w", l.name, b.name, err)
		}
	}
	return nil
}

// PPTWrite is one attribute write SetTDPState would make.
type PPTWrite struct {
	Path  string
	Watts int
}

// PlanTDPWrites returns the writes SetTDPState(s) would make, in order, on the
// active interface — nil when there is none. It exists for the dry run, which
// takes the plan as a parameter rather than discovering the interface itself so
// its tests do not depend on the machine running them.
func PlanTDPWrites(s api.TDPState) []PPTWrite {
	b, err := activePPT()
	if err != nil {
		return nil
	}
	b = b.withBounds()
	out := b.clamp(s)
	var w []PPTWrite
	for i, l := range pptLimits {
		if b.bounds[i].present {
			w = append(w, PPTWrite{Path: b.path(i), Watts: *l.field(&out)})
		}
	}
	return w
}
