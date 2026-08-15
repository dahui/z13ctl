// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package display

// pref.go — the refresh rate to select on each power source.
//
// A 180 Hz panel is worth 180 Hz on mains and frequently not worth it on the
// battery, so this is a switch, a rate per source, and the rules for resolving
// one against whatever screen is actually in front of you.
//
// # Why the switching is the GUI's and not the daemon's
//
// The same reason the manual control is: a video mode belongs to the
// compositor, and the daemon is a systemd user service with no guaranteed
// WAYLAND_DISPLAY. The daemon reports *that* the power source moved — its
// autoswitch watcher is the only thing on the machine that observes it
// correctly, Mains-only and edge-triggered — and a session client decides what
// that means for the screen. The package doc has the longer argument.
//
// # A preference is hertz, never a mode ID
//
// Mode.ID is the compositor's own handle and exactly the right thing to *send*,
// and exactly the wrong thing to store: kscreen derives it from the mode list,
// so it does not survive the panel's modes changing and means nothing at all on
// a different screen. A rate is what the user picked out of a list, and it is
// still true after a dock, a resolution change or a KDE upgrade. Match resolves
// one back to a mode at the moment it is needed.
//
// # The switch is the off state, not a row in each list
//
// Each side named a rate *or* "don't change" at first, which put the feature's
// on/off state in two places at once and let the two disagree — "don't change"
// on both sides is off, one side set is a half state with its own caution to
// explain. Enabled is one bool, the two lists offer only rates the screen has,
// and the shape then matches the profile autoswitch block it sits beside
// (Jeff, 2026-08-14).

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// PrefUnset is "no rate chosen for this source". It is not a user-selectable
// answer — the switch is the off state — but it is what an absent config value,
// an unreadable one, and a pair that has never been configured all read as.
const PrefUnset = 0

// Prefs is the refresh rate to select on each power source, and whether to.
//
// It deliberately mirrors api.AutoswitchState's shape — an enable plus a target
// per source — because it is the same idea for a different subsystem, and the
// two are rendered as neighbouring cards.
type Prefs struct {
	Enabled bool
	AC      int // hertz, or PrefUnset
	Battery int
}

// For returns the rate to select on a power source, or PrefUnset when there is
// nothing to do. Disabled answers PrefUnset for both sources, so a caller never
// has to check the switch separately from the rate.
func (p Prefs) For(onAC bool) int {
	if !p.Enabled {
		return PrefUnset
	}
	if onAC {
		return p.AC
	}
	return p.Battery
}

// Rate returns the rate stored for a power source regardless of the switch —
// what a chooser stands on, as against what For says to apply.
//
// The two agree whenever the switch is on, which is the only time the choosers
// are on screen, so a caller reaching for For there gets the right answer by
// accident. It is the accident that is worth removing: the question "which row
// is selected" has nothing to do with whether the feature is enabled, and the
// day something reads a stored rate with the switch off, For would answer
// "none" and the row would blank itself.
func (p Prefs) Rate(onAC bool) int {
	if onAC {
		return p.AC
	}
	return p.Battery
}

// Complete reports whether both sides name a rate. The UI writes both together,
// so a false here means a hand-edited config: worth saying out loud rather than
// silently doing nothing on one of the two transitions.
func (p Prefs) Complete() bool {
	return p.AC != PrefUnset && p.Battery != PrefUnset
}

// DefaultPrefs is what the pair holds the first time the switch is turned on:
// the rate the screen is already running on mains, and the lowest it offers on
// battery.
//
// Both halves are deliberate. Mains keeps the status quo, so enabling the
// switch cannot change the screen you are looking at. Battery takes the lowest
// rate because dropping it is the entire reason this exists — defaulting both
// to the current rate would make the switch do nothing at all until the user
// found the second dropdown, which is a control that appears not to work.
// Neither is a silent decision: both land in the two rows immediately, ahead of
// any transition, and either can be changed.
func DefaultPrefs(rates []Rate) Prefs {
	if len(rates) == 0 {
		return Prefs{}
	}
	// Rates is sorted highest first, so the running rate falls back to the
	// fastest the screen has — the honest reading of "leave mains alone" when
	// nothing claims to be current.
	ac := rates[0]
	for _, r := range rates {
		if r.Current {
			ac = r
			break
		}
	}
	return Prefs{
		Enabled: true,
		AC:      roundHz(ac.Hz),
		Battery: roundHz(rates[len(rates)-1].Hz),
	}
}

