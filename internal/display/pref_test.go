// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package display

import (
	"strings"
	"testing"
)

// z13Rates is the panel's own rate list, derived from the captured
// kscreen-doctor reply rather than invented — the whole point of matching on
// the rounded rate is that this panel reports 59.868 for what it calls 60.
func z13Rates(t *testing.T) []Rate {
	t.Helper()
	outs, err := parseKScreen(z13Reply(t))
	if err != nil {
		t.Fatalf("parseKScreen: %v", err)
	}
	out, ok := Primary(outs)
	if !ok {
		t.Fatal("no primary output in the fixture")
	}
	return Rates(out)
}

func TestParsePref(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want int
	}{
		{"a plain rate", "60", 60},
		{"the value the UI writes", "180", 180},
		{"empty is unset", "", PrefUnset},
		{"whitespace only", "   ", PrefUnset},
		// A user hand-editing config.toml would very reasonably copy the label.
		{"hand-edited with the unit", "60 Hz", 60},
		{"hand-edited lower case, no space", "60hz", 60},
		// Anything unreadable means nothing rather than something arbitrary.
		{"a word", "high", PrefUnset},
		{"a negative", "-60", PrefUnset},
		{"zero", "0", PrefUnset},
		{"a fraction we do not store", "59.868", PrefUnset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ParsePref(tc.in); got != tc.want {
				t.Errorf("ParsePref(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseEnabled(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"true", "TRUE", "1", "yes", "on", " true "} {
		if !ParseEnabled(s) {
			t.Errorf("ParseEnabled(%q) = false, want true", s)
		}
	}
	// Absent is off, which is what makes the feature opt-in; unrecognised is off
	// for the same reason ParsePref refuses to guess.
	for _, s := range []string{"", "false", "0", "no", "maybe"} {
		if ParseEnabled(s) {
			t.Errorf("ParseEnabled(%q) = true, want false", s)
		}
	}
}

// The pair is written by controls and read back at the next login, so a value
// that does not survive the round trip would silently reset itself.
func TestPrefRoundTrip(t *testing.T) {
	t.Parallel()

	for _, hz := range []int{PrefUnset, 60, 120, 180} {
		if got := ParsePref(FormatPref(hz)); got != hz {
			t.Errorf("round trip of %d = %d", hz, got)
		}
	}
	for _, on := range []bool{true, false} {
		if got := ParseEnabled(FormatEnabled(on)); got != on {
			t.Errorf("round trip of %v = %v", on, got)
		}
	}
	// Both defaults leave no key at all rather than something to interpret later.
	if got := FormatPref(PrefUnset); got != "" {
		t.Errorf("FormatPref(PrefUnset) = %q, want empty", got)
	}
	if got := FormatEnabled(false); got != "" {
		t.Errorf("FormatEnabled(false) = %q, want empty", got)
	}
}

func TestPrefLabel(t *testing.T) {
	t.Parallel()

	// The same placeholder the live control shows before it has read the screen.
	// Reachable only from a hand-edited config: the UI writes both sides at once.
	if got := PrefLabel(PrefUnset); got != "—" {
		t.Errorf("PrefLabel(PrefUnset) = %q", got)
	}
	if got := PrefLabel(60); got != "60 Hz" {
		t.Errorf("PrefLabel(60) = %q", got)
	}
}

// A chooser row's label must be the same string the rate list shows for that
// mode, or the user picks "60 Hz" from one list and reads "60 hz" back in the
// other and cannot tell whether they are the same thing.
func TestPrefOptionsMatchTheRateLabels(t *testing.T) {
	t.Parallel()

	rates := z13Rates(t)
	opts := PrefOptions(rates)
	// One row per rate and no more: the switch is the off state, so there is no
	// "don't change" row to leave the feature's on/off state in two places.
	if len(opts) != len(rates) {
		t.Fatalf("options = %d, want %d (one per rate, no off row)", len(opts), len(rates))
	}
	for i, r := range rates {
		if got := opts[i].Label; got != r.Label {
			t.Errorf("option %d label = %q, rate label = %q", i, got, r.Label)
		}
	}
	if len(PrefOptions(nil)) != 0 {
		t.Error("PrefOptions(nil) offered rows for a screen it has not read")
	}
}

// Turning the switch on must not change the screen you are looking at, and must
// not be a no-op either. Both halves are the point.
func TestDefaultPrefs(t *testing.T) {
	t.Parallel()

	rates := z13Rates(t) // 180 Hz (running) and 60 Hz
	d := DefaultPrefs(rates)
	if !d.Enabled {
		t.Error("DefaultPrefs is not enabled; it is what the switch turns on")
	}
	if d.AC != 180 {
		t.Errorf("AC = %d, want the rate already running (180)", d.AC)
	}
	if d.Battery != 60 {
		t.Errorf("battery = %d, want the lowest offered (60)", d.Battery)
	}
	// Defaulting both to the running rate would leave the switch doing nothing
	// until the user found the second dropdown — a control that appears broken.
	if d.AC == d.Battery {
		t.Error("both sides default to the same rate, so enabling the switch does nothing")
	}
}

func TestDefaultPrefsWithNothingToChooseFrom(t *testing.T) {
	t.Parallel()

	if got := (DefaultPrefs(nil)); got != (Prefs{}) {
		t.Errorf("DefaultPrefs(nil) = %+v, want the zero value", got)
	}
	// No rate claims to be current — the honest reading of "leave mains alone"
	// is then the fastest the screen has.
	rates := []Rate{{Hz: 144, Label: "144 Hz"}, {Hz: 60, Label: "60 Hz"}}
	d := DefaultPrefs(rates)
	if d.AC != 144 || d.Battery != 60 {
		t.Errorf("DefaultPrefs = %+v, want AC 144 / battery 60", d)
	}
}

// Switching off and on again must not discard what was configured — the pair is
// two dropdowns the user set, not a transient.
func TestWithDefaultsKeepsWhatIsAlreadySet(t *testing.T) {
	t.Parallel()

	rates := z13Rates(t)
	got := Prefs{AC: 60, Battery: 60}.WithDefaults(rates)
	if got.AC != 60 || got.Battery != 60 {
		t.Errorf("WithDefaults = %+v, want both sides left at 60", got)
	}
	// Only the unset side is filled.
	got = Prefs{Battery: 180}.WithDefaults(rates)
	if got.AC != 180 || got.Battery != 180 {
		t.Errorf("WithDefaults = %+v, want AC filled from the running rate", got)
	}
}

func TestMatchFindsTheRate(t *testing.T) {
	t.Parallel()

	rates := z13Rates(t)
	r, ok := Match(rates, 60)
	if !ok {
		t.Fatalf("Match(rates, 60) found nothing; the panel offers %v", labels(rates))
	}
	if r.Label != "60 Hz" {
		t.Errorf("matched %q, want the 60 Hz entry", r.Label)
	}
	if r.ModeID == "" {
		t.Error("matched rate carries no mode id, so nothing could be applied")
	}
}

// The case that decides between storing hertz and storing a mode id, driven
// from real data: at 2560x1440 this panel's modes are 179.94 and 59.961, and a
// stored 180 or 60 — the numbers the label shows and the user picked — has to
// find them. Nothing in the fixture's *native* mode list exercises this, which
// is exactly why it needs its own case rather than a note on the one above.
func TestMatchRoundsTheRateItComparesAgainst(t *testing.T) {
	t.Parallel()

	outs, err := parseKScreen(z13Reply(t))
	if err != nil {
		t.Fatalf("parseKScreen: %v", err)
	}
	out := outs[0]
	out.CurrentModeID = "20" // 2560x1440 @ 179.94
	rates := Rates(out)

	for _, hz := range []int{180, 60} {
		r, ok := Match(rates, hz)
		if !ok {
			t.Fatalf("Match(rates, %d) found nothing; offered %v", hz, labels(rates))
		}
		if r.Hz == float64(hz) {
			t.Fatalf("the %d Hz mode is exactly %d here, so this case proves nothing", hz, hz)
		}
	}
}

func TestMatchRefusesWhatTheScreenDoesNotHave(t *testing.T) {
	t.Parallel()

	rates := z13Rates(t)

	// The rule that matters: no nearest-neighbour. 61 is a hair from a mode the
	// panel really has, and answering with it would retune a screen the user was
	// not configuring when they set the preference.
	if r, ok := Match(rates, 61); ok {
		t.Errorf("Match(rates, 61) = %+v, want no match — there is no nearest-neighbour rule", r)
	}
	if _, ok := Match(rates, 240); ok {
		t.Error("Match found a 240 Hz mode this panel does not have")
	}
	if _, ok := Match(rates, PrefUnset); ok {
		t.Error("PrefUnset matched a mode; it means there is nothing to do")
	}
	if _, ok := Match(nil, 60); ok {
		t.Error("Match found a mode in an empty rate list")
	}
}

// The switch is the off state, so a caller never has to consult it separately
// from the rate — which is what stops the two disagreeing.
func TestPrefsFor(t *testing.T) {
	t.Parallel()

	p := Prefs{Enabled: true, AC: 180, Battery: 60}
	if got := p.For(true); got != 180 {
		t.Errorf("For(onAC) = %d, want 180", got)
	}
	if got := p.For(false); got != 60 {
		t.Errorf("For(battery) = %d, want 60", got)
	}

	off := p
	off.Enabled = false
	if got := off.For(true); got != PrefUnset {
		t.Errorf("For(onAC) with the switch off = %d, want PrefUnset", got)
	}
	if got := off.For(false); got != PrefUnset {
		t.Errorf("For(battery) with the switch off = %d, want PrefUnset", got)
	}
	// Off keeps its rates, so switching back on restores them.
	if off.AC != 180 || off.Battery != 60 {
		t.Errorf("turning the switch off discarded the rates: %+v", off)
	}

	// Rate is the chooser's question and ignores the switch, where For is the
	// applier's and does not.
	if got := off.Rate(true); got != 180 {
		t.Errorf("Rate(onAC) with the switch off = %d, want the stored 180", got)
	}
	if got := off.Rate(false); got != 60 {
		t.Errorf("Rate(battery) with the switch off = %d, want the stored 60", got)
	}

	if !p.Complete() {
		t.Error("a pair with both sides set reports incomplete")
	}
	if (Prefs{Enabled: true, Battery: 60}).Complete() {
		t.Error("a pair with one side unset reports complete")
	}
}

func TestPrefNote(t *testing.T) {
	t.Parallel()

	rates := z13Rates(t)
	on := func(ac, batt int) Prefs { return Prefs{Enabled: true, AC: ac, Battery: batt} }

	cases := []struct {
		name  string
		prefs Prefs
		rates []Rate
		want  string // "" = no note; otherwise a substring the note must contain
	}{
		{"the ordinary configured pair", on(180, 60), rates, ""},
		// Off says nothing about rates it is not acting on.
		{"switched off", Prefs{AC: 240, Battery: 90}, rates, ""},
		{"a rate this screen lacks", on(180, 240), rates, "no 240 Hz"},
		{"both sides lack a rate", on(240, 90), rates, "or"},
		// Only reachable by hand-editing the config: the UI writes both sides
		// together, which is exactly why it is worth saying rather than assuming.
		{"a side left unset", on(180, PrefUnset), rates, "each power source"},
		// Unavailable outranks incomplete: an inert setting is the more
		// surprising of the two.
		{"unavailable and incomplete", on(PrefUnset, 240), rates, "does nothing here"},
		// Not "this screen has no modes" — it is a screen nothing has read yet,
		// and a warning that clears itself a moment later is noise.
		{"before the first query", on(180, 60), nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := PrefNote(tc.prefs, tc.rates)
			switch {
			case tc.want == "" && got != "":
				t.Errorf("PrefNote = %q, want no note", got)
			case tc.want != "" && !strings.Contains(got, tc.want):
				t.Errorf("PrefNote = %q, want it to mention %q", got, tc.want)
			}
		})
	}
}

// A duplicate would read as "no 240 Hz or 240 Hz mode".
func TestPrefNoteNamesEachMissingRateOnce(t *testing.T) {
	t.Parallel()

	note := PrefNote(Prefs{Enabled: true, AC: 240, Battery: 240}, z13Rates(t))
	if strings.Count(note, "240 Hz") != 1 {
		t.Errorf("PrefNote = %q, want 240 Hz named once", note)
	}
}

func labels(rates []Rate) []string {
	out := make([]string, 0, len(rates))
	for _, r := range rates {
		out = append(out, r.Label)
	}
	return out
}
