// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package profileui holds the drawer's profile rules: which rows the profile
// selector shows and with which affordances, how the editor must address a
// given profile (live bare sends versus stored --profile sends), which values
// the editor displays for each target, and the autoswitch target choices.
//
// It exists as a separate package for the same reason internal/lighting and
// internal/limits do: internal/gui needs CGO and GTK4 headers, so any rule
// left in there is beyond every test. The GTK layer reads widgets, calls in
// here, and applies the answer.
//
// The split between StockRows and CustomRows mirrors the drawer's two levels:
// the main view offers the three firmware profiles plus one button standing
// for the whole custom family, and the custom view's selector offers the
// custom profiles themselves. Keeping the main view to four controls is
// deliberate — a saved profile per row crowded out everything below it.
//
// Every function tolerates a nil *api.State — the drawer builds its widget
// tree before the first successful state fetch — by answering as for an empty
// state: the stock profiles exist, "custom" is addressable, and nothing else
// is known.
package profileui

import (
	"sort"
	"strings"

	"github.com/dahui/voltaire/api/v2"
)

// Kind classifies a profile row.
type Kind int

const (
	// Stock is a firmware profile (quiet/balanced/performance): always
	// activatable, never editable or deletable.
	Stock Kind = iota
	// DefaultCustom is the reserved "custom" profile, addressable before it
	// has ever been populated.
	DefaultCustom
	// Named is a saved named custom profile.
	Named
)

// Row is one entry of a profile selector.
type Row struct {
	Name   string
	Label  string // display text; stock and "custom" title-cased, named profiles verbatim
	Kind   Kind
	Active bool
	// ActivateBlock is "" when the profile can be activated right now, else
	// the reason the affordance must be insensitive.
	ActivateBlock string
	// DeleteBlock is "" when the profile can be deleted, else the reason.
	// Mirrors the daemon's refusals so the tooltip and the error the daemon
	// would return agree.
	DeleteBlock string
}

// StockRows returns the three firmware profile rows, which are always present
// and always activatable.
func StockRows(s *api.State) []Row {
	rows := make([]Row, 0, len(api.StockProfiles))
	for _, name := range api.StockProfiles {
		rows = append(rows, Row{
			Name:        name,
			Label:       Label(name),
			Kind:        Stock,
			Active:      s != nil && s.Profile == name,
			DeleteBlock: "firmware profiles cannot be deleted",
		})
	}
	return rows
}

// CustomRows returns the custom-family profiles in display order: "custom"
// first, then the named profiles sorted by name. "custom" appears whether or
// not it has ever been populated — it is always addressable — but carries an
// ActivateBlock until it has settings, matching the daemon's refusal to
// activate an empty profile.
func CustomRows(s *api.State) []Row {
	rows := []Row{customRow(s, api.DefaultCustomProfile, DefaultCustom)}
	for _, name := range namedProfiles(s) {
		rows = append(rows, customRow(s, name, Named))
	}
	return rows
}

// namedProfiles returns the saved custom profile names other than "custom",
// sorted for a stable display order.
func namedProfiles(s *api.State) []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.CustomProfiles))
	for name := range s.CustomProfiles {
		if name != api.DefaultCustomProfile {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func customRow(s *api.State, name string, kind Kind) Row {
	return Row{
		Name:          name,
		Label:         Label(name),
		Kind:          kind,
		Active:        s != nil && s.Profile == name,
		ActivateBlock: ActivateBlock(s, name),
		DeleteBlock:   DeleteBlockFor(s, name),
	}
}

// CustomSummary describes the main view's single Custom button, which stands
// for the whole custom family.
type CustomSummary struct {
	// Label names the running custom profile when there is one, so the main
	// view still says what is in force without listing every profile.
	Label string
	// Active is true when a custom profile is running.
	Active bool
}

// Custom returns the main view's Custom button state.
func Custom(s *api.State) CustomSummary {
	if s != nil && s.InCustomProfile() {
		return CustomSummary{Label: Label(s.Profile), Active: true}
	}
	return CustomSummary{Label: "Custom"}
}

// DefaultEditTarget returns the profile the custom view should open on: the
// running custom profile when there is one, otherwise "custom" — which is
// what a bare edit would create and activate anyway.
func DefaultEditTarget(s *api.State) string {
	if s != nil && s.InCustomProfile() {
		return s.Profile
	}
	return api.DefaultCustomProfile
}

// ActivateBlock returns "" when name can be activated right now, else the
// reason the Activate affordance must be insensitive. Firmware profiles are
// always activatable; a custom profile needs settings to apply, and one
// already running has nothing to do.
func ActivateBlock(s *api.State, name string) string {
	if api.IsStockProfileName(name) {
		return ""
	}
	if s != nil && s.Profile == name {
		return "already the active profile"
	}
	var p api.CustomProfile
	var saved bool
	if s != nil {
		p, saved = s.CustomProfiles[name]
	}
	if !saved || p.Empty() {
		return "no settings saved yet — add some below first"
	}
	return ""
}

// DeleteBlockFor mirrors the daemon's profile-delete refusals for one custom
// profile, so the reason the button is insensitive and the error the daemon
// would have returned agree.
func DeleteBlockFor(s *api.State, name string) string {
	var saved bool
	if s != nil {
		_, saved = s.CustomProfiles[name]
	}
	if !saved {
		return "nothing is saved under this name"
	}
	if s.Profile == name {
		return "this profile is active — switch to another profile first"
	}
	if a := s.Autoswitch; a != nil {
		if a.AC == name {
			return "this profile is the autoswitch AC target — change that first"
		}
		if a.Battery == name {
			return "this profile is the autoswitch battery target — change that first"
		}
	}
	return ""
}

// Label returns the display text for a profile name: the stock profiles and
// "custom" are title-cased as the drawer always has, while a named profile is
// shown verbatim — it is an identifier the user typed, and "My-Profile" for
// "my-profile" would suggest a name that does not exist.
func Label(name string) string {
	if api.IsStockProfileName(name) || name == api.DefaultCustomProfile {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

// Signature identifies the row structure. The GTK side rebuilds the selector
// (and its focus rows) only when this changes; Active and the block reasons
// are applied to the existing widgets instead, so a state refresh during
// interaction does not tear the buttons out from under the pointer.
func Signature(rows []Row) string {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	return strings.Join(names, "\x00")
}
