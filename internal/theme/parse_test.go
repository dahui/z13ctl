// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import "testing"

func TestIsHexColor(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"#abc", true},
		{"#ABC", true},
		{"#aabbcc", true},
		{"#AABBCC", true},
		{"#aabbccdd", true},
		{"#AABBCCDD", true},
		{"#123", true},
		{"#112233", true},
		{"#11223344", true},
		{"", false},
		{"abc", false},
		{"#", false},
		{"#ab", false},
		{"#abcd", false},
		{"#abcde", false},
		{"#abcdefg", false},
		{"#gggggg", false},
		{"#zzzzzz", false},
		{"#abcdef ", false}, // trailing space makes length wrong
		{" #abcdef", false}, // leading space
		{"#12345", false},
	}
	for _, tt := range tests {
		got := IsHexColor(tt.input)
		if got != tt.want {
			t.Errorf("IsHexColor(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseThemeTOML_Full(t *testing.T) {
	data := []byte(`
accent = "#ff0000"
background = "#111111"
surface = "#222222"
surface_alt = "#333333"
text = "#eeeeee"
text_dim = "#999999"
border = "#555555"
error = "#ff8888"
`)
	c := ParseThemeTOML(data)
	if c.Accent != "#ff0000" {
		t.Errorf("Accent = %q, want #ff0000", c.Accent)
	}
	if c.Background != "#111111" {
		t.Errorf("Background = %q, want #111111", c.Background)
	}
	if c.Surface != "#222222" {
		t.Errorf("Surface = %q, want #222222", c.Surface)
	}
	if c.SurfaceAlt != "#333333" {
		t.Errorf("SurfaceAlt = %q, want #333333", c.SurfaceAlt)
	}
	if c.Text != "#eeeeee" {
		t.Errorf("Text = %q, want #eeeeee", c.Text)
	}
	if c.TextDim != "#999999" {
		t.Errorf("TextDim = %q, want #999999", c.TextDim)
	}
	if c.Border != "#555555" {
		t.Errorf("Border = %q, want #555555", c.Border)
	}
	if c.Error != "#ff8888" {
		t.Errorf("Error = %q, want #ff8888", c.Error)
	}
}

// A theme.toml written before the error color existed must keep working: the
// parser starts from DefaultColors, so the missing key inherits the default
// rather than producing an empty @define-color that would break the stylesheet.
func TestParseThemeTOML_PreErrorKeyThemeStillWorks(t *testing.T) {
	data := []byte(`
accent = "#ff0000"
background = "#111111"
surface = "#222222"
surface_alt = "#333333"
text = "#eeeeee"
text_dim = "#999999"
border = "#555555"
`)
	c := ParseThemeTOML(data)
	if c.Error != DefaultColors.Error {
		t.Errorf("Error = %q, want default %q", c.Error, DefaultColors.Error)
	}
	if !IsHexColor(c.Error) {
		t.Errorf("Error = %q is not a valid hex color; generated CSS would be malformed", c.Error)
	}
}

func TestParseThemeTOML_MissingKeysKeepDefaults(t *testing.T) {
	data := []byte(`accent = "#ff0000"`)
	c := ParseThemeTOML(data)
	if c.Accent != "#ff0000" {
		t.Errorf("Accent = %q, want #ff0000", c.Accent)
	}
	if c.Background != DefaultColors.Background {
		t.Errorf("Background = %q, want default %q", c.Background, DefaultColors.Background)
	}
}

func TestParseThemeTOML_InvalidHexIgnored(t *testing.T) {
	data := []byte(`accent = "not-a-color"`)
	c := ParseThemeTOML(data)
	if c.Accent != DefaultColors.Accent {
		t.Errorf("Accent = %q, want default %q", c.Accent, DefaultColors.Accent)
	}
}

func TestParseThemeTOML_CommentsAndBlanks(t *testing.T) {
	data := []byte(`
# This is a comment
accent = "#aabbcc"

# Another comment
background = "#112233"
`)
	c := ParseThemeTOML(data)
	if c.Accent != "#aabbcc" {
		t.Errorf("Accent = %q, want #aabbcc", c.Accent)
	}
	if c.Background != "#112233" {
		t.Errorf("Background = %q, want #112233", c.Background)
	}
}

func TestParseThemeTOML_InlineComments(t *testing.T) {
	data := []byte(`accent = "#aabbcc" # the accent color`)
	c := ParseThemeTOML(data)
	if c.Accent != "#aabbcc" {
		t.Errorf("Accent = %q, want #aabbcc", c.Accent)
	}
}

func TestParseThemeTOML_QuotedValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"double-quoted", `accent = "#aabbcc"`, "#aabbcc"},
		{"single-quoted", `accent = '#aabbcc'`, "#aabbcc"},
		// Unquoted hex values with # are ambiguous: ` #aabbcc` matches the
		// inline-comment heuristic, so the parser strips the value. Values
		// must be quoted to survive the inline-comment stripping.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ParseThemeTOML([]byte(tt.input))
			if c.Accent != tt.want {
				t.Errorf("Accent = %q, want %q", c.Accent, tt.want)
			}
		})
	}
}

