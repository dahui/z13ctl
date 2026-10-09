package cli

// ppt.go — which kernel interface holds the PPT power limits, and what it
// accepts.
//
// Two interfaces write the same firmware settings. asus-armoury's
// firmware-attributes (mainline since 6.19, with GZ302EA bounds in 7.x) is the
// supported one. The asus-nb-wmi platform-device attributes are deprecated:
// they exist only under CONFIG_ASUS_WMI_DEPRECATED_ATTRS, so a distribution
// can drop them outright, and reading them logs a deprecation notice (issue
// #22). armoury is used whenever it exposes the limits; asus-nb-wmi is the
// fallback for kernels that do not.
//
// The two are never mixed within one read or write. They write the same WMI
// device IDs, so the last write wins, and each keeps its own cache of what was
// written to it — mixing them would leave both caches wrong.

import (
	"fmt"
	"log/slog"
	"os"

	"golang.org/x/sys/unix"

	"github.com/dahui/z13ctl/api"
)

// PPT backend names, as reported in api.TDPLimits.Backend.
const (
	PPTBackendArmoury = "asus-armoury"
	PPTBackendLegacy  = "asus-nb-wmi"
)

// pptLimit maps one api.TDPState field to its attribute on each backend.
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

// pptBound is one limit's accepted range on the active backend. A limit the
// backend does not expose has present == false and is neither read nor written.
// def is armoury's default_value — what its cache holds until written — and 0
// on asus-nb-wmi, whose cache starts at TDPMin instead.
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

// activePPT picks the backend: asus-armoury when its PL1 attribute has
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
// `z13ctl setup` keeps writing asus-nb-wmi's attributes, which it can, rather
// than failing on armoury's, which it cannot. Root can write either.
func activePPT() (pptBackend, error) {
	if b, ok := armouryPPT(); ok {
		return b, nil
	}
	if _, err := os.Stat(pptBasePath + "/ppt_pl1_spl"); err == nil {
		b := pptBackend{name: PPTBackendLegacy}
		for i, l := range pptLimits {
			_, statErr := os.Stat(pptBasePath + "/" + l.legacy)
			b.bounds[i] = pptBound{min: TDPMin, max: TDPMaxForced, present: statErr == nil}
		}
		return b, nil
	}
	return pptBackend{}, fmt.Errorf("no PPT power limit interface found (neither %s nor %s)",
		PPTBackendArmoury, PPTBackendLegacy)
}

