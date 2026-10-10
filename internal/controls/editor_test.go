// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package controls_test

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/controls"
)

func rowIDs(rows []controls.EditorRow) (all, shown []string) {
	for _, r := range rows {
		all = append(all, r.Control.ID)
		if r.Shown {
			shown = append(shown, r.Control.ID)
		}
	}
	return all, shown
}

func list(ids ...string) *[]string { return &ids }

// With no saved list the editor shows the default drawer, and offers the
// opt-in controls hidden at their place in the default order.
func TestEditorRowsDefault(t *testing.T) {
	rows := controls.EditorRows(nil, withBoost(z13()), controls.CapRefreshRate)
	all, shown := rowIDs(rows)
	if want := []string{"profile", "autoswitch", "battery", "cpu-boost", "refresh-rate", "lighting"}; !equal(all, want) {
		t.Errorf("rows = %v, want %v", all, want)
	}
	if want := []string{"profile", "autoswitch", "battery", "lighting"}; !equal(shown, want) {
		t.Errorf("shown = %v, want %v", shown, want)
	}
}

// A control the machine cannot do is not offered at all.
func TestEditorRowsOfferOnlyWhatIsSupported(t *testing.T) {
	all, _ := rowIDs(controls.EditorRows(nil, z13())) // no boost, no display backend
	if want := []string{"profile", "autoswitch", "battery", "lighting"}; !equal(all, want) {
		t.Errorf("rows = %v, want %v", all, want)
	}
}

// A hidden control goes back after its nearest default-order predecessor that
// is in the list, wherever the user moved that one — so switching CPU boost on
// lands it with the power controls, not at the bottom under RGB.
func TestEditorRowsPlaceHiddenControlsByTheDefaultOrder(t *testing.T) {
	saved := list("lighting", "battery", "profile")
	all, shown := rowIDs(controls.EditorRows(saved, withBoost(z13()), controls.CapRefreshRate))
	want := []string{"lighting", "battery", "cpu-boost", "refresh-rate", "profile", "autoswitch"}
	if !equal(all, want) {
		t.Errorf("rows = %v, want %v", all, want)
	}
	if !equal(shown, []string{"lighting", "battery", "profile"}) {
		t.Errorf("shown = %v", shown)
	}

	// Nothing before it shown: it goes first.
	all, _ = rowIDs(controls.EditorRows(list("lighting"), z13()))
	if want := []string{"profile", "autoswitch", "battery", "lighting"}; !equal(all, want) {
		t.Errorf("rows = %v, want %v", all, want)
	}
}

func TestMoveRow(t *testing.T) {
	rows := controls.EditorRows(nil, z13())
	got, ok := controls.MoveRow(rows, 3, -1)
	if all, _ := rowIDs(got); !ok || !equal(all, []string{"profile", "autoswitch", "lighting", "battery"}) {
		t.Errorf("move lighting up = %v, %v", all, ok)
	}
	if all, _ := rowIDs(rows); all[3] != "lighting" {
		t.Error("MoveRow changed its input")
	}
	for _, tc := range []struct{ i, d int }{{0, -1}, {3, 1}, {-1, 1}, {4, -1}} {
		if _, ok := controls.MoveRow(rows, tc.i, tc.d); ok {
			t.Errorf("MoveRow(%d, %d) = ok, want refused", tc.i, tc.d)
		}
	}
}

