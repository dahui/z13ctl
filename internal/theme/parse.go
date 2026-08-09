// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import "strings"

// ParseThemeTOML reads a theme.toml file and returns its color definitions.
// Lines of the form key = "value" or key = value are parsed. Comment lines (#)
// and blank lines are ignored. Unknown keys are silently ignored. Invalid hex
// values fall back to the corresponding default. Missing keys keep their default
// value. Any [accents] section is ignored; use ParseThemeTOMLFull to get accents.
func ParseThemeTOML(data []byte) Colors {
	c, _ := ParseThemeTOMLFull(data)
	return c
}

// ParseThemeTOMLFull reads a theme.toml file and returns both its color
// definitions and any accent color variants defined in an [accents] section.
// The [accents] section is optional. Each line in it should be of the form
// id = "#hex". The returned accent slice preserves file order.
func ParseThemeTOMLFull(data []byte) (Colors, []Accent) {
	c := DefaultColors
	var accents []Accent
	inAccents := false

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Detect section headers.
		if strings.HasPrefix(line, "[") {
			inAccents = strings.TrimSpace(line) == "[accents]"
			continue
		}

		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = parseValue(v)
		if !IsHexColor(v) {
			continue
		}

		if inAccents {
			accents = append(accents, Accent{
				ID:   k,
				Name: titleCase(k),
				Hex:  v,
			})
			continue
		}

		switch k {
		case "accent":
			c.Accent = v
		case "background":
			c.Background = v
		case "surface":
			c.Surface = v
		case "surface_alt":
			c.SurfaceAlt = v
		case "text":
			c.Text = v
		case "text_dim":
			c.TextDim = v
		case "border":
			c.Border = v
		case "error":
			c.Error = v
		}
	}
	return c, accents
}

// parseValue extracts the value from the right-hand side of a `key = value` line,
// handling both quoted and bare forms and discarding any trailing comment.
//
// The comment stripping has to happen after the value is isolated, not before.
// Scanning the whole line for " #" first meant the bare form documented here —
// `accent = #ff0000` — had its own value taken for a comment, leaving an empty
// string that silently fell back to the default. A hex colour begins with the
// same character a comment does, which is what made a line-level scan wrong.
func parseValue(rhs string) string {
	v := strings.TrimSpace(rhs)
	if v != "" && (v[0] == '"' || v[0] == '\'') {
		quote := v[0]
		if end := strings.IndexByte(v[1:], quote); end >= 0 {
			return v[1 : 1+end]
		}
		return strings.Trim(v, `"'`) // unterminated quote; salvage what is there
	}
	// Bare value: everything up to the first space, which drops a trailing comment
	// without mistaking the value's own leading '#' for one.
	if i := strings.IndexAny(v, " \t"); i >= 0 {
		v = v[:i]
	}
	return v
}

// titleCase uppercases the first byte of s. Only correct for ASCII strings,
// which is fine for accent IDs like "blue" or "sapphire".
func titleCase(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// IsHexColor returns true for valid CSS hex color strings: #rgb, #rrggbb, #rrggbbaa.
func IsHexColor(s string) bool {
	if s == "" || s[0] != '#' {
		return false
	}
	rest := s[1:]
	switch len(rest) {
	case 3, 6, 8:
	default:
		return false
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
