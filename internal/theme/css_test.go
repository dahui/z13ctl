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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the embedded theme template: %v", err)
	}
	if missing := UndefinedColorTokens(string(data)); len(missing) > 0 {
		t.Errorf("%s references colour tokens it does not define: %v\n"+
			"It is documented as a starting point for theme.css, which is loaded "+
			"verbatim, so every rule using these would be dropped.", path, missing)
	}
}

// The template must also define exactly the tokens Colors carries, so that a
// verbatim copy and a BuildThemeCSS-substituted copy style the same things. A
// token in Colors but not the template means the standalone path is missing a
// colour; the reverse means BuildThemeCSS cannot override one.
func TestEmbeddedTemplateDefinesEveryColorToken(t *testing.T) {
	data, err := os.ReadFile("../gui/theme-default.css")
	if err != nil {
		t.Fatalf("cannot read the embedded theme template: %v", err)
	}
	css := string(data)

	// The tokens BuildThemeCSS emits, taken from its own output so the two cannot
	// drift apart.
	for _, tok := range UndefinedColorTokens(StripDefineColors(BuildThemeCSS(DefaultColors, ""))) {
		t.Errorf("BuildThemeCSS emits a reference to %s that it does not define", tok)
	}
	generated := BuildThemeCSS(DefaultColors, "")
	for _, m := range definePattern.FindAllStringSubmatch(generated, -1) {
		if !strings.Contains(css, "@define-color "+m[1]) &&
			!regexp.MustCompile(`@define-color\s+`+regexp.QuoteMeta(m[1])).MatchString(css) {
			t.Errorf("BuildThemeCSS defines %s but the template does not, so a "+
				"verbatim theme.css copy would leave it undefined", m[1])
		}
	}
}
