// Package api provides the public client interface for the z13ctl daemon.
// It contains the shared protocol types and socket client functions used by
// CLI commands, GUI frontends, and any other tool that communicates with the
// z13ctl daemon over its Unix socket.
package api

// State holds the last-applied settings for all controllable subsystems.
// It is returned by SendGetState and broadcast as part of daemon responses.
type State struct {
	Lighting LightingState            `json:"lighting"`
	Devices  map[string]LightingState `json:"devices,omitempty"` // per-device overrides keyed by name
	Profile  string                   `json:"profile,omitempty"`
	Battery  int                      `json:"battery_limit,omitempty"`
}

// LightingState captures all parameters needed to reproduce one lighting zone.
type LightingState struct {
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode"`
	Color      string `json:"color"`  // "RRGGBB" hex
	Color2     string `json:"color2"` // "RRGGBB" hex
	Speed      string `json:"speed"`
	Brightness int    `json:"brightness"` // 0–3
}
