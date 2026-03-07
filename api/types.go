// Package api provides the public client interface for the z13ctl daemon.
// It contains the shared protocol types and socket client functions used by
// CLI commands, GUI frontends, and any other tool that communicates with the
// z13ctl daemon over its Unix socket.
package api

// State holds the last-applied settings for all controllable subsystems.
// It is returned by SendGetState and broadcast as part of daemon responses.
type State struct {
	Lighting           LightingState            `json:"lighting"`
	Devices            map[string]LightingState `json:"devices,omitempty"` // per-device overrides keyed by name
	Profile            string                   `json:"profile,omitempty"`
	Battery            int                      `json:"battery_limit,omitempty"`
	BootSound          int                      `json:"boot_sound,omitempty"`
	PanelOverdrive     int                      `json:"panel_overdrive,omitempty"`
	FanCurve           *FanCurveState           `json:"fan_curve,omitempty"`
	TDP                *TDPState                `json:"tdp,omitempty"`
	Undervolt          *UndervoltState          `json:"undervolt,omitempty"`
	UndervoltAvailable bool                     `json:"undervolt_available"` // true if ryzen_smu is loaded
	Temperature        int                      `json:"temperature,omitempty"`  // APU temp, degrees Celsius
	FanRPM             int                      `json:"fan_rpm,omitempty"`      // fan1 speed in RPM
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

// FanCurvePoint represents one point on an 8-point fan curve.
type FanCurvePoint struct {
	Temp int `json:"temp"` // degrees Celsius
	PWM  int `json:"pwm"`  // 0–255 duty cycle
}

// FanCurveState captures the fan curve and mode applied to both fans.
type FanCurveState struct {
	Mode   int             `json:"mode"`   // pwm_enable: 0=full-speed, 1=custom, 2=auto
	Points []FanCurvePoint `json:"points"` // 8 points
}

// UndervoltState captures the AMD Curve Optimizer offset applied to the CPU.
// Values are non-positive integers (0 = stock, negative = undervolt).
// Active indicates whether the offset is currently applied to hardware.
type UndervoltState struct {
	CPUCO  int  `json:"cpu_co"` // all-core CPU Curve Optimizer offset
	Active bool `json:"active"` // true when CO is applied to hardware
}

// TDPState captures all PPT (Package Power Tracking) values in watts.
type TDPState struct {
	PL1SPL       int `json:"pl1_spl"`       // Sustained Power Limit
	PL2SPPT      int `json:"pl2_sppt"`      // Short Boost
	FPPT         int `json:"fppt"`          // Fast Boost
	APUSPPT      int `json:"apu_sppt"`      // APU Short PPT
	PlatformSPPT int `json:"platform_sppt"` // Platform Short PPT
}
