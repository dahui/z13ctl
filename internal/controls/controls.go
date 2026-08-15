// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package controls is the drawer's list of sections: which ones exist, what
// each needs from the device, and what order they appear in.
//
// The drawer used to answer all three questions by the order of Append calls in
// one GTK function. That is fine while there is one machine and one layout, and
// it is the wrong shape for a quickbar the user can reorder or trim — that
// needs the list as *data*, before any widget exists.
//
// It is a separate package for the reason every rule on the GUI side is: the
// drawer needs cgo and GTK4 headers, so `make test` cannot compile it and
// anything left in there is permanently unverifiable. What lives here is the
// resolution — defaults, user order, capability filtering. What stays in
// internal/gui is the mapping from an ID to a widget builder.
//
// # The property that matters
//
// A user with no gui.toml must get exactly the drawer they had before this
// package existed. Order is therefore not a fresh design: it is a transcription
// of the pre-registry buildContent, and TestDefaultOrderIsTheShippedLayout is
// what keeps it one.
//
// # What is deliberately not here yet
//
// The roadmap has this registry also driving a settings view and rows
// contributed by plugins, with a Kind field distinguishing a section the drawer
// builds itself from a bool row rendered generically from the device document.
// Both are out until something renders them, because a Kind with no renderer is
// the same trap as a device document declaring a capability nothing reads: it
// looks like a feature and produces an empty space.
//
// Generic toggle rows now exist, but they are the full window's Settings page
// (internal/settingsui + gui/settingsview.go), rendered straight from
// api.DeviceInfo.Toggles rather than from this registry. That is the split
// worth knowing: this package lists the *drawer's* sections, and a firmware
// toggle is not one of them — the rows are device data, so a registry entry
// per toggle would be a second list to keep in step with the document.
//
// A Kind field here therefore stays out until the drawer itself hosts
// generically-rendered rows, which is a quickbar-customization question rather
// than a settings one. Adding it now would still be the empty-space trap above.
// The capability those rows resolve against does live here (CapToggles), so
// that "does this device have any" has one answer for every surface.
package controls

import "github.com/dahui/voltaire/api/v2"

// Capability names a section of the device document a control depends on.
// A control whose capabilities are not all present is dropped: the device
// cannot do the thing, so the control would either be dead or lie.
type Capability string

// The capabilities a control can require. These mirror api.DeviceInfo's own
// sections; there is deliberately no capability for something the document does
// not report, since a requirement nothing can satisfy would hide a control
// forever with no way to tell why.
const (
	CapFans      Capability = "fans"
	CapPower     Capability = "power"
	CapProfiles  Capability = "profiles"
	CapLighting  Capability = "lighting"
	CapBattery   Capability = "battery"
	CapTelemetry Capability = "telemetry"
	CapUndervolt Capability = "undervolt"

	// CapToggles is the odd one out: every capability above is a section of
	// the document that is either present or nil, while toggles are a *list*,
	// and a device that declares an empty one has no firmware switches to
	// show. So the question here is "are there any", not "is the section
	// there" — a settings page gated on an empty list would be the tab that
	// opens onto an empty page.
	CapToggles Capability = "toggles"

	// CapCPUBoost is the boost switch. Like CapToggles it is not simply "the
	// section exists": [cpu] can declare a control this build does not have,
	// so the question is whether *boost* specifically is offered.
	CapCPUBoost Capability = "cpu-boost"
)

// The group headings the drawer prints, exactly as they are shown.
const (
	GroupPower = "TDP AND POWER"
	GroupRGB   = "RGB"
)

// Control describes one section of the drawer.
type Control struct {
	// ID is the stable wire name. It appears in the user's gui.toml, so it is
	// never renamed — a renamed ID silently drops the section from the layout
	// of every user who listed it.
	ID string

	// Label is what the list calls the control: for the customization UI, and
	// for anything that has to name it in an error. A control's own builder
	// supplies its in-drawer heading, which is why these are not the same
	// strings.
	Label string

	// Group is the heading the drawer prints above a run of controls that
	// share it ("TDP AND POWER", "RGB"). It belongs to the control rather than
	// to a separate list of headings so that reordering cannot leave a heading
	// stranded above the wrong section — the heading is emitted where the group
	// changes, so it follows whatever order the user chose. Empty means no
	// heading.
	Group string

	// Requires lists every capability the device must have. It is a slice, not
	// the single field the roadmap sketched, because autoswitch genuinely needs
	// two: it selects profiles, and it acts on a power-source change the daemon
	// can only observe through the battery capability (acPower returns unknown
	// without it, so autoswitch could never fire). Written as one field it
	// would have had to name the less important of the two.
	Requires []Capability
}

