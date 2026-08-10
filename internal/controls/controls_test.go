// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package controls_test

import (
	"os"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/controls"
)

// z13 is the capability document of a fully-featured device.
func z13() *api.DeviceInfo {
	return &api.DeviceInfo{
		ID: "asus-rog-flow-z13-2025", Model: "GZ302",
		Fans:      &api.FanInfo{Points: 8},
		Power:     &api.PowerInfo{TDPMin: 5},
		Profiles:  &api.ProfileInfo{Names: []string{"quiet", "balanced", "performance"}},
		Lighting:  &api.LightingInfo{Zones: []string{"keyboard", "lightbar"}},
		Battery:   &api.BatteryInfo{ChargeLimit: true, Health: true},
		Telemetry: &api.TelemetryInfo{HistorySeconds: 300},
		Undervolt: &api.UndervoltInfo{Min: -40},
	}
}

func ids(cs []controls.Control) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDefaultOrderIsTheShippedLayout is the guard the whole package exists
// under: a user with no gui.toml must get the drawer they had before the
// registry existed. These four IDs, in this order, are the sections
// buildContent appends between the title row and the bottom bar. Changing this
// list changes every default user's drawer, so it should only ever change
// alongside a deliberate decision to move something.
func TestDefaultOrderIsTheShippedLayout(t *testing.T) {
	want := []string{"profile", "autoswitch", "battery", "lighting"}

	got, complaints := controls.Resolve(controls.Config{}, z13())
	if !equal(ids(got), want) {
		t.Errorf("default resolve = %v, want %v", ids(got), want)
	}
	if len(complaints) != 0 {
		t.Errorf("default config produced complaints: %v", complaints)
	}
	if !equal(controls.IDs(), want) {
		t.Errorf("IDs() = %v, want %v — All and IDs must agree", controls.IDs(), want)
	}
}

