package safety

// limits.go — the ranges a device's power-limit interface accepts: resolving a
// requested TDP into the values to write, what a stored TDP becomes when it is
// written, and whether a readback is the interface's untouched cache.
//
// The ranges are the envelope's, and a driver that can read the kernel's own
// bounds puts them there (asusz13 does on asus-armoury: PL1 28–80, PL2 32–92,
// PL3 45–93 W on the GZ302EA). Device data alone gives one range for all three.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// PL1Range is the range the sustained limit may take, force or not.
func PL1Range(env driver.PowerEnvelope) driver.PowerRange {
	return driver.PowerRange{Min: env.TDPMin, Max: env.TDPMaxForced}
}

// PL2Range is the short-boost limit's range; PL1's when the envelope sets none.
func PL2Range(env driver.PowerEnvelope) driver.PowerRange {
	if env.PL2.IsZero() {
		return PL1Range(env)
	}
	return env.PL2
}

// PL3Range is the fast-boost limit's range; PL1's when the envelope sets none.
func PL3Range(env driver.PowerEnvelope) driver.PowerRange {
	if env.PL3.IsZero() {
		return PL1Range(env)
	}
	return env.PL3
}

// PL1Ceiling is the highest PL1 a request may ask for: the interface's maximum
// with force, and TDPMaxSafe (or the interface's maximum, if lower) without.
func PL1Ceiling(env driver.PowerEnvelope, force bool) int {
	if force {
		return env.TDPMaxForced
	}
	return min(env.TDPMaxSafe, env.TDPMaxForced)
}

// TDPStateFor builds the state a TDP request describes: watts for every limit,
// with pl1/pl2/pl3 overriding it when non-zero, and APU/Platform sPPT following
// PL2.
func TDPStateFor(watts, pl1, pl2, pl3 int) api.TDPState {
	if pl1 == 0 {
		pl1 = watts
	}
	if pl2 == 0 {
		pl2 = watts
	}
	if pl3 == 0 {
		pl3 = watts
	}
	return api.TDPState{PL1SPL: pl1, PL2SPPT: pl2, FPPT: pl3, APUSPPT: pl2, PlatformSPPT: pl2}
}

// ResolveTDP validates a requested TDP against the envelope and returns what
// will be stored and written, with a note for each value it raised. It is the
// one validation the CLI and the daemon share; they used to carry separate
// copies of the range checks.
//
//   - PL1 outside its range is refused, as is PL1 above TDPMaxSafe without
//     force — the sustained limit is the one that decides heat, so it is never
//     silently changed.
//   - PL2 or PL3 above its maximum is refused.
//   - PL2 or PL3 below its minimum is raised to it, with a note. asus-armoury
//     requires PL2 >= 32 and PL3 >= 45 on the GZ302EA, so a unified 30 W means
//     30/32/45 there; refusing it would make every low unified value an error.
//     The raised PL2 is not cosmetic: under load, 30/32/45 held 32 W for four
//     minutes rather than settling to 30 W (measured 2026-10-08), so 32 W is
//     armoury's practical floor, and the note is what tells the user.
//
// APU and Platform sPPT follow PL2, including when it was raised, even on an
// interface that has neither: the stored profile stays valid on one that does,
// and EffectiveTDP drops them where they are absent.
func ResolveTDP(env driver.PowerEnvelope, watts, pl1, pl2, pl3 int, force bool) (api.TDPState, []string, error) {
	s := TDPStateFor(watts, pl1, pl2, pl3)
	where := ""
	if env.Interface != "" {
		where = " (" + env.Interface + ")"
	}

	if s.PL1SPL > env.TDPMaxSafe && !force {
		return s, nil, fmt.Errorf("PL1 %dW exceeds the safe sustained maximum (%dW); use force to allow up to %dW",
			s.PL1SPL, env.TDPMaxSafe, env.TDPMaxForced)
	}
	if hi := PL1Ceiling(env, force); s.PL1SPL < env.TDPMin || s.PL1SPL > hi {
		return s, nil, fmt.Errorf("PL1 %dW out of range %d–%dW%s", s.PL1SPL, env.TDPMin, hi, where)
	}

	var notes []string
	for _, v := range []struct {
		name  string
		value *int
		r     driver.PowerRange
	}{
		{"PL2", &s.PL2SPPT, PL2Range(env)},
		{"PL3", &s.FPPT, PL3Range(env)},
	} {
		if *v.value > v.r.Max {
			return s, nil, fmt.Errorf("%s %dW out of range %d–%dW%s", v.name, *v.value, v.r.Min, v.r.Max, where)
		}
		if *v.value < v.r.Min {
			notes = append(notes, fmt.Sprintf("%s raised from %dW to %dW, the minimum", v.name, *v.value, v.r.Min))
			*v.value = v.r.Min
		}
	}
	s.APUSPPT, s.PlatformSPPT = s.PL2SPPT, s.PL2SPPT
	return s, notes, nil
}

// EffectiveTDP returns s as the interface will hold it once written: each limit
// clamped into its range, and APU/Platform sPPT zeroed where the interface has
// neither. It is what PowerLimiter.Apply writes.
//
// Anything that compares a wanted TDP against a readback must compare this, not
// the stored value. A profile saved under asus-nb-wmi's 5 W floor reads back as
// armoury's 28 W, and the reconcile watcher comparing the raw 15 W would see
// drift on every tick and re-write the limit every two seconds forever.
func EffectiveTDP(env driver.PowerEnvelope, s api.TDPState) api.TDPState {
	clamp := func(v int, r driver.PowerRange) int { return max(r.Min, min(r.Max, v)) }
	out := api.TDPState{
		PL1SPL:  clamp(s.PL1SPL, PL1Range(env)),
		PL2SPPT: clamp(s.PL2SPPT, PL2Range(env)),
		FPPT:    clamp(s.FPPT, PL3Range(env)),
	}
	if !env.NoSPPTMirrors {
		out.APUSPPT = clamp(s.APUSPPT, PL1Range(env))
		out.PlatformSPPT = clamp(s.PlatformSPPT, PL1Range(env))
	}
	return out
}

// CacheStale reports whether a readback is the interface's own initial cache
// rather than anything written since boot, and so says nothing about the limits
// in force: every field the envelope's Initial sets still holds that value. On
// asus-nb-wmi that is PL1 at the 5 W the attributes start at; on asus-armoury it
// is every limit still at its default_value, which armoury re-seeds per power
// source. An envelope with no Initial falls back to PL1 at TDPMin, the rule
// that predates drivers reporting one.
func CacheStale(env driver.PowerEnvelope, s api.TDPState) bool {
	in := env.Initial
	if in == (api.TDPState{}) {
		in.PL1SPL = env.TDPMin
	}
	pairs := [][2]int{
		{in.PL1SPL, s.PL1SPL}, {in.PL2SPPT, s.PL2SPPT}, {in.FPPT, s.FPPT},
		{in.APUSPPT, s.APUSPPT}, {in.PlatformSPPT, s.PlatformSPPT},
	}
	known := false
	for _, p := range pairs {
		if p[0] == 0 {
			continue
		}
		known = true
		if p[0] != p[1] {
			return false
		}
	}
	return known
}