func TestParseThemeTOML_UnknownKeysIgnored(t *testing.T) {
	data := []byte(`
accent = "#ff0000"
unknown_key = "#00ff00"
another_thing = "hello"
`)
	c := ParseThemeTOML(data)
	if c.Accent != "#ff0000" {
		t.Errorf("Accent = %q, want #ff0000", c.Accent)
	}
	// No panic, unknown keys silently ignored.
}

func TestParseThemeTOML_Empty(t *testing.T) {
	c := ParseThemeTOML([]byte(""))
	if c != DefaultColors {
		t.Error("empty input should return DefaultColors")
	}
}

func TestParseThemeTOML_NoEquals(t *testing.T) {
	data := []byte("this is not valid toml\n")
	c := ParseThemeTOML(data)
	if c != DefaultColors {
		t.Error("invalid lines should be ignored, returning DefaultColors")
	}
}

func TestParseThemeTOMLFull_NoAccents(t *testing.T) {
	data := []byte(`accent = "#ff0000"`)
	c, accents := ParseThemeTOMLFull(data)
	if c.Accent != "#ff0000" {
		t.Errorf("Accent = %q, want #ff0000", c.Accent)
	}
	if len(accents) != 0 {
		t.Errorf("got %d accents, want 0", len(accents))
	}
}

func TestParseThemeTOMLFull_WithAccents(t *testing.T) {
	data := []byte(`
accent = "#5294e2"
background = "#1b2838"

[accents]
blue = "#5294e2"
teal = "#2eb398"
purple = "#9b59b6"
`)
	c, accents := ParseThemeTOMLFull(data)
	if c.Accent != "#5294e2" {
		t.Errorf("Accent = %q, want #5294e2", c.Accent)
	}
	if c.Background != "#1b2838" {
		t.Errorf("Background = %q, want #1b2838", c.Background)
	}
	if len(accents) != 3 {
		t.Fatalf("got %d accents, want 3", len(accents))
	}
	want := []Accent{
		{ID: "blue", Name: "Blue", Hex: "#5294e2"},
		{ID: "teal", Name: "Teal", Hex: "#2eb398"},
		{ID: "purple", Name: "Purple", Hex: "#9b59b6"},
	}
	for i, a := range accents {
		if a != want[i] {
			t.Errorf("accents[%d] = %+v, want %+v", i, a, want[i])
		}
	}
}

func TestParseThemeTOMLFull_AccentInvalidHex(t *testing.T) {
	data := []byte(`
[accents]
good = "#aabbcc"
bad = "not-a-color"
also_good = "#112233"
`)
	_, accents := ParseThemeTOMLFull(data)
	if len(accents) != 2 {
		t.Fatalf("got %d accents, want 2 (bad hex should be skipped)", len(accents))
	}
	if accents[0].ID != "good" {
		t.Errorf("accents[0].ID = %q, want good", accents[0].ID)
	}
	if accents[1].ID != "also_good" {
		t.Errorf("accents[1].ID = %q, want also_good", accents[1].ID)
	}
}

func TestParseThemeTOMLFull_AccentOrder(t *testing.T) {
	data := []byte(`
[accents]
cherry = "#ff0000"
apple = "#00ff00"
banana = "#0000ff"
`)
	_, accents := ParseThemeTOMLFull(data)
	if len(accents) != 3 {
		t.Fatalf("got %d accents, want 3", len(accents))
	}
	ids := []string{accents[0].ID, accents[1].ID, accents[2].ID}
	if ids[0] != "cherry" || ids[1] != "apple" || ids[2] != "banana" {
		t.Errorf("accent IDs = %v, want [cherry apple banana] (file order)", ids)
	}
}