// TestLayoutReproducesTheShippedChrome pins the panel's visual structure, not
// just its control order: with no gui.toml the drawer must emit the heading,
// three sections, separator, heading, lighting sequence buildContent used to
// spell out literally.
func TestLayoutReproducesTheShippedChrome(t *testing.T) {
	resolved, _ := controls.Resolve(controls.Config{}, z13())
	rows := controls.Layout(resolved)

	want := []controls.Row{
		{Heading: "TDP AND POWER"},
		{},
		{},
		{Separator: true, Heading: "RGB"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i := range want {
		if rows[i].Separator != want[i].Separator || rows[i].Heading != want[i].Heading {
			t.Errorf("row %d (%s) = {sep:%v heading:%q}, want {sep:%v heading:%q}",
				i, rows[i].Control.ID, rows[i].Separator, rows[i].Heading,
				want[i].Separator, want[i].Heading)
		}
	}
}

func TestLayoutFollowsTheUsersOrder(t *testing.T) {
	cases := []struct {
		name string
		list []string
		want []controls.Row
	}{
		{
			// The first group never gets a separator, whichever group it is.
			name: "reordered groups",
			list: []string{"lighting", "battery"},
			want: []controls.Row{{Heading: "RGB"}, {Separator: true, Heading: "TDP AND POWER"}},
		},
		{
			// A group split by another repeats its heading. That is the honest
			// rendering of what the user asked for — the alternative is a
			// section sitting under a heading that does not describe it.
			name: "interleaved groups repeat the heading",
			list: []string{"profile", "lighting", "battery"},
			want: []controls.Row{
				{Heading: "TDP AND POWER"},
				{Separator: true, Heading: "RGB"},
				{Separator: true, Heading: "TDP AND POWER"},
			},
		},
		{
			name: "a single control gets its heading and no separator",
			list: []string{"battery"},
			want: []controls.Row{{Heading: "TDP AND POWER"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := tc.list
			cfg := controls.Config{Quickbar: controls.QuickbarConfig{Controls: &list}}
			resolved, _ := controls.Resolve(cfg, z13())
			rows := controls.Layout(resolved)
			if len(rows) != len(tc.want) {
				t.Fatalf("got %d rows, want %d", len(rows), len(tc.want))
			}
			for i := range tc.want {
				if rows[i].Separator != tc.want[i].Separator || rows[i].Heading != tc.want[i].Heading {
					t.Errorf("row %d (%s) = {sep:%v heading:%q}, want {sep:%v heading:%q}",
						i, rows[i].Control.ID, rows[i].Separator, rows[i].Heading,
						tc.want[i].Separator, tc.want[i].Heading)
				}
			}
		})
	}

	if got := controls.Layout(nil); len(got) != 0 {
		t.Errorf("Layout(nil) = %v, want nothing", got)
	}
}

func TestNilDocumentKeepsEverything(t *testing.T) {
	// A nil document means the daemon did not answer, not that the machine has
	// no capabilities. The drawer already falls back to built-in limits there;
	// blanking its controls as well would turn "daemon is starting" into an
	// empty panel.
	got, _ := controls.Resolve(controls.Config{}, nil)
	if len(got) != len(controls.All()) {
		t.Errorf("nil document resolved to %v, want everything", ids(got))
	}
}

func TestCapabilityFiltering(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*api.DeviceInfo)
		want []string
	}{
		{"no lighting", func(d *api.DeviceInfo) { d.Lighting = nil },
			[]string{"profile", "autoswitch", "battery"}},
		{"no battery drops autoswitch too", func(d *api.DeviceInfo) { d.Battery = nil },
			// Autoswitch needs both: without a battery the daemon's acPower
			// returns unknown, so the feature could never fire. This is the
			// case the Requires slice exists for.
			[]string{"profile", "lighting"}},
		{"no profiles", func(d *api.DeviceInfo) { d.Profiles = nil },
			[]string{"battery", "lighting"}},
		{"a device with nothing", func(d *api.DeviceInfo) {
			*d = api.DeviceInfo{ID: "bare", Model: "Bare"}
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := z13()
			tc.mut(info)
			got, complaints := controls.Resolve(controls.Config{}, info)
			if !equal(ids(got), tc.want) {
				t.Errorf("resolve = %v, want %v", ids(got), tc.want)
			}
			// An absent capability is the document working as intended, not a
			// user error — a config carried to a second machine must not
			// produce a wall of complaints.
			if len(complaints) != 0 {
				t.Errorf("capability filtering complained: %v", complaints)
			}
		})
	}
}

func TestResolveHonoursTheUsersOrder(t *testing.T) {
	list := []string{"lighting", "battery"}
	cfg := controls.Config{Quickbar: controls.QuickbarConfig{Controls: &list}}

	got, complaints := controls.Resolve(cfg, z13())
	if !equal(ids(got), list) {
		t.Errorf("resolve = %v, want %v", ids(got), list)
	}
	if len(complaints) != 0 {
		t.Errorf("unexpected complaints: %v", complaints)
	}
}

func TestResolveComplaints(t *testing.T) {
	cases := []struct {
		name    string
		list    []string
		want    []string
		mention string
	}{
		{
			// A silent drop turns a typo into a missing section with no
			// explanation, on the one surface that cannot show a parse error.
			name: "unknown id is dropped and named",
			list: []string{"profile", "lightning"},
			want: []string{"profile"}, mention: `unknown control "lightning"`,
		},
		{
			// Building one section twice would give two live widget trees
			// bound to a single piece of state.
			name: "duplicate is kept once at its first position",
			list: []string{"lighting", "profile", "lighting"},
			want: []string{"lighting", "profile"}, mention: "more than once",
		},
		{
			name: "blank entries are ignored quietly",
			list: []string{"profile", "", "  "},
			want: []string{"profile"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := tc.list
			cfg := controls.Config{Quickbar: controls.QuickbarConfig{Controls: &list}}
			got, complaints := controls.Resolve(cfg, z13())
			if !equal(ids(got), tc.want) {
				t.Errorf("resolve = %v, want %v", ids(got), tc.want)
			}
			if tc.mention == "" {
				if len(complaints) != 0 {
					t.Errorf("unexpected complaints: %v", complaints)
				}
				return
			}
			if len(complaints) != 1 || !strings.Contains(complaints[0], tc.mention) {
				t.Errorf("complaints = %v, want one mentioning %q", complaints, tc.mention)
			}
		})
	}
}

func TestEmptyListMeansEmpty(t *testing.T) {
	// `controls = []` is a strange thing to want, but it is unambiguous, and
	// treating it as "no preference" would leave the user editing a file that
	// does nothing. This is why Controls is a pointer.
	empty := []string{}
	cfg := controls.Config{Quickbar: controls.QuickbarConfig{Controls: &empty}}
	got, complaints := controls.Resolve(cfg, z13())
	if len(got) != 0 {
		t.Errorf("resolve = %v, want nothing", ids(got))
	}
	if len(complaints) != 0 {
		t.Errorf("unexpected complaints: %v", complaints)
	}
}

func TestDefaultsAreNotAliased(t *testing.T) {
	// Callers reorder and filter what they are handed; the package's own
	// defaults must not be editable through it.
	a := controls.All()
	a[0].ID = "clobbered"
	a[0].Requires = nil
	if b := controls.All(); b[0].ID == "clobbered" || len(b[0].Requires) == 0 {
		t.Errorf("All() returned a view of the package defaults: %+v", b[0])
	}
}

func TestLoad(t *testing.T) {
	t.Run("missing file is not an error", func(t *testing.T) {
		cfg, err := controls.Load(t.TempDir())
		if err != nil {
			t.Fatalf("Load = %v, want no error for a missing file", err)
		}
		if cfg.Quickbar.Controls != nil {
			t.Error("a missing file must resolve to the defaults, not to an empty list")
		}
	})

	t.Run("round trip", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "[quickbar]\ncontrols = [\"lighting\", \"profile\"]\n")

		cfg, err := controls.Load(dir)
		if err != nil {
			t.Fatalf("Load = %v", err)
		}
		if cfg.Quickbar.Controls == nil {
			t.Fatal("controls list did not parse")
		}
		got, _ := controls.Resolve(cfg, z13())
		if !equal(ids(got), []string{"lighting", "profile"}) {
			t.Errorf("resolve = %v, want [lighting profile]", ids(got))
		}
	})

	t.Run("malformed file is an error the caller can carry on from", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "[quickbar\ncontrols = oops\n")

		cfg, err := controls.Load(dir)
		if err == nil {
			t.Fatal("malformed config parsed without error")
		}
		// The zero Config must still resolve to the defaults, because that is
		// what the caller falls back to: a typo in an optional file must never
		// be what stops the drawer opening.
		got, _ := controls.Resolve(cfg, z13())
		if len(got) != len(controls.All()) {
			t.Errorf("fallback resolve = %v, want the defaults", ids(got))
		}
	})

	t.Run("an unrelated key is ignored", func(t *testing.T) {
		// Config keys are add-only; a file written by a newer voltaire must not
		// stop an older one starting.
		dir := t.TempDir()
		write(t, dir, "[quickbar]\nedge = \"top\"\ncontrols = [\"battery\"]\n")

		cfg, err := controls.Load(dir)
		if err != nil {
			t.Fatalf("Load = %v", err)
		}
		got, _ := controls.Resolve(cfg, z13())
		if !equal(ids(got), []string{"battery"}) {
			t.Errorf("resolve = %v, want [battery]", ids(got))
		}
	})
}

func write(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(dir+"/"+controls.ConfigFile, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