// WithDefaults fills in whichever sides are unset, leaving the rest alone. It is
// what the switch calls when it is turned on: a pair configured earlier and
// switched off and on again comes back as it was, and one that has never been
// configured gets DefaultPrefs.
func (p Prefs) WithDefaults(rates []Rate) Prefs {
	d := DefaultPrefs(rates)
	if p.AC == PrefUnset {
		p.AC = d.AC
	}
	if p.Battery == PrefUnset {
		p.Battery = d.Battery
	}
	return p
}

// ParsePref reads a stored rate: a whole number of hertz, optionally with the
// unit the label carries, so a hand-edited `refresh_battery = "60 Hz"` means
// what it looks like.
//
// Anything else — empty, a word, a negative — reads as PrefUnset. A client that
// cannot understand a preference has exactly one safe thing to do with it, and
// that is nothing.
func ParsePref(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSpace(strings.TrimSuffix(s, "hz"))
	if s == "" {
		return PrefUnset
	}
	hz, err := strconv.Atoi(s)
	if err != nil || hz <= 0 {
		return PrefUnset
	}
	return hz
}

// FormatPref renders a rate for storage. PrefUnset is the empty string, so an
// unset side leaves no key in the config file at all rather than a "0" somebody
// later has to interpret.
func FormatPref(hz int) string {
	if hz <= PrefUnset {
		return ""
	}
	return strconv.Itoa(hz)
}

// ParseEnabled reads the stored switch position. Absent is off, which is what
// makes the whole feature opt-in; anything unrecognised is off for the same
// reason ParsePref refuses to guess.
func ParseEnabled(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// FormatEnabled renders the switch position for storage. Off is the empty
// string, so the default leaves no key behind.
func FormatEnabled(on bool) string {
	if on {
		return "true"
	}
	return ""
}

// PrefLabel is a rate as a menu entry or a collapsed trigger. An unset side
// shows the same "—" the live control uses before it has read the screen, which
// is the only way it can be reached: the UI writes both sides together.
func PrefLabel(hz int) string {
	if hz <= PrefUnset {
		return "—"
	}
	return fmt.Sprintf("%d Hz", hz)
}

// PrefOption is one row of a rate chooser.
type PrefOption struct {
	Hz    int
	Label string
}

// PrefOptions is the chooser's rows: every rate the screen offers at its
// current resolution, in the order Rates returns them. There is no off row —
// the switch is the off state.
func PrefOptions(rates []Rate) []PrefOption {
	opts := make([]PrefOption, 0, len(rates))
	for _, r := range rates {
		hz := roundHz(r.Hz)
		opts = append(opts, PrefOption{Hz: hz, Label: PrefLabel(hz)})
	}
	return opts
}

// Match resolves a stored rate against the rates a screen currently offers.
//
// The comparison is exact on the *rounded* rate, which is the number the user
// picked out of a list — the same rounding Rates already deduplicates by, so at
// most one entry can match.
//
// There is deliberately no nearest-neighbour fallback. A preference says "run
// this screen at 60"; on a screen with no 60 the honest answer is to do
// nothing, because substituting whatever is closest changes a mode nobody asked
// about, on hardware they were not configuring when they set it. Docking a
// monitor should not silently retune it.
func Match(rates []Rate, hz int) (Rate, bool) {
	if hz <= PrefUnset {
		return Rate{}, false
	}
	for _, r := range rates {
		if roundHz(r.Hz) == hz {
			return r, true
		}
	}
	return Rate{}, false
}

// PrefNote is the caution to show beneath the pair, or "" when there is nothing
// to say.
//
// Both cases are things the two dropdowns cannot show on their own: a stored
// rate this screen does not have is inert and would otherwise look like it
// works, and a side left unset does nothing on that transition — reachable only
// by hand-editing the config, since the UI fills both together, which is
// exactly why it needs saying rather than assuming.
//
// Nothing is said at all when the switch is off, or when rates is empty. The
// latter is not a screen with no modes, it is a screen that has not been read
// yet, and calling every stored rate unavailable during the first query would
// be a warning that clears itself a moment later.
func PrefNote(p Prefs, rates []Rate) string {
	if !p.Enabled || len(rates) == 0 {
		return ""
	}

	var missing []string
	for _, hz := range []int{p.AC, p.Battery} {
		if hz == PrefUnset {
			continue
		}
		if _, ok := Match(rates, hz); !ok {
			if label := PrefLabel(hz); !contains(missing, label) {
				missing = append(missing, label)
			}
		}
	}
	if len(missing) > 0 {
		return "This screen has no " + strings.Join(missing, " or ") +
			" mode, so that setting does nothing here."
	}

	if !p.Complete() {
		return "Pick a rate for each power source."
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// roundHz is the rounding every comparison in this file shares with FormatHz's
// label, so a rate that displays as "60 Hz" is the rate a stored 60 matches.
func roundHz(hz float64) int {
	return int(math.Round(hz))
}
