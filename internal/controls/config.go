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
type QuickbarConfig struct {
	Controls *[]string `toml:"controls"`
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
// The rules, in the order they apply:
//
//   - No controls list: every known control in default order.
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
func Resolve(cfg Config, info *api.DeviceInfo) (resolved []Control, complaints []string) {
	wanted := All()
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
		if Supports(info, c) {
			resolved = append(resolved, c)
		}
	}
	return resolved, complaints
}
