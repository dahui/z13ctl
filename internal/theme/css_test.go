// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestBuildThemeCSS_ContainsDefineColors(t *testing.T) {
	template := "/* template */\n.drawer { background: @z13-bg; }\n"
	css := BuildThemeCSS(DefaultColors, template)

	expected := []string{
		"@define-color z13-accent",
		"@define-color z13-bg",
		"@define-color z13-surface ", // trailing space to distinguish from z13-surface-alt
		"@define-color z13-surface-alt",
		"@define-color z13-text ", // trailing space to distinguish from z13-text-dim
		"@define-color z13-text-dim",
		"@define-color z13-border",
		"@define-color z13-error",
	}
	for _, exp := range expected {
		if !strings.Contains(css, exp) {
			t.Errorf("output missing %q", exp)
		}
	}
}

func TestBuildThemeCSS_ContainsColorValues(t *testing.T) {
	c := Colors{
		Accent:     "#ff0000",
		Background: "#111111",
		Surface:    "#222222",
		SurfaceAlt: "#333333",
		Text:       "#eeeeee",
		TextDim:    "#999999",
		Border:     "#555555",
		Error:      "#ff8888",
	}
	css := BuildThemeCSS(c, "")
	for _, hex := range []string{"#ff0000", "#111111", "#222222", "#333333", "#eeeeee", "#999999", "#555555", "#ff8888"} {
		if !strings.Contains(css, hex) {
			t.Errorf("output missing color value %s", hex)
		}
	}
}

func TestBuildThemeCSS_StripsTemplateDefineColors(t *testing.T) {
	template := "@define-color z13-accent #old;\n.drawer { color: @z13-text; }\n"
	css := BuildThemeCSS(DefaultColors, template)

	// The template's @define-color line should be stripped.
	lines := strings.Split(css, "\n")
	oldFound := false
	for _, l := range lines {
		if strings.Contains(l, "#old") {
			oldFound = true
		}
	}
	if oldFound {
		t.Error("template @define-color was not stripped")
	}

	// But the template rule should remain.
	if !strings.Contains(css, ".drawer") {
		t.Error("template rule was incorrectly stripped")
	}
}

func TestBuildThemeCSS_PreservesTemplateRules(t *testing.T) {
	template := ".drawer { background: @z13-bg; }\n.bottom-bar { margin: 4px; }\n"
	css := BuildThemeCSS(DefaultColors, template)
	if !strings.Contains(css, ".drawer") {
		t.Error("missing .drawer rule")
	}
	if !strings.Contains(css, ".bottom-bar") {
		t.Error("missing .bottom-bar rule")
	}
}

func TestStripDefineColors(t *testing.T) {
	input := "@define-color z13-accent #cc0000;\n.foo { color: red; }\n@define-color z13-bg #1a1a1a;\n.bar { }\n"
	got := StripDefineColors(input)
	if strings.Contains(got, "@define-color") {
		t.Error("@define-color lines were not stripped")
	}
	if !strings.Contains(got, ".foo") {
		t.Error(".foo rule was incorrectly stripped")
	}
	if !strings.Contains(got, ".bar") {
		t.Error(".bar rule was incorrectly stripped")
	}
}

func TestStripDefineColors_IndentedLine(t *testing.T) {
	input := "  @define-color z13-accent #cc0000;\n.foo { }\n"
	got := StripDefineColors(input)
	if strings.Contains(got, "@define-color") {
		t.Error("indented @define-color line was not stripped")
	}
}

func TestStripDefineColors_EmptyInput(t *testing.T) {
	got := StripDefineColors("")
	if got != "\n" { // strings.Split("", "\n") produces one empty element
		t.Errorf("unexpected output for empty input: %q", got)
	}
}

func TestBuildThemeCSS_EmptyTemplate(t *testing.T) {
	css := BuildThemeCSS(DefaultColors, "")
	if !strings.Contains(css, "@define-color z13-accent") {
		t.Error("even with empty template, @define-color lines should be present")
	}
}

