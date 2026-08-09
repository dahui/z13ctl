// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import "testing"

func TestBuiltinsNotEmpty(t *testing.T) {
	if len(Builtins) == 0 {
		t.Fatal("Builtins slice is empty")
	}
}

func TestBuiltinsUniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, b := range Builtins {
		if seen[b.ID] {
			t.Errorf("duplicate builtin theme ID: %q", b.ID)
		}
		seen[b.ID] = true
	}
}

func TestBuiltinsAllColorsSet(t *testing.T) {
	for _, b := range Builtins {
		c := b.Colors
		if c.Accent == "" || c.Background == "" || c.Surface == "" ||
			c.SurfaceAlt == "" || c.Text == "" || c.TextDim == "" || c.Border == "" ||
			c.Error == "" {
			t.Errorf("theme %q has empty color fields", b.ID)
		}
	}
}

func TestBuiltinByID_Found(t *testing.T) {
	c, ok := BuiltinByID("rog-dark")
	if !ok {
		t.Fatal("BuiltinByID(rog-dark) returned false")
	}
	if c.Accent != "#cc0000" {
		t.Errorf("expected accent #cc0000, got %s", c.Accent)
	}
}

func TestBuiltinByID_NotFound(t *testing.T) {
	_, ok := BuiltinByID("nonexistent-theme")
	if ok {
		t.Error("BuiltinByID(nonexistent-theme) should return false")
	}
}

func TestBuiltinByID_AllThemes(t *testing.T) {
	for _, b := range Builtins {
		c, ok := BuiltinByID(b.ID)
		if !ok {
			t.Errorf("BuiltinByID(%q) returned false", b.ID)
			continue
		}
		if c != b.Colors {
			t.Errorf("BuiltinByID(%q) returned mismatched colors", b.ID)
		}
	}
}

func TestBuiltinAccentHex_Found(t *testing.T) {
	hex, ok := BuiltinAccentHex("catppuccin-mocha", "blue")
	if !ok {
		t.Fatal("BuiltinAccentHex(catppuccin-mocha, blue) returned false")
	}
	if hex != "#89b4fa" {
		t.Errorf("expected #89b4fa, got %s", hex)
	}
}

func TestBuiltinAccentHex_MissingAccent(t *testing.T) {
	_, ok := BuiltinAccentHex("catppuccin-mocha", "nonexistent")
	if ok {
		t.Error("should return false for nonexistent accent")
	}
}

func TestBuiltinAccentHex_MissingTheme(t *testing.T) {
	_, ok := BuiltinAccentHex("nonexistent-theme", "blue")
	if ok {
		t.Error("should return false for nonexistent theme")
	}
}

func TestBuiltinAccentHex_ThemeWithoutAccents(t *testing.T) {
	_, ok := BuiltinAccentHex("rog-dark", "blue")
	if ok {
		t.Error("should return false for theme without accents")
	}
}

func TestBuiltinAccentHex_AllAccents(t *testing.T) {
	for _, b := range Builtins {
		for _, a := range b.Accents {
			hex, ok := BuiltinAccentHex(b.ID, a.ID)
			if !ok {
				t.Errorf("BuiltinAccentHex(%q, %q) returned false", b.ID, a.ID)
				continue
			}
			if hex != a.Hex {
				t.Errorf("BuiltinAccentHex(%q, %q) = %q, want %q", b.ID, a.ID, hex, a.Hex)
			}
		}
	}
}

func TestDefaultColorsValid(t *testing.T) {
	c := DefaultColors
	for _, pair := range []struct {
		name, val string
	}{
		{"Accent", c.Accent},
		{"Background", c.Background},
		{"Surface", c.Surface},
		{"SurfaceAlt", c.SurfaceAlt},
		{"Text", c.Text},
		{"TextDim", c.TextDim},
		{"Border", c.Border},
		{"Error", c.Error},
	} {
		if !IsHexColor(pair.val) {
			t.Errorf("DefaultColors.%s = %q is not a valid hex color", pair.name, pair.val)
		}
	}
}
