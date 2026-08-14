// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package display is the screen's refresh rate: which rates the panel offers at
// the resolution it is currently running, and how to select one.
//
// # Why this is not in the daemon
//
// Every other setting voltaire touches is hardware behind sysfs or hidraw, and
// the daemon owns it because it outlives any client and has to restore it. A
// refresh rate is neither: it belongs to the compositor, it is per-session, and
// the compositor already persists it. Routing it through the daemon would also
// put a KDE dependency inside a process that deliberately knows nothing about
// desktops — and the daemon is a systemd user service, so it is not even
// guaranteed to have WAYLAND_DISPLAY in its environment, while a GTK client
// definitively does.
//
// So the GUI calls this package directly. That is a deliberate exception to
// "the GUI is an ordinary socket client", and it is a narrow one: the rule
// exists to stop the daemon growing GUI-shaped shortcuts, not to stop the GUI
// from talking to the session it is running in.
//
// # Capability is by absence, as everywhere else
//
// There is one backend today (KDE's kscreen-doctor). A machine without it —
// GNOME, wlroots, a gamescope session — reports no rates and the caller shows
// no control, which is the same rule the device document's nil sections follow.
// Guessing at a rate we cannot read, or offering a control that silently fails,
// are both worse than an absent control.
//
// The parsing and selection rules are pure and tested; only Query and Apply
// shell out, through a seam tests replace.
package display

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"time"
)

// Mode is one video mode an output can run.
type Mode struct {
	// ID is the compositor's own handle for the mode, and is what Apply sends.
	// It is used rather than the "2560x1600@180" name because the name is
	// rounded — two modes a tenth of a hertz apart share one — so selecting by
	// name is ambiguous exactly where a refresh-rate control is not allowed to
	// be.
	ID string

	Width, Height int

	// RefreshHz is the exact rate, unrounded: 59.96 rather than 60. Labels
	// round it; equality never does.
	RefreshHz float64
}

// Output is one screen.
type Output struct {
	Name          string
	Enabled       bool
	Connected     bool
	CurrentModeID string

	// Priority is the compositor's ordering, 1 being primary. Zero means the
	// backend did not report one.
	Priority int

	Modes []Mode
}

// Current returns the mode the output is running.
func (o Output) Current() (Mode, bool) {
	for _, m := range o.Modes {
		if m.ID == o.CurrentModeID {
			return m, true
		}
	}
	return Mode{}, false
}

// Rate is one selectable refresh rate at an output's current resolution.
type Rate struct {
	ModeID  string
	Hz      float64
	Label   string
	Current bool
}

// Rates returns the refresh rates available at the output's *current*
// resolution, highest first.
//
// Resolution is deliberately held fixed. A dropdown that offered every mode
// would be a resolution picker with the rate as a suffix, and changing the
// resolution out from under a running session is not what "change the refresh
// rate" means — the compositor's own display settings are the right place for
// that, and they do it with a confirmation timer this control has no business
// reimplementing.
//
// Rates that round to the same whole number collapse to one entry, keeping the
// higher exact value. Panels routinely list 59.96 and 60.00 as separate modes;
// presenting both as "60 Hz" would be a menu with two identical items.
func Rates(o Output) []Rate {
	cur, ok := o.Current()
	if !ok {
		return nil
	}

	byLabel := map[string]Rate{}
	for _, m := range o.Modes {
		if m.Width != cur.Width || m.Height != cur.Height {
			continue
		}
		label := FormatHz(m.RefreshHz)
		prev, seen := byLabel[label]
		// Keep the higher exact rate, except that the mode actually running
		// always wins — a control must be able to show the state it is in.
		switch {
		case !seen, m.ID == o.CurrentModeID:
		case prev.Current, m.RefreshHz <= prev.Hz:
			continue
		}
		byLabel[label] = Rate{
			ModeID:  m.ID,
			Hz:      m.RefreshHz,
			Label:   label,
			Current: m.ID == o.CurrentModeID,
		}
	}

	out := make([]Rate, 0, len(byLabel))
	for _, r := range byLabel {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hz > out[j].Hz })
	return out
}

// FormatHz is a rate as a menu entry. Panels report 59.96 for what every other
// piece of software on the machine calls 60, so the label rounds; the exact
// value is never compared against the rounded one.
func FormatHz(hz float64) string {
	return fmt.Sprintf("%d Hz", int(math.Round(hz)))
}

