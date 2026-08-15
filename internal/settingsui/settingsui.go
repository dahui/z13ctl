// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package settingsui is the settings page's rules: which firmware-toggle rows
// a device offers, what each says, and what its switch is allowed to claim.
//
// It is a separate package for the reason every rule on the GUI side is: the
// drawer needs cgo and GTK4 headers, so `make test` cannot compile it, and
// anything left in there is permanently unverifiable. What lives here is the
// row resolution; what stays in internal/gui is a label, a switch, and a send.
//
// # The rows are device data, not a written list
//
// This page exists to render api.DeviceInfo.Toggles generically. The drawer's
// bottom bar names its two switches — "Panel Overdrive", "Boot Sound" — as GTK
// literals, with their warning text written beside them, which is exactly why a
// device with a different set of toggles could not be described at all. Here
// the label, the prose and the id all come from the document, so a device that
// declares three toggles gets three rows and one that declares none gets a page
// that says so.
package settingsui

import "github.com/dahui/voltaire/api/v2"

// Row is one toggle, ready to render.
//
// On and Known are separate for the reason the daemon omits an unreadable
// toggle from api.State.Features rather than reporting zero: zero is "off",
// which is a claim about the hardware. A switch that cannot read its own value
// has to be able to show that it does not know, and the caller renders
// Known == false as an insensitive row — which the gamepad focus grid then
// skips, since a control that cannot be operated is one a controller should
// not land on.
type Row struct {
	ID          string // the wire id for the feature commands
	Label       string // human-readable name, never empty
	Description string // prose beside the control; empty when the device offers none
	On          bool   // the current value, meaningful only when Known
	Known       bool   // whether the current value could be read at all
}

// Rows returns the toggle rows for a device, with each row's current value
// filled in from the state.
//
// doc is the capability document and st the most recent get-state; either may
// be nil. A nil document yields no rows — unlike the capability questions
// elsewhere, where nil means "the daemon did not answer, so keep everything",
// there is no fallback list to keep here: the rows *are* the document. A nil
// state yields rows whose values are all unknown, which is the honest state of
// a page built before the first sync.
//
// A toggle whose kind this build does not recognize is skipped rather than
// rendered as a switch. Only api.ToggleKindBool exists today; an enumerated
// toggle drawn as a switch would misrepresent it, and writing to it would send
// a 0 or a 1 to something that means neither.
func Rows(doc *api.DeviceInfo, st *api.State) []Row {
	if doc == nil {
		return nil
	}
	var out []Row
	for _, t := range doc.Toggles {
		if t.Kind != api.ToggleKindBool {
			continue
		}
		r := Row{ID: t.ID, Label: t.Label, Description: t.Description}
		if r.Label == "" {
			// A device that declares an id and no label still gets a usable
			// row: the id is terse but true, where a blank label is a switch
			// with nothing to say what it does.
			r.Label = t.ID
		}
		if st != nil {
			if v, ok := st.Features[t.ID]; ok {
				r.On, r.Known = v != 0, true
			}
		}
		out = append(out, r)
	}
	return out
}

// EmptyReason is the text to show in place of an empty row set, saying which
// kind of nothing this is.
//
// Three of them are distinguishable and they call for different responses from
// the user, which is the whole reason this is not one string: a daemon that was
// down when the window opened is fixed by starting it, a device with no
// firmware toggles is fixed by nothing, and a device whose toggles are all of
// some kind this build cannot render is a voltaire limitation and should say
// so rather than claiming the machine has no settings.
//
// Returns "" when there is nothing to explain — the caller has rows to draw.
func EmptyReason(doc *api.DeviceInfo) string {
	if len(Rows(doc, nil)) > 0 {
		return ""
	}
	switch {
	case doc == nil:
		return "The voltaire daemon was not running when this window opened, " +
			"so its list of firmware settings is unknown. Start the daemon and reopen."
	case len(doc.Toggles) > 0:
		return "This device's firmware settings are of a kind this version of " +
			"voltaire cannot show yet."
	default:
		return "This device exposes no firmware settings voltaire can change."
	}
}

// RebootNotice is the banner text for a firmware setting that is waiting on a
// restart, or "" when there is nothing to say.
//
// It is gated on a *known* true, which is the whole point of api.State's
// PendingReboot being a pointer. Absent means the device cannot say — no
// firmware interface, a pre-2.0 daemon, or a failed read — and drawing
// "everything is applied" from that would be a claim nothing established. The
// banner appears only when the firmware positively reports something pending.
//
// The wording avoids naming which setting, because the firmware does not say:
// asus-armoury exposes one flag for the whole interface, not one per attribute.
// Promising more than that would send the user looking for a row to fix.
func RebootNotice(st *api.State) string {
	if st == nil || st.PendingReboot == nil || !*st.PendingReboot {
		return ""
	}
	return "A firmware setting has changed and takes effect after a restart."
}
