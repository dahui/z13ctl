// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// BuildThemeCSS generates a complete theme CSS string for the given colors.
// It prepends @define-color declarations, then appends the rules from
// templateCSS (with its own @define-color lines stripped to avoid duplication).
// The templateCSS is typically the embedded theme-default.css from the gui package.
func BuildThemeCSS(c Colors, templateCSS string) string {
	defs := fmt.Sprintf(
		"@define-color z13-accent      %s;\n"+
			"@define-color z13-bg          %s;\n"+
			"@define-color z13-surface     %s;\n"+
			"@define-color z13-surface-alt %s;\n"+
			"@define-color z13-text        %s;\n"+
			"@define-color z13-text-dim    %s;\n"+
			"@define-color z13-border      %s;\n"+
			"@define-color z13-error       %s;\n",
		c.Accent, c.Background, c.Surface, c.SurfaceAlt, c.Text, c.TextDim, c.Border, c.Error,
	)
	return defs + "\n" + StripDefineColors(templateCSS)
}

// definePattern matches an "@define-color z13-foo …;" declaration, capturing the
// token name. referencePattern matches a "@z13-foo" use.
var (
	definePattern    = regexp.MustCompile(`@define-color\s+(z13-[a-z0-9-]+)`)
	referencePattern = regexp.MustCompile(`@(z13-[a-z0-9-]+)`)
)

// UndefinedColorTokens returns, sorted, the @z13-* tokens css references without
// also defining — the tokens that make a stylesheet fail to stand on its own.
//
// This distinction is easy to miss because the two ways of supplying a theme are
// not symmetrical. A theme.toml is substituted into a copy of the embedded
// template, which is prefixed with a full set of @define-color lines, so a missing
// definition there is invisible. A theme.css is loaded **verbatim**: anything it
// references and does not define is simply undefined, and every rule using it is
// dropped. The embedded template shipped for months referencing @z13-error without
// defining it, so anyone who followed its own instructions and copied it to
// theme.css lost the error bar's and the TDP warning's colours.
//
// References inside comments are counted, deliberately: the template documents its
// tokens in a comment block, and a token advertised there but never defined is the
// same broken promise to whoever copies the file.
func UndefinedColorTokens(css string) []string {
	defined := make(map[string]bool)
	for _, m := range definePattern.FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	seen := make(map[string]bool)
	var missing []string
	for _, m := range referencePattern.FindAllStringSubmatch(css, -1) {
		tok := m[1]
		// A define's own name matches referencePattern too; skip those positions by
		// checking definition membership first.
		if defined[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		missing = append(missing, tok)
	}
	sort.Strings(missing)
	return missing
}

// StripDefineColors removes all @define-color lines from a CSS string.
func StripDefineColors(css string) string {
	var b strings.Builder
	for _, line := range strings.Split(css, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "@define-color") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
