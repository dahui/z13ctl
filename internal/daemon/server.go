package daemon

// server.go — incoming socket connection handler and command dispatch.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/cli"
)

// request and response mirror the unexported types in api/client.go.
// They are intentionally duplicated: the api module is a separate Go module
// (stdlib-only), so the daemon cannot share types with it across the module
// boundary without making them public. Both sides must stay in sync with the
// JSON wire protocol.

// request is a command sent by a client over the socket.
// One JSON object per newline-terminated message.
type request struct {
	Cmd        string   `json:"cmd"`
	Mode       string   `json:"mode,omitempty"`
	Color      string   `json:"color,omitempty"`  // "RRGGBB" hex
	Color2     string   `json:"color2,omitempty"` // "RRGGBB" hex
	Speed      string   `json:"speed,omitempty"`
	Brightness int      `json:"brightness,omitempty"`
	Set        string   `json:"set,omitempty"`
	Device     string   `json:"device,omitempty"` // "keyboard", "lightbar", /dev/hidrawN; empty = all
	Events     []string `json:"events,omitempty"`
	PL1        string   `json:"pl1,omitempty"`
	PL2        string   `json:"pl2,omitempty"`
	PL3        string   `json:"pl3,omitempty"`
	Force      bool     `json:"force,omitempty"`
	// Profile names the custom profile a fancurve/tdp/undervolt command edits.
	// Empty means the active profile — which is what every client written before
	// this field existed sends, so their behaviour is unchanged.
	Profile string `json:"profile,omitempty"`
	AC      string `json:"ac,omitempty"`
	Battery string `json:"battery,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

// response is the reply to a command or a streamed event notification.
type response struct {
	OK    bool       `json:"ok"`
	Error string     `json:"error,omitempty"`
	Value string     `json:"value,omitempty"`
	State *api.State `json:"state,omitempty"`
	Event string     `json:"event,omitempty"`
}

// requestReadTimeout bounds how long a connection may stay open without
// sending its request line. Declared as a var so tests can shorten it.
var requestReadTimeout = 30 * time.Second

// handleConn reads one JSON request, dispatches it, and writes one JSON response.
// For "subscribe" requests the connection is kept open for event streaming.
func (d *Daemon) handleConn(conn net.Conn) {
	// Bound how long a connection may sit without sending its request line.
	// handleConn runs in its own goroutine per connection with no cap, so a
	// client that connects and stays silent would otherwise pin a goroutine
	// (and, for a subscribe, a file descriptor) for the daemon's lifetime.
	_ = conn.SetReadDeadline(time.Now().Add(requestReadTimeout))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		_ = conn.Close()
		return
	}

	var req request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		writeResponse(conn, response{OK: false, Error: "invalid JSON: " + err.Error()})
		_ = conn.Close()
		return
	}

	if req.Cmd == "subscribe" {
		// Acknowledge and keep the connection open for event streaming. Clear
		// the deadline first: a subscription is idle by design between button
		// presses, and the daemon only ever writes on it from here on.
		_ = conn.SetReadDeadline(time.Time{})
		writeResponse(conn, response{OK: true})
		d.addSubscriber(conn)
		return
	}

	// The command is handled synchronously below; clear the read deadline so it
	// cannot expire mid-write on a slow hardware operation.
	_ = conn.SetReadDeadline(time.Time{})

	resp := d.dispatch(req)
	if !resp.OK {
		slog.Warn("command failed", "cmd", req.Cmd, "err", resp.Error)
	}
	writeResponse(conn, resp)
	_ = conn.Close()
}

func (d *Daemon) dispatch(req request) response {
	switch req.Cmd {
	case "apply":
		return d.handleApply(req)
	case "off":
		return d.handleOff(req)
	case "brightness":
		return d.handleBrightness(req)
	case "profile":
		return d.handleProfile(req)
	case "profile-get":
		return handleProfileGet()
	case "profile-create":
		return d.handleProfileCreate(req)
	case "profile-save":
		return d.handleProfileSave(req)
	case "profile-delete":
		return d.handleProfileDelete(req)
	case "profile-list":
		return d.handleProfileList()
	case "autoswitch":
		return d.handleAutoswitch(req)
	case "autoswitch-get":
		return d.handleAutoswitchGet()
	case "batterylimit":
		return d.handleBatteryLimit(req)
	case "batterylimit-get":
		return handleBatteryLimitGet()
	case "bootsound":
		return handleBootSound(req)
	case "bootsound-get":
		return handleBootSoundGet()
	case "paneloverdrive":
		return d.handlePanelOverdrive(req)
	case "paneloverdrive-get":
		return handlePanelOverdriveGet()
	case "fancurve":
		return d.handleFanCurve(req)
	case "fancurve-get":
		return handleFanCurveGet()
	case "fancurve-reset":
		return d.handleFanCurveReset(req)
	case "tdp":
		return d.handleTDP(req)
	case "tdp-get":
		return d.handleTDPGet()
	case "tdp-reset":
		return d.handleTDPReset(req)
	case "undervolt":
		return d.handleUndervolt(req)
	case "undervolt-get":
		return d.handleUndervoltGet()
	case "undervolt-reset":
		return d.handleUndervoltReset(req)
	case "get-state":
		d.mu.Lock()
		// The legacy FanCurve/TDP/Undervolt fields are projected here from the
		// active custom profile so clients written before named profiles existed
		// still see the settings that are in force. Two of the three are then
		// overwritten with live sysfs readings below.
		s := withLegacyProjection(cloneState(d.state))
		d.mu.Unlock()
		if onAC, acErr := cli.OnACPower(); acErr == nil {
			s.OnAC = onAC
		}
		// Populate firmware-managed fields from sysfs (not cached in daemon state).
		s.BootSound = readIntSysfs(cli.FindBootSoundPath())
		s.PanelOverdrive = readIntSysfs(cli.FindPanelOverdrivePath())
		// Populate fan curve from sysfs for ground truth.
		s.FanCurve = readFanCurveFromSysfs()
		// Populate TDP, substituting per-profile defaults if sysfs is stale.
		// Pass the daemon's own profile: platform_profile is never "custom", so
		// using it would report the stock table for a legitimate 5W custom TDP.
		if tdp, err := cli.ReadEffectivePPT(d.effectiveProfile()); err == nil {
			s.TDP = &tdp
		}
		// Indicate whether undervolt is available (ryzen_smu loaded + commands work).
		s.UndervoltAvailable = cli.SMUProbeUndervolt()
		// Populate APU temperature and fan RPM from sysfs.
		if temp, err := cli.ReadAPUTemperature(); err == nil {
			s.Temperature = temp
		}
		if rpms, err := cli.ReadBothFanRPM(); err == nil {
			s.FanRPM = rpms[0]
		}
		return response{OK: true, State: &s}
	default:
		return response{OK: false, Error: "unknown command: " + req.Cmd}
	}
}

// handleProfileGet reads the current performance profile from sysfs.
// Reading from sysfs (not daemon state) ensures accurate values even if
// the profile was changed by another process.
func handleProfileGet() response {
	data, err := os.ReadFile(cli.FindProfilePath())
	if err != nil {
		return response{OK: false, Error: "reading profile: " + err.Error()}
	}
	return response{OK: true, Value: strings.TrimSpace(string(data))}
}

// handleBatteryLimitGet reads the current battery charge limit from sysfs.
func handleBatteryLimitGet() response {
	data, err := os.ReadFile(cli.FindBatteryThresholdPath())
	if err != nil {
		return response{OK: false, Error: "reading battery limit: " + err.Error()}
	}
	return response{OK: true, Value: strings.TrimSpace(string(data))}
}

func (d *Daemon) handleApply(req request) response {
	mode, err := aura.ModeFromString(req.Mode)
	if err != nil {
		return response{OK: false, Error: "mode: " + err.Error()}
	}
	speed, err := aura.SpeedFromString(req.Speed)
	if err != nil {
		return response{OK: false, Error: "speed: " + err.Error()}
	}
	r, g, b, err := cli.ParseColor(req.Color)
	if err != nil {
		return response{OK: false, Error: "color: " + err.Error()}
	}
	r2, g2, b2, err := cli.ParseColor(req.Color2)
	if err != nil {
		return response{OK: false, Error: "color2: " + err.Error()}
	}
	if req.Brightness < 0 || req.Brightness > 3 {
		return response{OK: false, Error: fmt.Sprintf("brightness %d out of range 0–3", req.Brightness)}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.dev == nil {
		return response{OK: false, Error: "no HID device available"}
	}
	target, err := d.dev.FilteredView(req.Device)
	if err != nil {
		return response{OK: false, Error: err.Error()}
	}
	if err := aura.Apply(target, mode, r, g, b, r2, g2, b2, speed, uint8(req.Brightness)); err != nil {
		return response{OK: false, Error: "apply: " + err.Error()}
	}
	device := req.Device
	if device == "" {
		device = "all"
	}
	slog.Info("apply", "device", device, "mode", req.Mode, "color", req.Color, "brightness", req.Brightness)
	ls := api.LightingState{
		Enabled:    true,
		Mode:       req.Mode,
		Color:      req.Color,
		Color2:     req.Color2,
		Speed:      req.Speed,
		Brightness: req.Brightness,
	}
	if req.Device == "" {
		// All-device apply: update canonical state and clear per-device overrides.
		d.state.Lighting = ls
		d.state.Devices = nil
	} else if !strings.HasPrefix(req.Device, "/") {
		// Named per-device apply (keyboard/lightbar): save as a per-device override.
		if d.state.Devices == nil {
			d.state.Devices = make(map[string]api.LightingState)
		}
		d.state.Devices[req.Device] = ls
	}
	// Raw /dev/hidrawN paths are transient; not persisted.
	if req.Device == "" || !strings.HasPrefix(req.Device, "/") {
		if err := saveState(cloneState(d.state)); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
	}
	return response{OK: true}
}

func (d *Daemon) handleOff(req request) response {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.dev == nil {
		return response{OK: false, Error: "no HID device available"}
	}
	target, err := d.dev.FilteredView(req.Device)
	if err != nil {
		return response{OK: false, Error: err.Error()}
	}
	if err := aura.TurnOff(target); err != nil {
		return response{OK: false, Error: "off: " + err.Error()}
	}
	if req.Device != "" {
		slog.Info("off", "device", req.Device)
		if !strings.HasPrefix(req.Device, "/") {
			// Named per-device off: save disabled state for this zone.
			if d.state.Devices == nil {
				d.state.Devices = make(map[string]api.LightingState)
			}
			d.state.Devices[req.Device] = api.LightingState{Enabled: false}
			if err := saveState(cloneState(d.state)); err != nil {
				slog.Warn("failed to save state", "err", err)
			}
		}
	} else {
		slog.Info("off")
		d.state.Lighting.Enabled = false
		d.state.Devices = nil
		if err := saveState(cloneState(d.state)); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
	}
	return response{OK: true}
}

func (d *Daemon) handleBrightness(req request) response {
	if req.Brightness < 0 || req.Brightness > 3 {
		return response{OK: false, Error: fmt.Sprintf("brightness %d out of range 0–3", req.Brightness)}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.dev == nil {
		return response{OK: false, Error: "no HID device available"}
	}
	target, err := d.dev.FilteredView(req.Device)
	if err != nil {
		return response{OK: false, Error: err.Error()}
	}
	if err := aura.Init(target); err != nil {
		return response{OK: false, Error: "init: " + err.Error()}
	}
	on := req.Brightness > 0
	if err := aura.SetPower(target, on); err != nil {
		return response{OK: false, Error: "setpower: " + err.Error()}
	}
	if err := aura.SetBrightness(target, uint8(req.Brightness)); err != nil {
		return response{OK: false, Error: "brightness: " + err.Error()}
	}
	logArgs := []any{"level", req.Brightness}
	if req.Device != "" {
		logArgs = append(logArgs, "device", req.Device)
	}
	slog.Info("brightness", logArgs...)
	if req.Device == "" {
		d.state.Lighting.Brightness = req.Brightness
		d.state.Lighting.Enabled = on
		if err := saveState(cloneState(d.state)); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
	} else if !strings.HasPrefix(req.Device, "/") {
		// Named per-device brightness: update or create entry, preserving other fields.
		if d.state.Devices == nil {
			d.state.Devices = make(map[string]api.LightingState)
		}
		ls := d.state.Lighting // base: fall back to all-device state
		if existing, ok := d.state.Devices[req.Device]; ok {
			// Normalise: a zone turned off earlier is stored as
			// {Enabled: false} with no mode or colour, and reusing it verbatim
			// would persist an enabled state that cannot be re-applied.
			ls = normalizeLightingState(existing, d.state.Lighting)
		}
		ls.Brightness = req.Brightness
		ls.Enabled = on
		d.state.Devices[req.Device] = ls
		if err := saveState(cloneState(d.state)); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
	}
	return response{OK: true}
}

// restoreStockPPT writes the measured stock PPT values for a stock profile back
// to hardware. The asus-nb-wmi PPT attributes have no "reset to firmware
// default" operation and the firmware does not re-apply per-profile limits on a
// platform_profile change, so without this a custom TDP leaks into every stock
// profile. Failures are logged and swallowed: a profile switch must not
// hard-fail because the PPT restore did not take.
//
// Callers must not clear the saved custom TDP in daemon state — only the
// hardware values are reset, so the user can select "custom" again.
func restoreStockPPT(profile string) {
	stock, ok := cli.StockProfilePPT[profile]
	if !ok {
		return
	}
	if err := cli.SetTDPState(stock); err != nil {
		slog.Warn("failed to restore stock PPT values", "profile", profile, "err", err)
		return
	}
	slog.Info("restored stock PPT", "profile", profile, "pl1", stock.PL1SPL, "pl2", stock.PL2SPPT)
}

func (d *Daemon) handleBatteryLimit(req request) response {
	limit, err := strconv.Atoi(req.Set)
	if err != nil || limit < 40 || limit > 100 {
		return response{OK: false, Error: "battery limit must be an integer 40–100"}
	}
	if err := os.WriteFile(cli.FindBatteryThresholdPath(), []byte(req.Set+"\n"), 0o644); err != nil {
		return response{OK: false, Error: "batterylimit: " + err.Error()}
	}
	slog.Info("batterylimit", "set", limit)
	d.mu.Lock()
	d.state.Battery = limit
	s := cloneState(d.state)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func handleBootSoundGet() response {
	data, err := os.ReadFile(cli.FindBootSoundPath())
	if err != nil {
		return response{OK: false, Error: "reading boot sound: " + err.Error()}
	}
	return response{OK: true, Value: strings.TrimSpace(string(data))}
}

func handleBootSound(req request) response {
	value, err := strconv.Atoi(req.Set)
	if err != nil || (value != 0 && value != 1) {
		return response{OK: false, Error: "boot sound must be 0 or 1"}
	}
	if err := cli.SetBootSound(value); err != nil {
		return response{OK: false, Error: "bootsound: " + err.Error()}
	}
	slog.Info("bootsound", "set", value)
	return response{OK: true}
}

func handlePanelOverdriveGet() response {
	data, err := os.ReadFile(cli.FindPanelOverdrivePath())
	if err != nil {
		return response{OK: false, Error: "reading panel overdrive: " + err.Error()}
	}
	return response{OK: true, Value: strings.TrimSpace(string(data))}
}

func (d *Daemon) handlePanelOverdrive(req request) response {
	value, err := strconv.Atoi(req.Set)
	if err != nil || (value != 0 && value != 1) {
		return response{OK: false, Error: "panel overdrive must be 0 or 1"}
	}
	if err := cli.SetPanelOverdrive(value); err != nil {
		return response{OK: false, Error: "paneloverdrive: " + err.Error()}
	}
	slog.Info("paneloverdrive", "set", value)
	d.mu.Lock()
	d.state.PanelOverdrive = value
	s := cloneState(d.state)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

// readIntSysfs reads a sysfs file, trims whitespace, and parses it as an int.
// Returns 0 on any error (file missing, unreadable, or non-numeric content).
func readIntSysfs(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return v
}

// handleFanCurveGet reads the current fan curve from sysfs (both fans).
func handleFanCurveGet() response {
	fc := readFanCurveFromSysfs()
	if fc == nil {
		return response{OK: false, Error: "failed to read fan curve from sysfs"}
	}
	data, _ := json.Marshal(fc)
	return response{OK: true, Value: string(data)}
}

// readFanCurveFromSysfs reads the fan curve and mode from sysfs.
// Returns fan 1's curve (both fans share the same curve).
func readFanCurveFromSysfs() *api.FanCurveState {
	modes, modeErr := cli.ReadBothFanModes()
	curves, curveErr := cli.ReadBothFanCurves()
	if modeErr != nil && curveErr != nil {
		return nil
	}
	mode := 0
	if modeErr == nil {
		mode = modes[0]
	}
	var points []api.FanCurvePoint
	if curveErr == nil {
		points = curves[0]
	}
	return &api.FanCurveState{Mode: mode, Points: points}
}

func (d *Daemon) handleFanCurve(req request) response {
	points, err := cli.ParseFanCurve(req.Set)
	if err != nil {
		return response{OK: false, Error: "fancurve: " + err.Error()}
	}
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	target, err := d.resolveEditTargetLocked(req.Profile)
	if err != nil {
		d.mu.Unlock()
		return response{OK: false, Error: "fancurve: " + err.Error()}
	}
	d.mu.Unlock()

	// Enforce the minimum PWM floor when the sustained TDP that will accompany
	// this curve exceeds the safe max. For the live profile that limit comes
	// from hardware, which is ground truth and can disagree with cached state —
	// a TDP set while the daemon was down, or a reset state file. For any other
	// profile it comes from that profile's own saved TDP, so a profile can never
	// be stored in a state that would be unsafe the moment it is activated.
	if err := cli.CheckCurveAgainstTDP(points, d.floorLimitFor(target)); err != nil {
		return response{OK: false, Error: "fancurve: " + err.Error()}
	}

	if target.Live {
		if err := cli.SetBothFanCurves(points); err != nil {
			return response{OK: false, Error: "fancurve: " + err.Error()}
		}
		slog.Info("fancurve", "fans", "both", "profile", target.Name)
	} else {
		slog.Info("fancurve", "profile", target.Name, "applied", false)
	}

	p := target.Profile
	p.FanCurve = &api.FanCurveState{Mode: 1, Points: points}
	d.mu.Lock()
	s := d.commitEditLocked(target, p)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func (d *Daemon) handleFanCurveReset(req request) response {
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	target, err := d.resolveEditTargetLocked(req.Profile)
	if err != nil {
		d.mu.Unlock()
		return response{OK: false, Error: "fancurve-reset: " + err.Error()}
	}
	d.mu.Unlock()

	// Firmware auto has no PWM floor, so releasing the fans while a high
	// sustained TDP is still in force removes the very protection the high-TDP
	// curve provides. "tdp --reset" is the way out — it lowers power first. The
	// same reasoning applies to clearing the curve from a profile that keeps a
	// high TDP: it would be unsafe the moment that profile is activated.
	if err := cli.CheckFanFloorReleaseAt(d.floorLimitFor(target)); err != nil {
		return response{OK: false, Error: "fancurve-reset: " + err.Error()}
	}

	if target.Live {
		if err := cli.ResetAllFanCurves(); err != nil {
			return response{OK: false, Error: "fancurve-reset: " + err.Error()}
		}
		slog.Info("fancurve-reset", "fans", "both", "profile", target.Name)
	} else {
		slog.Info("fancurve-reset", "profile", target.Name, "applied", false)
	}

	p := target.Profile
	p.FanCurve = nil
	d.mu.Lock()
	s := d.commitEditLocked(target, p)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func (d *Daemon) handleTDPGet() response {
	tdp, err := cli.ReadEffectivePPT(d.effectiveProfile())
	if err != nil {
		return response{OK: false, Error: "reading TDP: " + err.Error()}
	}
	data, _ := json.Marshal(tdp)
	return response{OK: true, Value: string(data)}
}

// effectiveProfile returns the profile to use when interpreting PPT values:
// the daemon's own state when set, falling back to platform_profile. The
// distinction matters because "custom" is a virtual profile that is never
// written to platform_profile, so sysfs alone cannot tell a legitimate 5W
// custom TDP from the kernel's stale 5W cache.
func (d *Daemon) effectiveProfile() string {
	d.mu.Lock()
	p := d.state.Profile
	d.mu.Unlock()
	if p != "" {
		return p
	}
	return readProfileFromSysfs()
}

// readProfileFromSysfs reads the current platform_profile value.
func readProfileFromSysfs() string {
	data, err := os.ReadFile(cli.FindProfilePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (d *Daemon) handleTDP(req request) response {
	watts, err := strconv.Atoi(req.Set)
	if err != nil {
		return response{OK: false, Error: "TDP value must be an integer"}
	}

	pl1, pl2, pl3 := watts, watts, watts
	if req.PL1 != "" {
		if pl1, err = strconv.Atoi(req.PL1); err != nil {
			return response{OK: false, Error: "invalid pl1 value"}
		}
	}
	if req.PL2 != "" {
		if pl2, err = strconv.Atoi(req.PL2); err != nil {
			return response{OK: false, Error: "invalid pl2 value"}
		}
	}
	if req.PL3 != "" {
		if pl3, err = strconv.Atoi(req.PL3); err != nil {
			return response{OK: false, Error: "invalid pl3 value"}
		}
	}

	// PL1 (sustained) requires force flag above 75W. PL2/PL3 (burst) allowed up to hardware max.
	pl1Max := cli.TDPMaxSafe
	if req.Force {
		pl1Max = cli.TDPMaxForced
	}
	if pl1 < cli.TDPMin || pl1 > pl1Max {
		if pl1 > cli.TDPMaxSafe && !req.Force {
			return response{OK: false, Error: fmt.Sprintf("PL1 %dW exceeds safe sustained max (%dW); use force flag", pl1, cli.TDPMaxSafe)}
		}
		return response{OK: false, Error: fmt.Sprintf("PL1 %dW out of range %d–%d", pl1, cli.TDPMin, pl1Max)}
	}
	for _, v := range []int{pl2, pl3} {
		if v < cli.TDPMin || v > cli.TDPMaxForced {
			return response{OK: false, Error: fmt.Sprintf("TDP %dW out of range %d–%d", v, cli.TDPMin, cli.TDPMaxForced)}
		}
	}

	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	target, err := d.resolveEditTargetLocked(req.Profile)
	if err != nil {
		d.mu.Unlock()
		return response{OK: false, Error: "tdp: " + err.Error()}
	}
	d.mu.Unlock()

	tdp := cli.TDPStateFor(watts, pl1, pl2, pl3)

	// A profile that is not running must still be storable only in a state that
	// is safe to activate: a high sustained limit alongside a curve that dips
	// below the floor would be refused at activation, so refuse it here where
	// the user can see why. The live profile needs no equivalent check —
	// ApplyTDPSafely raises the floor itself.
	if !target.Live && target.Profile.FanCurve != nil {
		if err := cli.CheckCurveAgainstTDP(target.Profile.FanCurve.Points, pl1); err != nil {
			return response{OK: false, Error: "tdp: " + err.Error() + " (stored in profile " + target.Name + ")"}
		}
	}

	fc := target.Profile.FanCurve
	if target.Live {
		// ApplyTDPSafely raises the fans to the 50% floor first when pl1 exceeds
		// the safe sustained max, and refuses to apply the TDP at all if that
		// fails.
		if err := cli.ApplyTDPSafely(tdp); err != nil {
			return response{OK: false, Error: "tdp: " + err.Error()}
		}
		if pl1 > cli.TDPMaxSafe {
			slog.Warn("fans set to 50%+ curve for high TDP", "pl1", pl1)
		}
		slog.Info("tdp", "pl1", pl1, "pl2", pl2, "pl3", pl3, "profile", target.Name)
	} else {
		slog.Info("tdp", "pl1", pl1, "pl2", pl2, "pl3", pl3, "profile", target.Name, "applied", false)
	}

	p := target.Profile
	p.TDP = &tdp
	d.mu.Lock()
	s := d.commitEditLocked(target, p)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}

	// With the limit safe again, put the fans back to what the profile actually
	// describes: its own curve, or firmware auto when it has none. Releasing
	// them in the second case is the part that used to be missing — the
	// high-TDP floor stayed applied to a profile holding no curve, so the same
	// profile gave a different machine depending on whether it was reached by
	// lowering the TDP or by selecting it, which applyCustomHW does not do.
	if target.Live && pl1 <= cli.TDPMaxSafe {
		if fc != nil && fc.Mode == 1 && len(fc.Points) == 8 {
			if err := cli.SetBothFanCurves(fc.Points); err != nil {
				slog.Warn("failed to restore fan curve after TDP change", "err", err)
			} else {
				slog.Info("fan curve restored after TDP reduced to safe levels")
			}
		} else if err := cli.ResetAllFanCurves(); err != nil {
			slog.Warn("failed to release fans after TDP reduced to safe levels", "err", err)
		}
	}

	return response{OK: true}
}

// handleTDPReset lands on the balanced firmware profile when it targets the
// live profile, and merely clears the stored limits when it targets another.
func (d *Daemon) handleTDPReset(req request) response {
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	target, err := d.resolveEditTargetLocked(req.Profile)
	if err != nil {
		d.mu.Unlock()
		return response{OK: false, Error: "tdp-reset: " + err.Error()}
	}
	d.mu.Unlock()

	if !target.Live {
		p := target.Profile
		p.TDP = nil
		d.mu.Lock()
		s := d.commitEditLocked(target, p)
		d.mu.Unlock()
		if err := saveState(s); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
		slog.Info("tdp-reset", "profile", target.Name, "applied", false)
		return response{OK: true}
	}

	// Lower power first, then release the fans: balanced's sustained limit is
	// below TDPMaxSafe, so by the time the fans drop to firmware auto the limit
	// that required the 50% floor is gone. Doing it the other way round leaves a
	// window at full power with no floor, and a failed profile switch would
	// leave it that way. The firmware manages fan curves on a profile change but
	// does not restore PPT, so restoreStockPPT has to be explicit.
	// Reset the undervolt too. This lands on "balanced", a stock profile, and
	// every other route to a stock profile clears CO — leaving it applied here
	// would leak a custom setting into a stock profile (the defect class behind
	// #12) and leave undervolt --get reporting "active" on a stock profile.
	// Saved values are kept in state so "custom" stays re-selectable.
	if cli.SMUProbeUndervolt() {
		if err := cli.ResetCurveOptimizer(); err != nil {
			slog.Warn("failed to reset undervolt after TDP reset", "err", err)
		}
	}
	if err := cli.SetProfile("balanced"); err != nil {
		return response{OK: false, Error: "tdp-reset: switching to balanced profile: " + err.Error()}
	}
	restoreStockPPT("balanced")
	if err := cli.ResetAllFanCurves(); err != nil {
		slog.Warn("failed to reset fan curves after TDP reset", "err", err)
	}
	slog.Info("tdp-reset", "profile", "balanced")
	d.mu.Lock()
	// Clear the limits and curve from the profile that was running, not from a
	// global slot: switching back to it later must not resurrect the TDP this
	// command just reset in hardware.
	p := target.Profile
	p.TDP = nil
	p.FanCurve = nil
	if d.state.CustomProfiles == nil {
		d.state.CustomProfiles = make(map[string]api.CustomProfile, 1)
	}
	d.state.CustomProfiles[target.Name] = p
	d.state.Profile = "balanced"
	setUndervoltActive(d.state, false)
	s := cloneState(d.state)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func (d *Daemon) handleUndervoltGet() response {
	if !cli.SMUProbeUndervolt() {
		return response{OK: false, Error: "Curve Optimizer not available — ryzen_smu module missing or does not support this platform"}
	}
	d.mu.Lock()
	profile := d.state.Profile
	// Report the offset that "undervolt --set" with no profile would replace:
	// the active custom profile's, or the default one's when a firmware profile
	// is active. Values survive a switch to a stock profile so they can be
	// recalled; Active is what says whether they are in hardware right now.
	target, _ := d.resolveEditTargetLocked("")
	uv := target.Profile.Undervolt
	d.mu.Unlock()

	uvState := api.UndervoltState{}
	if uv != nil {
		uvState = *uv
	}
	// Include the current profile so the client can tell whether CO is active.
	data, _ := json.Marshal(struct {
		api.UndervoltState
		Profile string `json:"profile"`
	}{uvState, profile})
	return response{OK: true, Value: string(data)}
}

func (d *Daemon) handleUndervolt(req request) response {
	if !cli.SMUProbeUndervolt() {
		return response{OK: false, Error: "Curve Optimizer not available — ryzen_smu module missing or does not support this platform"}
	}

	cpuOffset := 0
	if req.Set != "" {
		v, err := strconv.Atoi(req.Set)
		if err != nil {
			return response{OK: false, Error: "invalid CPU undervolt value: must be an integer"}
		}
		cpuOffset = v
	}

	if err := cli.ValidateCOValues(cpuOffset); err != nil {
		return response{OK: false, Error: err.Error()}
	}

	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	target, err := d.resolveEditTargetLocked(req.Profile)
	if err != nil {
		d.mu.Unlock()
		return response{OK: false, Error: "undervolt: " + err.Error()}
	}
	d.mu.Unlock()

	active := false
	if target.Live {
		if err := cli.SetCurveOptimizer(cpuOffset); err != nil {
			return response{OK: false, Error: "undervolt: " + err.Error()}
		}
		active = true
		slog.Info("undervolt", "cpu", cpuOffset, "profile", target.Name)
	} else {
		slog.Info("undervolt", "cpu", cpuOffset, "profile", target.Name, "applied", false)
	}

	p := target.Profile
	p.Undervolt = &api.UndervoltState{CPUCO: cpuOffset, Active: active}
	d.mu.Lock()
	s := d.commitEditLocked(target, p)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func (d *Daemon) handleUndervoltReset(req request) response {
	if !cli.SMUProbeUndervolt() {
		return response{OK: false, Error: "Curve Optimizer not available — ryzen_smu module missing or does not support this platform"}
	}

	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	target, err := d.resolveEditTargetLocked(req.Profile)
	if err != nil {
		d.mu.Unlock()
		return response{OK: false, Error: "undervolt-reset: " + err.Error()}
	}
	d.mu.Unlock()

	if target.Live {
		if err := cli.ResetCurveOptimizer(); err != nil {
			return response{OK: false, Error: "undervolt-reset: " + err.Error()}
		}
		slog.Info("undervolt-reset", "profile", target.Name)
	} else {
		slog.Info("undervolt-reset", "profile", target.Name, "applied", false)
	}

	p := target.Profile
	p.Undervolt = nil
	d.mu.Lock()
	s := d.commitEditLocked(target, p)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func writeResponse(conn net.Conn, r response) {
	data, _ := json.Marshal(r)
	_, _ = fmt.Fprintf(conn, "%s\n", data)
}