// armouryPPT decides whether armoury is the backend: PL1's current_value
// exists and is writable, and its min_value and max_value read as a sane
// range — a kernel that registers the attributes without calibration data for
// this model has nothing to offer the fallback lacks. The other limits are
// present when their current_value exists; their bounds are not read here.
func armouryPPT() (pptBackend, bool) {
	b := pptBackend{name: PPTBackendArmoury}
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
// and every default_value — for the callers that need them: clamping, the
// stale-cache test, and the limits reported to clients. A limit whose bounds
// cannot be read is treated as absent. A no-op on asus-nb-wmi, whose bounds
// are fixed.
func (b pptBackend) withBounds() pptBackend {
	if b.name != PPTBackendArmoury {
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

// path returns the file holding limit i on this backend.
func (b pptBackend) path(i int) string {
	if b.name == PPTBackendArmoury {
		return sysFirmwareAttrDir + "/" + pptLimits[i].armoury + "/current_value"
	}
	return pptBasePath + "/" + pptLimits[i].legacy
}

// clamp returns s as this backend would hold it: each present limit clamped
// into its range, each absent one zeroed.
func (b pptBackend) clamp(s api.TDPState) api.TDPState {
	var out api.TDPState
	for i, l := range pptLimits {
		bd := b.bounds[i]
		if !bd.present {
			continue
		}
		v := *l.field(&s)
		v = max(bd.min, min(bd.max, v))
		*l.field(&out) = v
	}
	return out
}

// EffectiveTDP returns s as it will actually be written: clamped into the
// running kernel's range, with any limit the interface does not expose zeroed.
//
// Anything that compares a wanted TDP against a readback must compare this, not
// the stored value. A profile saved under asus-nb-wmi's 5 W floor reads back as
// armoury's 28 W, and the reconcile watcher comparing the raw 15 W would see
// drift on every tick and re-write the limit every two seconds forever. With no
// PPT interface at all s is returned unchanged.
func EffectiveTDP(s api.TDPState) api.TDPState {
	b, err := activePPT()
	if err != nil {
		return s
	}
	return b.withBounds().clamp(s)
}

// PPTCacheStale reports whether a readback is the kernel's own initial cache
// rather than anything written since boot, so it says nothing about the limits
// in force. On asus-nb-wmi that is the 5 W (TDPMin) the attributes start at; on
// armoury it is every exposed limit still at default_value, which armoury
// re-seeds per power source.
func PPTCacheStale(s api.TDPState) bool {
	b, err := activePPT()
	if err != nil {
		return false
	}
	return b.withBounds().stale(s)
}

func (b pptBackend) stale(s api.TDPState) bool {
	if b.name == PPTBackendLegacy {
		return s.PL1SPL == TDPMin
	}
	for i, l := range pptLimits {
		if bd := b.bounds[i]; bd.present && *l.field(&s) != bd.def {
			return false
		}
	}
	return true
}

// PPTLimits returns the ranges the running kernel accepts for PL1, PL2 and PL3,
// for validation and for clients drawing sliders (get-state's tdp_limits).
// SafeMax is ours, not the kernel's: above it PL1 needs force and the fan floor.
func PPTLimits() (api.TDPLimits, error) {
	b, err := activePPT()
	if err != nil {
		return api.TDPLimits{}, err
	}
	b = b.withBounds()
	r := func(i int) api.TDPRange { return api.TDPRange{Min: b.bounds[i].min, Max: b.bounds[i].max} }
	return api.TDPLimits{Backend: b.name, PL1: r(0), PL2: r(1), PL3: r(2), SafeMax: TDPMaxSafe}, nil
}

// LegacyPPTLimits is what validation falls back to with no PPT interface
// present (a dry run on another machine, CI), or with no limits to be had from
// the daemon: asus-nb-wmi's historical range. A write the kernel would refuse
// then fails on its own, which is the honest answer.
func LegacyPPTLimits() api.TDPLimits {
	r := api.TDPRange{Min: TDPMin, Max: TDPMaxForced}
	return api.TDPLimits{Backend: PPTBackendLegacy, PL1: r, PL2: r, PL3: r, SafeMax: TDPMaxSafe}
}

// ResolveTDP validates a requested TDP against the running kernel's limits and
// returns what will be written, with a note for each value it raised. It is the
// one validation the CLI and the daemon share; they used to carry separate
// copies of the range checks.
//
// pl1/pl2/pl3 override watts when non-zero, exactly as in TDPStateFor.
//
//   - PL1 outside the kernel's range is refused, as is PL1 above TDPMaxSafe
//     without force — the sustained limit is the one that decides heat, so it
//     is never silently changed.
//   - PL2 or PL3 above the kernel's maximum is refused.
//   - PL2 or PL3 below the kernel's minimum is raised to it, with a note.
//     armoury requires PL2 >= 32 and PL3 >= 45 on the GZ302EA, so a unified
//     `tdp --set 30` means 30/32/45 there; refusing it would make every low
//     unified value an error. The raised PL2 is not cosmetic: under load,
//     30/32/45 held 32 W for four minutes rather than settling to 30 W, so 32 W
//     is armoury's practical floor, and the note is what tells the user.
func ResolveTDP(watts, pl1, pl2, pl3 int, force bool) (api.TDPState, []string, error) {
	lim, err := PPTLimits()
	if err != nil {
		lim = LegacyPPTLimits()
	}
	return ResolveTDPWith(lim, watts, pl1, pl2, pl3, force)
}

// ResolveTDPWith is ResolveTDP against limits the caller already has — the
// daemon's, for a CLI that must not read the kernel's bounds itself while a
// daemon is running (on asus-armoury each read is a live ACPI call, which the
// daemon withholds while the EC is not answering).
func ResolveTDPWith(lim api.TDPLimits, watts, pl1, pl2, pl3 int, force bool) (api.TDPState, []string, error) {
	s := TDPStateFor(watts, pl1, pl2, pl3)

	if s.PL1SPL > TDPMaxSafe && !force {
		return s, nil, fmt.Errorf("PL1 %dW exceeds the safe sustained maximum (%dW); use --force to allow up to %dW",
			s.PL1SPL, TDPMaxSafe, lim.PL1.Max)
	}
	if hi := pl1Ceiling(lim, force); s.PL1SPL < lim.PL1.Min || s.PL1SPL > hi {
		return s, nil, fmt.Errorf("PL1 %dW out of range %d–%dW (%s)", s.PL1SPL, lim.PL1.Min, hi, lim.Backend)
	}

	var notes []string
	for _, v := range []struct {
		name  string
		value *int
		r     api.TDPRange
	}{
		{"PL2", &s.PL2SPPT, lim.PL2},
		{"PL3", &s.FPPT, lim.PL3},
	} {
		if *v.value > v.r.Max {
			return s, nil, fmt.Errorf("%s %dW out of range %d–%dW (%s)", v.name, *v.value, v.r.Min, v.r.Max, lim.Backend)
		}
		if *v.value < v.r.Min {
			notes = append(notes, fmt.Sprintf("%s raised from %dW to %dW, the kernel's minimum", v.name, *v.value, v.r.Min))
			*v.value = v.r.Min
		}
	}
	// APU and Platform sPPT follow PL2, including when it was raised.
	s.APUSPPT, s.PlatformSPPT = s.PL2SPPT, s.PL2SPPT
	return s, notes, nil
}

// pl1Ceiling is the highest PL1 a request may ask for: the kernel's maximum
// with force, and TDPMaxSafe (or the kernel's maximum, if lower) without.
func pl1Ceiling(lim api.TDPLimits, force bool) int {
	if force {
		return lim.PL1.Max
	}
	return min(TDPMaxSafe, lim.PL1.Max)
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
	return b.read()
}

func (b pptBackend) read() (api.TDPState, error) {
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

// SetTDPState writes s to the active interface, each limit clamped into the
// kernel's range (see EffectiveTDP) and any limit the interface does not expose
// skipped. No mirroring or derivation: use this when the exact values matter,
// notably when restoring StockProfilePPT.
//
// Clamping rather than refusing is for values that were valid when stored — a
// custom profile saved under asus-nb-wmi's 5 W floor still applies, at
// armoury's minimum, and the stored profile is left as the user saved it. New
// values are validated up front by ResolveTDP.
func SetTDPState(s api.TDPState) error {
	b, err := activePPT()
	if err != nil {
		return err
	}
	b = b.withBounds()
	out := b.clamp(s)
	for i, l := range pptLimits {
		if b.bounds[i].present && *l.field(&out) != *l.field(&s) {
			slog.Info("power limits clamped to the kernel's range", "backend", b.name,
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
	var plan []PPTWrite
	for i, l := range pptLimits {
		if b.bounds[i].present {
			plan = append(plan, PPTWrite{Path: b.path(i), Watts: *l.field(&out)})
		}
	}
	return plan
}
