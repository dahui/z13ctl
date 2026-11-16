// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package devicefixture holds a synthetic second device for tests: its device
// file and the capability document the daemon produces from it. It exists so
// the rules that decide what a client shows can be tested against a machine
// that is not the Z13 — one power limit, no fans, no undervolt, no lighting,
// no hardware button, no firmware toggles — before a second real device file
// exists to catch the assumptions a single supported device lets slip in.
//
// The two halves are kept honest by internal/daemon's
// TestSyntheticFixtureIsADocumentTheDaemonProduces, which assembles the device
// file with the real drivers and compares the result with synthetic.json; run
// it with VOLTAIRE_UPDATE_FIXTURE=1 to rewrite the JSON after a deliberate
// change. Imported only by tests.
package devicefixture

import (
	_ "embed"
	"encoding/json"

	"github.com/dahui/voltaire/api/v2"
)

//go:embed synthetic.toml
var syntheticTOML []byte

//go:embed synthetic.json
var syntheticJSON []byte

// SyntheticConfig is the synthetic device's device file.
func SyntheticConfig() []byte { return append([]byte(nil), syntheticTOML...) }

// Synthetic is the capability document the daemon serves for the synthetic
// device. Each call returns a fresh copy.
func Synthetic() *api.DeviceInfo {
	var info api.DeviceInfo
	if err := json.Unmarshal(syntheticJSON, &info); err != nil {
		panic("devicefixture: synthetic.json: " + err.Error())
	}
	return &info
}