// Primary returns the output a single refresh-rate control should act on: the
// enabled screen the compositor ranks first, or the first enabled one when it
// ranks none.
//
// A machine with two screens gets one control acting on the primary rather than
// one control per screen. That is the honest shape for a quick control — the
// caller labels it with the output's name whenever there is more than one, so
// it never silently acts on a screen the user did not mean.
func Primary(outs []Output) (Output, bool) {
	var best Output
	found := false
	for _, o := range outs {
		if !o.Enabled || !o.Connected {
			continue
		}
		if !found || betterPrimary(o, best) {
			best, found = o, true
		}
	}
	return best, found
}

func betterPrimary(candidate, best Output) bool {
	switch {
	case candidate.Priority == best.Priority:
		return false
	case best.Priority == 0:
		return candidate.Priority > 0
	case candidate.Priority == 0:
		return false
	default:
		return candidate.Priority < best.Priority
	}
}

// EnabledCount is how many screens are lit, which is what decides whether a
// control has to name the one it acts on.
func EnabledCount(outs []Output) int {
	n := 0
	for _, o := range outs {
		if o.Enabled && o.Connected {
			n++
		}
	}
	return n
}

// commandTimeout bounds both calls. kscreen-doctor talks to the compositor over
// DBus, so a wedged compositor would otherwise hang the caller — and this one
// is called from a GTK thread's goroutine while a dropdown waits on it.
const commandTimeout = 5 * time.Second

// runner is the exec seam. Tests replace it; nothing else does.
var runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// lookPath is the other half of the seam: whether the backend is installed.
var lookPath = exec.LookPath

const kscreenDoctor = "kscreen-doctor"

// ErrNoBackend means nothing on this machine can report or set a video mode.
// Callers show no control rather than an error — an absent capability is not a
// failure.
var ErrNoBackend = errors.New("no supported display backend")

// Available reports whether a backend exists. It is a PATH lookup, not a query:
// it runs while the widget tree is being built, and shelling out to the
// compositor there would put a DBus round trip in front of the first frame.
func Available() bool {
	_, err := lookPath(kscreenDoctor)
	return err == nil
}

// Query returns the machine's screens.
func Query() ([]Output, error) {
	if !Available() {
		return nil, ErrNoBackend
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := runner(ctx, kscreenDoctor, "-j")
	if err != nil {
		return nil, fmt.Errorf("%s -j: %w", kscreenDoctor, err)
	}
	return parseKScreen(out)
}

// Apply switches an output to a mode.
func Apply(outputName, modeID string) error {
	if !Available() {
		return ErrNoBackend
	}
	if outputName == "" || modeID == "" {
		return errors.New("display: output and mode are both required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	arg := fmt.Sprintf("output.%s.mode.%s", outputName, modeID)
	if _, err := runner(ctx, kscreenDoctor, arg); err != nil {
		return fmt.Errorf("%s %s: %w", kscreenDoctor, arg, err)
	}
	return nil
}

// kscreenReply is the shape of `kscreen-doctor -j` this package reads. Every
// other field it emits is deliberately ignored: unmarshalling into a narrow
// struct means a new KDE release adding keys cannot break the parse.
type kscreenReply struct {
	Outputs []struct {
		Name          string `json:"name"`
		Enabled       bool   `json:"enabled"`
		Connected     bool   `json:"connected"`
		CurrentModeID string `json:"currentModeId"`
		Priority      int    `json:"priority"`
		Modes         []struct {
			ID          string  `json:"id"`
			RefreshRate float64 `json:"refreshRate"`
			Size        struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"size"`
		} `json:"modes"`
	} `json:"outputs"`
}

// parseKScreen turns a kscreen-doctor JSON reply into outputs. Pure, so the
// wire shape is testable without a compositor.
func parseKScreen(data []byte) ([]Output, error) {
	var reply kscreenReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return nil, fmt.Errorf("parse %s output: %w", kscreenDoctor, err)
	}
	outs := make([]Output, 0, len(reply.Outputs))
	for _, o := range reply.Outputs {
		out := Output{
			Name:          o.Name,
			Enabled:       o.Enabled,
			Connected:     o.Connected,
			CurrentModeID: o.CurrentModeID,
			Priority:      o.Priority,
		}
		for _, m := range o.Modes {
			out.Modes = append(out.Modes, Mode{
				ID:        m.ID,
				Width:     m.Size.Width,
				Height:    m.Size.Height,
				RefreshHz: m.RefreshRate,
			})
		}
		outs = append(outs, out)
	}
	return outs, nil
}