func TestParseThemeTOMLFull_StopsAtNextSection(t *testing.T) {
	data := []byte(`
accent = "#ff0000"

[accents]
blue = "#0000ff"

[other]
something = "#00ff00"
`)
	c, accents := ParseThemeTOMLFull(data)
	if c.Accent != "#ff0000" {
		t.Errorf("Accent = %q, want #ff0000", c.Accent)
	}
	if len(accents) != 1 {
		t.Fatalf("got %d accents, want 1 (should stop at [other])", len(accents))
	}
	if accents[0].ID != "blue" {
		t.Errorf("accents[0].ID = %q, want blue", accents[0].ID)
	}
}

func TestParseThemeTOMLFull_CommentsInAccents(t *testing.T) {
	data := []byte(`
[accents]
# This is a comment
blue = "#0000ff"
# Another comment
red = "#ff0000"
`)
	_, accents := ParseThemeTOMLFull(data)
	if len(accents) != 2 {
		t.Fatalf("got %d accents, want 2", len(accents))
	}
}

func TestParseThemeTOMLFull_AccentTitleCase(t *testing.T) {
	data := []byte(`
[accents]
deep_blue = "#0000ff"
RED = "#ff0000"
`)
	_, accents := ParseThemeTOMLFull(data)
	if len(accents) != 2 {
		t.Fatalf("got %d accents, want 2", len(accents))
	}
	if accents[0].Name != "Deep_blue" {
		t.Errorf("accents[0].Name = %q, want Deep_blue", accents[0].Name)
	}
	if accents[1].Name != "RED" {
		t.Errorf("accents[1].Name = %q, want RED (already uppercase)", accents[1].Name)
	}
}

func TestParseThemeTOML_BackwardCompatible(t *testing.T) {
	// ParseThemeTOML should still work unchanged, ignoring [accents].
	data := []byte(`
accent = "#ff0000"
background = "#111111"

[accents]
blue = "#0000ff"
`)
	c := ParseThemeTOML(data)
	if c.Accent != "#ff0000" {
		t.Errorf("Accent = %q, want #ff0000", c.Accent)
	}
	if c.Background != "#111111" {
		t.Errorf("Background = %q, want #111111", c.Background)
	}
}

// TestParseThemeTOMLValueForms covers both spellings the doc comment promises,
// with and without a trailing comment.
//
// The bare form never worked: comments were stripped by scanning the whole line
// for " #" before the value was isolated, and a hex colour starts with the same
// character a comment does — so `accent = #ff0000` had its own value taken for a
// comment and fell back to the default without a word.
func TestParseThemeTOMLValueForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"quoted", `accent = "#ff0000"`},
		{"bare", `accent = #ff0000`},
		{"quoted with comment", `accent = "#ff0000" # brand red`},
		{"bare with comment", `accent = #ff0000 # brand red`},
		{"no spaces around equals", `accent="#ff0000"`},
		{"single quotes", `accent = '#ff0000'`},
		{"extra whitespace", `   accent   =   "#ff0000"   `},
		{"tab before comment", "accent = #ff0000\t# red"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseThemeTOML([]byte(tt.in)).Accent; got != "#ff0000" {
				t.Errorf("ParseThemeTOML(%q).Accent = %q, want %q", tt.in, got, "#ff0000")
			}
		})
	}
}

// A comment-only or malformed line must leave the default alone rather than
// setting something odd.
func TestParseThemeTOMLIgnoresJunk(t *testing.T) {
	for _, in := range []string{
		`# accent = "#ff0000"`,
		`accent =`,
		`accent = ""`,
		`accent = not-a-colour`,
		`accent = "#12345"`,
		`accent`,
	} {
		if got := ParseThemeTOML([]byte(in)).Accent; got != DefaultColors.Accent {
			t.Errorf("ParseThemeTOML(%q).Accent = %q, want the default %q",
				in, got, DefaultColors.Accent)
		}
	}
}

// Bare values work in the [accents] section too, since it uses the same parser.
func TestParseAccentsAcceptsBareValues(t *testing.T) {
	_, accents := ParseThemeTOMLFull([]byte("[accents]\nblue = #89b4fa\nred = \"#f38ba8\" # muted\n"))
	if len(accents) != 2 {
		t.Fatalf("got %d accents, want 2: %+v", len(accents), accents)
	}
	if accents[0].ID != "blue" || accents[0].Hex != "#89b4fa" {
		t.Errorf("accents[0] = %+v, want blue/#89b4fa", accents[0])
	}
	if accents[1].ID != "red" || accents[1].Hex != "#f38ba8" {
		t.Errorf("accents[1] = %+v, want red/#f38ba8", accents[1])
	}
}