// defaultOrder is the drawer's scrolling sections in the order buildContent
// appends them. Read it as a transcription, not a design — see the package doc.
//
// The bottom bar is not in this list. It is fixed chrome outside the scroll
// area, so a reordering that could move the theme button into the middle of the
// scrolling content would have to move it between two containers; "the things
// you can reorder" and "the things that scroll" are the same set, and saying so
// is simpler than supporting a move nobody wants.
var defaultOrder = []Control{
	{ID: "profile", Label: "Performance profile", Group: GroupPower,
		Requires: []Capability{CapProfiles}},
	{ID: "autoswitch", Label: "AC/battery autoswitch", Group: GroupPower,
		Requires: []Capability{CapProfiles, CapBattery}},
	{ID: "battery", Label: "Charge limit", Group: GroupPower,
		Requires: []Capability{CapBattery}},
	// The whole RGB block — zone tabs, effect modes, both colours, speed and
	// brightness — is one control, because its parts are not independently
	// meaningful: which of them are visible is already decided by the selected
	// effect (syncModeVis), so a user who could hide "speed" would be fighting
	// that logic rather than configuring anything. Hiding RGB entirely is the
	// choice people actually want.
	{ID: "lighting", Label: "RGB lighting", Group: GroupRGB,
		Requires: []Capability{CapLighting}},
}

// Row is one control together with the chrome the drawer prints before it.
type Row struct {
	// Separator is whether to draw a divider above this control.
	Separator bool
	// Heading is the group heading to print above this control, after the
	// separator. Empty means none.
	Heading string
	Control Control
}

// Layout pairs each resolved control with the heading and separator that
// precede it: a heading wherever the group changes, and a separator before
// every heading except the first.
//
// This is here rather than in the drawer's build loop because it is the shape
// of the panel — with the default list it is exactly the label/section/
// separator/label sequence buildContent used to spell out — and a rule that
// decides what the user sees should not live in the one package `make test`
// cannot compile. TestLayoutReproducesTheShippedChrome is the pin.
func Layout(resolved []Control) []Row {
	rows := make([]Row, 0, len(resolved))
	group := ""
	first := true
	for _, c := range resolved {
		r := Row{Control: c}
		if c.Group != group {
			r.Heading = c.Group
			r.Separator = !first
			group = c.Group
		}
		first = false
		rows = append(rows, r)
	}
	return rows
}

// All returns every known control in default order. The returned slice shares
// nothing with the package's own defaults, which callers reorder and filter.
func All() []Control {
	out := make([]Control, len(defaultOrder))
	copy(out, defaultOrder)
	for i := range out {
		out[i].Requires = append([]Capability(nil), defaultOrder[i].Requires...)
	}
	return out
}

// Lookup returns the control with the given ID.
func Lookup(id string) (Control, bool) {
	for _, c := range All() {
		if c.ID == id {
			return c, true
		}
	}
	return Control{}, false
}

// IDs returns every known control ID in default order — what a config file may
// legally name, and what an error message lists when it names something else.
func IDs() []string {
	return IDsOf(defaultOrder)
}

// IDsOf returns the IDs of the given controls, for logging a resolved layout.
func IDsOf(cs []Control) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// Supports reports whether info has every capability c requires.
//
// A nil document means "the daemon did not answer", not "this machine has
// nothing": the drawer falls back to built-in limits in exactly that case and
// must not also blank its own controls. Everything is supported until the
// document says otherwise, which is the posture limits.FromDevice already
// takes for the same reason.
func Supports(info *api.DeviceInfo, c Control) bool {
	return SupportsAll(info, c.Requires)
}

// SupportsAll reports whether info has every capability in want. It is the
// same question Supports asks, exposed for callers whose unit is not a
// Control: internal/mainwin resolves the full window's tabs against the same
// document, and a second copy of hasCapability is how two lists come to
// disagree about what a device can do.
func SupportsAll(info *api.DeviceInfo, want []Capability) bool {
	if info == nil {
		return true
	}
	for _, c := range want {
		if !hasCapability(info, c) {
			return false
		}
	}
	return true
}

func hasCapability(info *api.DeviceInfo, capability Capability) bool {
	switch capability {
	case CapFans:
		return info.Fans != nil
	case CapPower:
		return info.Power != nil
	case CapProfiles:
		return info.Profiles != nil
	case CapLighting:
		return info.Lighting != nil
	case CapBattery:
		return info.Battery != nil
	case CapTelemetry:
		return info.Telemetry != nil
	case CapUndervolt:
		return info.Undervolt != nil
	case CapCPUBoost:
		return info.CPU != nil && info.CPU.Boost
	case CapToggles:
		return len(info.Toggles) > 0
	}
	// An unknown capability is not satisfiable. It can only come from a Control
	// written against a newer document than this build understands, and hiding
	// that control is the conservative answer.
	return false
}
