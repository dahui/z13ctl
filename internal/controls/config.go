// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package controls

// config.go — ~/.config/voltaire/gui.toml, and resolving it into the list the
// drawer builds from.
//
// Parsing and resolution are separate functions on purpose: Resolve is pure and
// takes a Config, so every rule below is tested against a literal rather than
// against a file, and Load is the thin part that can touch a disk.

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/dahui/voltaire/api/v2"
)

// ConfigFile is the config's name within the voltaire config directory.
const ConfigFile = "gui.toml"

// Config is the user's gui.toml.
//
// Quickbar.Controls is a *pointer* to a slice so that "absent" and "empty" are
// different answers. An absent list means "you choose" and gets the default
// order; an empty list means "show nothing", which is a strange thing to want
// but is unambiguously what `controls = []` says, and silently overriding it
// with the defaults would leave the user editing a file that does nothing.
type Config struct {
	Quickbar QuickbarConfig `toml:"quickbar"`
}

// QuickbarConfig is the [quickbar] table.
//
// Edge is a plain string here rather than a panelgeom.Edge because this package
// is about *which* controls exist, not where the panel sits; panelgeom.ParseEdge
// owns the spellings and the refusals, and the caller pairs the two. Keeping
// both settings in one file and one struct is what stops a second config file
// appearing the first time the drawer grows another preference.
type QuickbarConfig struct {
	Controls *[]string `toml:"controls"`
	Edge     string    `toml:"edge"`
}

// FormatList encodes a saved controls list for config.toml, the file the UI
// writes: nil is "" (no choice made), an empty list is "none" (the user hid
// everything — a choice, and a different one), anything else the IDs joined
// by commas. No control ID is "none" or contains a comma.
func FormatList(l *[]string) string {
	switch {
	case l == nil:
		return ""
	case len(*l) == 0:
		return "none"
	}
	return strings.Join(*l, ",")
}

// ParseList is FormatList's inverse.
func ParseList(s string) *[]string {
	s = strings.TrimSpace(s)
	switch s {
	case "":
		return nil
	case "none":
		return &[]string{}
	}
	var out []string
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return &out
}

// Effective picks the list the drawer builds from and says where it came
// from. A controls list in gui.toml, which a person wrote by hand, wins over
// the one the Settings editor saved to config.toml; locked reports that, so
// the editor can show the layout without offering to overwrite it.
func Effective(guiFile Config, saved string) (cfg Config, locked bool) {
	cfg = guiFile
	if cfg.Quickbar.Controls != nil {
		return cfg, true
	}
	cfg.Quickbar.Controls = ParseList(saved)
	return cfg, false
}

// Load reads gui.toml from dir. A missing file is not an error — it is the
// overwhelmingly common case, and it means the defaults.
//
// A malformed file *is* an error, and the caller is expected to log it and
// carry on with the defaults rather than refuse to start: the drawer is the
// only way some users reach these settings, so a typo in an optional file must
// never be the thing that stops it opening.
func Load(dir string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(dir + "/" + ConfigFile)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", ConfigFile, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", ConfigFile, err)
	}
	return cfg, nil
}

// Resolve returns the controls the drawer should build, in order, together with
// any complaints about the config worth logging.
//
// session lists the capabilities only the client can know (see
// CapRefreshRate).
//
// The rules, in the order they apply:
//
//   - No controls list: the default controls (Control.Default), in order.
//   - A controls list: exactly those, in that order. An ID that is not a known
//     control is dropped with a complaint naming it and listing what is valid —
//     a silent drop turns a typo into a missing section with no explanation.
//   - A repeated ID is kept once, at its first position. Building the same
//     section twice would produce two live widget trees bound to one piece of
//     state.
//   - Anything the device cannot do is dropped last, whether it was named or
//     defaulted. This is not a complaint: an absent capability is the document
//     working as intended, and a user who moves their config to a second
//     machine should not be told off for it.
//
// Complaints are returned rather than logged so that the pure function stays
// pure and the caller decides where they go.
func Resolve(cfg Config, info *api.DeviceInfo, session ...Capability) (resolved []Control, complaints []string) {
	wanted := Defaults()
	if cfg.Quickbar.Controls != nil {
		wanted = nil
		seen := make(map[string]bool)
		for _, id := range *cfg.Quickbar.Controls {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			c, ok := Lookup(id)
			if !ok {
				complaints = append(complaints, fmt.Sprintf(
					"%s: unknown control %q (known: %s)", ConfigFile, id, strings.Join(IDs(), ", ")))
				continue
			}
			if seen[id] {
				complaints = append(complaints, fmt.Sprintf(
					"%s: control %q listed more than once; keeping the first", ConfigFile, id))
				continue
			}
			seen[id] = true
			wanted = append(wanted, c)
		}
	}

	for _, c := range wanted {
		if Supports(info, c, session...) {
			resolved = append(resolved, c)
		}
	}
	return resolved, complaints
}