func TestUndefinedColorTokens(t *testing.T) {
	tests := []struct {
		name string
		css  string
		want []string
	}{
		{
			name: "self-contained",
			css:  "@define-color z13-accent #cc0000;\n.a { color: @z13-accent; }\n",
			want: nil,
		},
		{
			// The shipped bug: four rules used @z13-error and nothing defined it.
			name: "referenced but never defined",
			css:  "@define-color z13-accent #cc0000;\n.a { color: @z13-error; }\n",
			want: []string{"z13-error"},
		},
		{
			name: "several missing, sorted and deduplicated",
			css:  ".a { color: @z13-text; border-color: @z13-border; }\n.b { color: @z13-text; }\n",
			want: []string{"z13-border", "z13-text"},
		},
		{
			// A token advertised in a comment but never defined is the same broken
			// promise to anyone copying the file, so it counts.
			name: "advertised in a comment only",
			css:  "/* Available: @z13-radius — corner radius */\n.a { color: red; }\n",
			want: []string{"z13-radius"},
		},
		{
			name: "a define does not count as an undefined reference",
			css:  "@define-color z13-bg #1a1a1a;\n",
			want: nil,
		},
		{
			name: "no tokens at all",
			css:  ".a { color: red; }\n",
			want: nil,
		},
		{
			name: "empty",
			css:  "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UndefinedColorTokens(tt.css)
			if len(got) != len(tt.want) {
				t.Fatalf("UndefinedColorTokens = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("UndefinedColorTokens = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestEmbeddedTemplateIsSelfContained is the regression guard for the bug above.
//
// The template lives in internal/gui, which needs CGO and GTK4 headers and so has
// no tests of its own — hence reaching across for the file rather than importing
// it. The assertion is worth the awkward path: the template doubles as the
// documented starting point for a hand-written theme.css, which is loaded verbatim,
// so a token it references without defining silently drops every rule that uses it.
func TestEmbeddedTemplateIsSelfContained(t *testing.T) {
	const path = "../gui/theme-default.css"
	if missing := UndefinedColorTokens(readTemplate(t)); len(missing) > 0 {
		t.Errorf("%s references colour tokens it does not define: %v\n"+
			"It is documented as a starting point for theme.css, which is loaded "+
			"verbatim, so every rule using these would be dropped.", path, missing)
	}
}

// Every token the template defines must also be defined by BuildThemeCSS.
//
// The direction matters and is easy to write backwards. BuildThemeCSS strips
// the template's own @define-color lines and prepends its own, so a token the
// template defines and BuildThemeCSS does not is a colour a theme.toml can
// never change: the standalone copy has it, the substituted copy has nothing.
// The reverse is not a defect — BuildThemeCSS also emits the @z13-* aliases,
// which nothing in the tree references any more and which exist only for a
// third-party sheet written against the old names.
func TestBuildThemeCSSCanOverrideEveryTemplateToken(t *testing.T) {
	css := readTemplate(t)
	generated := BuildThemeCSS(DefaultColors, "")

	for _, m := range definePattern.FindAllStringSubmatch(css, -1) {
		tok := m[1]
		if !regexp.MustCompile(`@define-color\s+` + regexp.QuoteMeta(tok) + `\s`).MatchString(generated) {
			t.Errorf("the template defines %s but BuildThemeCSS does not, so a "+
				"theme.toml could never change that colour", tok)
		}
	}

	// And BuildThemeCSS's own output must stand on its own too.
	for _, tok := range UndefinedColorTokens(StripDefineColors(generated)) {
		t.Errorf("BuildThemeCSS emits a reference to %s that it does not define", tok)
	}
}

// TestTheComposedSheetHasNoUndefinedTokens checks the thing the app actually
// loads: BuildThemeCSS's defines followed by the template with its own defines
// stripped. The two halves are verified separately elsewhere, and a rename
// applied to only one of them passes both of those and still ships a drawer
// missing a colour.
//
// It has to be a test. GTK4 drops a rule with an unresolvable colour **without
// logging anything** — verified by breaking a token deliberately and watching a
// full debug run stay silent — so there is no runtime signal to notice, and the
// symptom is a border or a label that is simply the wrong colour.
func TestTheComposedSheetHasNoUndefinedTokens(t *testing.T) {
	composed := BuildThemeCSS(DefaultColors, readTemplate(t))
	if missing := UndefinedColorTokens(composed); len(missing) > 0 {
		t.Errorf("the composed stylesheet references colour tokens nothing defines: %v\n"+
			"GTK drops every rule using them, silently.", missing)
	}
}

// TestTemplateUsesTheCurrentTokenNames pins the migration. The bundled sheet
// referenced @z13-* until the rename was staged through it; a rule reverting to
// the old prefix would still *work* — BuildThemeCSS defines both names — right
// up until 3.0 deletes the aliases, at which point it would silently lose its
// colour. Catching it here is the difference between a failing test now and an
// unstyled control in a major release.
func TestTemplateUsesTheCurrentTokenNames(t *testing.T) {
	css := readTemplate(t)
	if legacy := regexp.MustCompile(`@z13-[a-z0-9-]+`).FindAllString(css, -1); len(legacy) > 0 {
		t.Errorf("the bundled template still uses the pre-rename token names: %v\n"+
			"They are aliases removed at 3.0; use the @voltaire-* names.", legacy)
	}
}

// TestLegacyAliasesAreStillEmitted keeps the 2.x compatibility promise honest.
// Nothing in the tree references @z13-* any more, so dropping the aliases would
// break nothing here and everything in a third-party theme.css written against
// them. Removing this test is the deliberate act that 3.0 requires.
func TestLegacyAliasesAreStillEmitted(t *testing.T) {
	generated := BuildThemeCSS(DefaultColors, "")
	for _, tok := range []string{"z13-accent", "z13-bg", "z13-surface", "z13-surface-alt",
		"z13-text", "z13-text-dim", "z13-border", "z13-error"} {
		if !regexp.MustCompile(`@define-color\s+` + tok + `\s`).MatchString(generated) {
			t.Errorf("BuildThemeCSS no longer defines @%s; the alias is promised through 2.x", tok)
		}
	}
}

// readTemplate reads the sheet embedded by internal/gui.
//
// Reaching across packages for a file is deliberate: the template lives in the
// cgo island, which has no tests of its own, and it doubles as the documented
// starting point for a hand-written theme.css.
func readTemplate(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../gui/theme-default.css")
	if err != nil {
		t.Fatalf("cannot read the embedded theme template: %v", err)
	}
	return string(data)
}
