package api

// protocol.go — the wire protocol's messages and command names, shared by the
// client functions here and the daemon (which aliases Request and Response),
// so there is one definition of what crosses the socket.

// Command names: the "cmd" field of a Request. The daemon's command table
// (internal/daemon) is keyed by these, and website/docs/reference/daemon.md
// documents each; a test holds the three together.
const (
	CmdApply             = "apply"
	CmdOff               = "off"
	CmdBrightness        = "brightness"
	CmdProfile           = "profile"
	CmdProfileGet        = "profile-get"
	CmdProfileCreate     = "profile-create"
	CmdProfileSave       = "profile-save"
	CmdProfileDelete     = "profile-delete"
	CmdProfileList       = "profile-list"
	CmdAutoswitch        = "autoswitch"
	CmdAutoswitchGet     = "autoswitch-get"
	CmdBatteryLimit      = "batterylimit"
	CmdBatteryLimitGet   = "batterylimit-get"
	CmdDeviceGet         = "device-get"
	CmdTelemetryHistory  = "telemetry-history"
	CmdFeature           = "feature"
	CmdFeatureGet        = "feature-get"
	CmdBootSound         = "bootsound"
	CmdBootSoundGet      = "bootsound-get"
	CmdButtonSet         = "button-set"
	CmdCPUBoost          = "cpuboost"
	CmdCPUBoostGet       = "cpuboost-get"
	CmdRumble            = "rumble"
	CmdRumbleGet         = "rumble-get"
	CmdPanelOverdrive    = "paneloverdrive"
	CmdPanelOverdriveGet = "paneloverdrive-get"
	CmdFanCurve          = "fancurve"
	CmdFanCurveGet       = "fancurve-get"
	CmdFanCurveReset     = "fancurve-reset"
	CmdTDP               = "tdp"
	CmdTDPGet            = "tdp-get"
	CmdTDPReset          = "tdp-reset"
	CmdTuningReset       = "tuning-reset"
	CmdUndervolt         = "undervolt"
	CmdUndervoltGet      = "undervolt-get"
	CmdUndervoltReset    = "undervolt-reset"
	CmdGetState          = "get-state"
	CmdSubscribe         = "subscribe"
)

// Request is a command sent to the daemon: one JSON object per
// newline-terminated message.
type Request struct {
	Cmd        string   `json:"cmd"`
	Mode       string   `json:"mode,omitempty"`
	Color      string   `json:"color,omitempty"`  // "RRGGBB" hex
	Color2     string   `json:"color2,omitempty"` // "RRGGBB" hex
	Speed      string   `json:"speed,omitempty"`
	Brightness int      `json:"brightness,omitempty"`
	Set        string   `json:"set,omitempty"`
	Device     string   `json:"device,omitempty"` // a lighting zone or /dev/hidrawN; empty = all
	Events     []string `json:"events,omitempty"`
	PL1        string   `json:"pl1,omitempty"`
	PL2        string   `json:"pl2,omitempty"`
	PL3        string   `json:"pl3,omitempty"`
	Force      bool     `json:"force,omitempty"`
	// Profile names the custom profile a fancurve/tdp/undervolt command edits.
	// Empty means the active profile, which is also what every client written
	// before this field existed sends.
	Profile string `json:"profile,omitempty"`
	AC      string `json:"ac,omitempty"`
	Battery string `json:"battery,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	// ID names the firmware toggle a feature/feature-get command addresses,
	// e.g. "boot_sound". The valid set is DeviceInfo.Toggles.
	ID string `json:"id,omitempty"`
	// Seconds is telemetry-history's window. Absent means the whole retained
	// history, whose length DeviceInfo.Telemetry.HistorySeconds gives.
	Seconds int `json:"seconds,omitempty"`
}

// Response is the reply to a command, or a streamed event notification.
// Every response carries ok, events included.
type Response struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error,omitempty"`
	Value  string      `json:"value,omitempty"`
	State  *State      `json:"state,omitempty"`
	Device *DeviceInfo `json:"device,omitempty"`
	Event  string      `json:"event,omitempty"`
	// History is telemetry-history's payload: a typed field rather than JSON
	// stuffed into Value, because double-encoding hurts most at this size (a
	// full window is thousands of samples, every quote escaped).
	History []TelemetrySample `json:"history,omitempty"`
}

// UndervoltReading is undervolt-get's value: the stored offset, and the
// profile running, so a client can tell whether the offset is in force.
type UndervoltReading struct {
	UndervoltState
	Profile string `json:"profile"`
}

// ProfileListEntry is one entry of profile-list: a custom profile and whether
// it is the one running.
type ProfileListEntry struct {
	CustomProfile
	Active bool `json:"active"`
}

// AutoswitchReading is autoswitch-get's value: the configuration and the
// power source now, so a client can say which side is in force without a
// second round trip. SourceKnown false means claim nothing about the source.
type AutoswitchReading struct {
	AutoswitchState
	OnAC        bool `json:"on_ac"`
	SourceKnown bool `json:"source_known"`
}

// ECWedgedMessage is the daemon's refusal of a command that would reach the
// embedded controller while it is not answering after a resume. A client
// matches it with errors.Is(err, ErrECWedged).
const ECWedgedMessage = "the embedded controller has not answered since resume; " +
	"refusing to touch it (this clears on its own once it responds)"
