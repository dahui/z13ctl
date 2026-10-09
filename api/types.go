// Package api provides the public client interface for the z13ctl daemon.
// It contains the shared protocol types and socket client functions used by
// CLI commands, GUI frontends, and any other tool that communicates with the
// z13ctl daemon over its Unix socket.
package api

// State holds the last-applied settings for all controllable subsystems.
// It is returned by SendGetState and broadcast as part of daemon responses.
//
// CustomProfiles is the source of truth for custom settings. FanCurve, TDP and
// Undervolt are a projection, retained so clients written against earlier
// versions keep working: they carry the active custom profile's settings, or
// the default "custom" profile's when a firmware profile is active — the same
// values selecting "custom" would recall. Undervolt.Active is what says whether
// an offset is applied to hardware right now; the values themselves survive a
// switch to a firmware profile so they can be recalled.
type State struct {
	Lighting           LightingState            `json:"lighting"`
	Devices            map[string]LightingState `json:"devices,omitempty"` // per-device overrides keyed by name
	Profile            string                   `json:"profile,omitempty"`
	Battery            int                      `json:"battery_limit,omitempty"`
	BootSound          int                      `json:"boot_sound,omitempty"`
	PanelOverdrive     int                      `json:"panel_overdrive,omitempty"`
	CustomProfiles     map[string]CustomProfile `json:"custom_profiles,omitempty"` // saved custom profiles keyed by name
	Autoswitch         *AutoswitchState         `json:"autoswitch,omitempty"`
	FanCurve           *FanCurveState           `json:"fan_curve,omitempty"`   // projection; see the type doc
	TDP                *TDPState                `json:"tdp,omitempty"`         // projection; see the type doc
	TDPLimits          *TDPLimits               `json:"tdp_limits,omitempty"`  // get-state only; what the kernel accepts
	Undervolt          *UndervoltState          `json:"undervolt,omitempty"`   // projection; see the type doc
	UndervoltAvailable bool                     `json:"undervolt_available"`   // true if ryzen_smu is loaded
	OnAC               bool                     `json:"on_ac"`                 // true when running on mains power
	Temperature        int                      `json:"temperature,omitempty"` // APU temp, degrees Celsius
	FanRPM             int                      `json:"fan_rpm,omitempty"`     // fan1 speed in RPM
}

// StockProfiles are the firmware performance profiles that can be written to
// platform_profile. They are reserved: a custom profile can never take one of
// these names, so selecting one always reaches the firmware profile.
var StockProfiles = []string{"quiet", "balanced", "performance"}

// DefaultCustomProfile is the name of the custom profile created implicitly by
// the first fan curve, TDP, or undervolt setting made while a stock profile is
// active. It is reserved and cannot be chosen as a user-supplied name.
const DefaultCustomProfile = "custom"

// IsStockProfileName reports whether name is one of the reserved firmware
// profile names.
func IsStockProfileName(name string) bool {
	for _, p := range StockProfiles {
		if name == p {
			return true
		}
	}
	return false
}

// CustomProfile is a named set of custom hardware settings. Each subsystem is a
// pointer so that nil means "this profile does not control that subsystem",
// which is what lets a profile stay loadable as new subsystems are added.
type CustomProfile struct {
	Name      string          `json:"name"`
	FanCurve  *FanCurveState  `json:"fan_curve,omitempty"`
	TDP       *TDPState       `json:"tdp,omitempty"`
	Undervolt *UndervoltState `json:"undervolt,omitempty"`
}

// Empty reports whether the profile controls no subsystem at all. An empty
// profile cannot be activated: there would be nothing to apply.
func (p CustomProfile) Empty() bool {
	return p.FanCurve == nil && p.TDP == nil && p.Undervolt == nil
}

// AutoswitchState configures automatic profile selection by power source.
// An empty AC or Battery target means "leave the profile alone on that source",
// which is how a caller hands one side back to power-profiles-daemon.
type AutoswitchState struct {
	Enabled bool   `json:"enabled"`
	AC      string `json:"ac,omitempty"`
	Battery string `json:"battery,omitempty"`
}

// Target returns the profile to apply for the given power source, or "" when
// autoswitch is disabled or that side is unconfigured.
func (a *AutoswitchState) Target(onAC bool) string {
	if a == nil || !a.Enabled {
		return ""
	}
	if onAC {
		return a.AC
	}
	return a.Battery
}

// IsCustomProfile reports whether name identifies a z13ctl-managed custom
// profile: the default "custom" profile, or a saved named one.
//
// Clients that check Profile == "custom" to decide whether custom controls
// apply must move to this — a named profile would otherwise read as stock.
//
// A reserved firmware profile name is never custom, whatever the map contains.
// The check is deliberately ahead of the lookup so that a hand-edited state file
// cannot make a stock profile look custom to the fan curve reconciler.
func (s State) IsCustomProfile(name string) bool {
	if name == "" || IsStockProfileName(name) {
		return false
	}
	if name == DefaultCustomProfile {
		return true
	}
	_, ok := s.CustomProfiles[name]
	return ok
}

// InCustomProfile reports whether the active profile is a custom one.
func (s State) InCustomProfile() bool { return s.IsCustomProfile(s.Profile) }

// ActiveCustomProfile returns the active custom profile and true, or the zero
// value and false when a stock profile is active.
func (s State) ActiveCustomProfile() (CustomProfile, bool) {
	if !s.InCustomProfile() {
		return CustomProfile{}, false
	}
	p, ok := s.CustomProfiles[s.Profile]
	if !ok {
		// "custom" is addressable before it has ever been populated.
		return CustomProfile{Name: s.Profile}, true
	}
	return p, true
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
//
// APUSPPT and PlatformSPPT read as 0 when the kernel interface in use does not
// expose them: asus-armoury has neither on the GZ302EA. When setting a TDP they
// follow PL2.
type TDPState struct {
	PL1SPL       int `json:"pl1_spl"`       // Sustained Power Limit
	PL2SPPT      int `json:"pl2_sppt"`      // Short Boost
	FPPT         int `json:"fppt"`          // Fast Boost
	APUSPPT      int `json:"apu_sppt"`      // APU Short PPT
	PlatformSPPT int `json:"platform_sppt"` // Platform Short PPT
}

// TDPLimits describes the power limits the running kernel accepts, so a client
// can bound its controls instead of discovering the range from an error. It is
// reported by get-state and never stored.
//
// The ranges come from the kernel at the time of the request: asus-armoury
// reports per-limit bounds (on the GZ302EA, PL1 28–80, PL2 32–92, PL3 45–93 W)
// and may report different ones on battery; the deprecated asus-nb-wmi
// interface has none of its own, so the daemon reports 5–93 W for it. A PL2 or
// PL3 below its minimum is raised to it when set; a PL1 outside its range is
// refused.
type TDPLimits struct {
	Backend string   `json:"backend"`  // "asus-armoury" or "asus-nb-wmi"
	PL1     TDPRange `json:"pl1"`      // sustained
	PL2     TDPRange `json:"pl2"`      // short boost
	PL3     TDPRange `json:"pl3"`      // fast boost
	SafeMax int      `json:"safe_max"` // highest PL1 accepted without force; above it the fan floor applies
}

// TDPRange is an inclusive range in watts.
type TDPRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}
