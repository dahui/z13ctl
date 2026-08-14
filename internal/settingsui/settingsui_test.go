// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package settingsui_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/settingsui"
)

// doc is a device declaring the Z13's two toggles, in document order.
func doc() *api.DeviceInfo {
	return &api.DeviceInfo{Toggles: []api.ToggleInfo{
		{ID: "boot_sound", Label: "POST boot sound", Kind: api.ToggleKindBool,
			Description: "Play the startup sound when the laptop powers on.", Source: "core"},
		{ID: "panel_overdrive", Label: "Panel overdrive", Kind: api.ToggleKindBool,
			Description: "Faster pixel response; may cause ghosting.", Source: "core"},
	}}
}

func TestRows(t *testing.T) {
	t.Parallel()

	t.Run("document order, values from Features", func(t *testing.T) {
		t.Parallel()
		rows := settingsui.Rows(doc(), &api.State{
			Features: map[string]int{"boot_sound": 0, "panel_overdrive": 1},
		})
		if len(rows) != 2 {
			t.Fatalf("got %d rows, want 2", len(rows))
		}
		if rows[0].ID != "boot_sound" || rows[1].ID != "panel_overdrive" {
			t.Errorf("rows are not in document order: %q, %q", rows[0].ID, rows[1].ID)
		}
		if rows[0].On || !rows[0].Known {
			t.Errorf("boot_sound = %+v, want a known off", rows[0])
		}
		if !rows[1].On || !rows[1].Known {
			t.Errorf("panel_overdrive = %+v, want a known on", rows[1])
		}
		// The prose is device data — the reason ToggleInfo.Description exists
		// rather than each client restating it.
		if rows[1].Description == "" {
			t.Error("the ghosting warning did not reach the row")
		}
	})

	t.Run("a toggle absent from Features is unknown, not off", func(t *testing.T) {
		t.Parallel()
		// The daemon omits a toggle it could not read, precisely so that a
		// failed read is distinguishable from "off". Reading absence as zero
		// here would put that claim back, one layer up.
		rows := settingsui.Rows(doc(), &api.State{Features: map[string]int{"boot_sound": 1}})
		if !rows[0].Known {
			t.Error("boot_sound was reported and reads as unknown")
		}
		if rows[1].Known {
			t.Errorf("panel_overdrive = %+v, want Known false — it was not reported", rows[1])
		}
		if rows[1].On {
			t.Error("an unreadable toggle claims to be on")
		}
	})

	t.Run("a nil state is every row unknown", func(t *testing.T) {
		t.Parallel()
		// The page is built before the first sync; rows exist, values do not.
		rows := settingsui.Rows(doc(), nil)
		if len(rows) != 2 {
			t.Fatalf("got %d rows, want 2 — the rows come from the document, not the state", len(rows))
		}
		for _, r := range rows {
			if r.Known {
				t.Errorf("%s claims a known value with no state", r.ID)
			}
		}
	})

	t.Run("a nil document is no rows", func(t *testing.T) {
		t.Parallel()
		// Deliberately unlike controls.SupportsAll and limits.FromDevice, whose
		// nil-document posture is "keep everything": there is no fallback list
		// of toggles to keep, because the rows *are* the document.
		if rows := settingsui.Rows(nil, &api.State{Features: map[string]int{"boot_sound": 1}}); rows != nil {
			t.Errorf("got %+v, want no rows without a document", rows)
		}
	})

	t.Run("an unrecognized kind is skipped", func(t *testing.T) {
		t.Parallel()
		d := doc()
		d.Toggles = append(d.Toggles, api.ToggleInfo{
			ID: "fan_mode", Label: "Fan mode", Kind: "enum", Source: "core",
		})
		rows := settingsui.Rows(d, nil)
		if len(rows) != 2 {
			t.Fatalf("got %d rows, want 2 — an enum must not be rendered as a switch", len(rows))
		}
	})

	t.Run("a label-less toggle falls back to its id", func(t *testing.T) {
		t.Parallel()
		rows := settingsui.Rows(&api.DeviceInfo{Toggles: []api.ToggleInfo{
			{ID: "mystery_switch", Kind: api.ToggleKindBool},
		}}, nil)
		if len(rows) != 1 || rows[0].Label != "mystery_switch" {
			t.Errorf("got %+v, want the id standing in for the missing label", rows)
		}
	})
}

// TestEmptyReason: the three kinds of nothing are distinguishable and call for
// different responses, which is why the page does not print one string.
func TestEmptyReason(t *testing.T) {
	t.Parallel()

	if got := settingsui.EmptyReason(doc()); got != "" {
		t.Errorf("EmptyReason with rows to draw = %q, want empty", got)
	}

	noDaemon := settingsui.EmptyReason(nil)
	noToggles := settingsui.EmptyReason(&api.DeviceInfo{})
	unknownKind := settingsui.EmptyReason(&api.DeviceInfo{Toggles: []api.ToggleInfo{
		{ID: "fan_mode", Kind: "enum"},
	}})

	for name, got := range map[string]string{
		"no daemon": noDaemon, "no toggles": noToggles, "unknown kind": unknownKind,
	} {
		if got == "" {
			t.Errorf("%s: an empty page must say which kind of nothing it is", name)
		}
	}
	if noDaemon == noToggles || noToggles == unknownKind || noDaemon == unknownKind {
		t.Error("the three empty states must not share a message: one is fixed by " +
			"starting the daemon, one by nothing, and one is a voltaire limitation")
	}
}