func TestSavedList(t *testing.T) {
	info := withBoost(z13())

	// Unchanged defaults save as nil, so a later build's new default control
	// still reaches a user who once opened the editor.
	rows := controls.EditorRows(nil, info)
	if got := controls.SavedList(rows, nil, info); got != nil {
		t.Errorf("defaults saved as %v, want nil", *got)
	}

	// Moved back to the defaults by hand is the same answer.
	moved, _ := controls.MoveRow(rows, 0, 1)
	back, _ := controls.MoveRow(moved, 1, -1)
	if got := controls.SavedList(back, list("autoswitch", "profile", "battery", "lighting"), info); got != nil {
		t.Errorf("restored defaults saved as %v, want nil", *got)
	}

	// A change is the shown IDs in order.
	rows[3].Shown = true // cpu-boost
	if got := controls.SavedList(rows, nil, info); got == nil ||
		!equal(*got, []string{"profile", "autoswitch", "battery", "cpu-boost", "lighting"}) {
		t.Errorf("saved = %v", got)
	}

	// IDs this machine has no row for survive, after the shown ones.
	prev := list("refresh-rate", "battery", "warp-drive")
	rows = controls.EditorRows(prev, info) // no display backend here
	got := controls.SavedList(rows, prev, info)
	if got == nil || !equal(*got, []string{"battery", "refresh-rate", "warp-drive"}) {
		t.Errorf("saved = %v, want battery then the kept IDs", got)
	}

	// Hiding everything is a choice, not "no choice".
	for i := range rows {
		rows[i].Shown = false
	}
	if got := controls.SavedList(rows, nil, info); got == nil || len(*got) != 0 {
		t.Errorf("all hidden saved as %v, want an empty list", got)
	}
}

// The opt-in controls resolve when listed and supported, and are dropped
// quietly where the session or device cannot do them.
func TestOptInControlsResolve(t *testing.T) {
	cfg := controls.Config{Quickbar: controls.QuickbarConfig{Controls: list("refresh-rate", "cpu-boost")}}
	got, complaints := controls.Resolve(cfg, withBoost(z13()), controls.CapRefreshRate)
	if !equal(ids(got), []string{"refresh-rate", "cpu-boost"}) || len(complaints) != 0 {
		t.Errorf("resolve = %v, %v", ids(got), complaints)
	}
	got, _ = controls.Resolve(cfg, z13())
	if len(got) != 0 {
		t.Errorf("without the capabilities = %v, want nothing", ids(got))
	}
	// A nil document keeps document capabilities, never the session one.
	got, _ = controls.Resolve(cfg, nil)
	if !equal(ids(got), []string{"cpu-boost"}) {
		t.Errorf("nil document = %v, want [cpu-boost]", ids(got))
	}
}

func TestLayoutGroupsTheOptInControls(t *testing.T) {
	cfg := controls.Config{Quickbar: controls.QuickbarConfig{Controls: list("profile", "cpu-boost", "refresh-rate", "lighting")}}
	resolved, _ := controls.Resolve(cfg, withBoost(z13()), controls.CapRefreshRate)
	var heads []string
	for _, r := range controls.Layout(resolved) {
		if r.Heading != "" {
			heads = append(heads, r.Heading)
		}
	}
	if want := []string{controls.GroupPower, controls.GroupDisplay, controls.GroupRGB}; !equal(heads, want) {
		t.Errorf("headings = %v, want %v", heads, want)
	}
}

func TestListEncoding(t *testing.T) {
	for _, l := range []*[]string{nil, {}, {"battery"}, {"profile", "cpu-boost", "lighting"}} {
		got := controls.ParseList(controls.FormatList(l))
		if (got == nil) != (l == nil) || (l != nil && !equal(*got, *l)) {
			t.Errorf("round trip of %v = %v", l, got)
		}
	}
	if got := controls.FormatList(&[]string{}); got != "none" {
		t.Errorf("empty list = %q, want none", got)
	}
}

// gui.toml is hand-edited and wins; the editor's saved list applies only
// when it has none.
func TestEffectiveList(t *testing.T) {
	hand := controls.Config{Quickbar: controls.QuickbarConfig{Controls: list("lighting"), Edge: "left"}}
	cfg, locked := controls.Effective(hand, "battery")
	if !locked || !equal(*cfg.Quickbar.Controls, []string{"lighting"}) {
		t.Errorf("gui.toml list: %v locked=%v", *cfg.Quickbar.Controls, locked)
	}
	cfg, locked = controls.Effective(controls.Config{Quickbar: controls.QuickbarConfig{Edge: "left"}}, "battery")
	if locked || !equal(*cfg.Quickbar.Controls, []string{"battery"}) || cfg.Quickbar.Edge != "left" {
		t.Errorf("saved list: %+v locked=%v", cfg, locked)
	}
	if cfg, _ = controls.Effective(controls.Config{}, ""); cfg.Quickbar.Controls != nil {
		t.Error("nothing saved should leave the defaults")
	}
}
