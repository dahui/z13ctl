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
	Color      string   `json:"color,omitempty"`   // "RRGGBB" hex
	Color2     string   `json:"color2,omitempty"`  // "RRGGBB" hex
	Speed      string   `json:"speed,omitempty"`
	Brightness int      `json:"brightness,omitempty"`
	Set        string   `json:"set,omitempty"`
	Device     string   `json:"device,omitempty"`  // "keyboard", "lightbar", /dev/hidrawN; empty = all
	Events     []string `json:"events,omitempty"`
	PL1        string   `json:"pl1,omitempty"`
	PL2        string   `json:"pl2,omitempty"`
	PL3        string   `json:"pl3,omitempty"`
	Force      bool     `json:"force,omitempty"`
	IGPU       string   `json:"igpu,omitempty"`
}

// response is the reply to a command or a streamed event notification.
type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Value string `json:"value,omitempty"`
	State *api.State `json:"state,omitempty"`
	Event string `json:"event,omitempty"`
}

// handleConn reads one JSON request, dispatches it, and writes one JSON response.
// For "subscribe" requests the connection is kept open for event streaming.
func (d *Daemon) handleConn(conn net.Conn) {
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
		// Acknowledge and keep connection open for event streaming.
		writeResponse(conn, response{OK: true})
		d.addSubscriber(conn)
		return
	}

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
	case "batterylimit":
		return d.handleBatteryLimit(req)
	case "batterylimit-get":
		return handleBatteryLimitGet()
	case "bootsound":
		return handleBootSound(req)
	case "bootsound-get":
		return handleBootSoundGet()
	case "paneloverdrive":
		return handlePanelOverdrive(req)
	case "paneloverdrive-get":
		return handlePanelOverdriveGet()
	case "fancurve":
		return d.handleFanCurve(req)
	case "fancurve-get":
		return handleFanCurveGet()
	case "fancurve-reset":
		return d.handleFanCurveReset()
	case "tdp":
		return d.handleTDP(req)
	case "tdp-get":
		return handleTDPGet()
	case "tdp-reset":
		return d.handleTDPReset()
	case "undervolt":
		return d.handleUndervolt(req)
	case "undervolt-get":
		return d.handleUndervoltGet()
	case "undervolt-reset":
		return d.handleUndervoltReset()
	case "get-state":
		d.mu.Lock()
		s := d.state
		d.mu.Unlock()
		// Populate firmware-managed fields from sysfs (not cached in daemon state).
		s.BootSound = readIntSysfs(cli.FindBootSoundPath())
		s.PanelOverdrive = readIntSysfs(cli.FindPanelOverdrivePath())
		// Populate fan curve from sysfs for ground truth.
		s.FanCurve = readFanCurveFromSysfs()
		// Populate TDP, substituting per-profile defaults if sysfs is stale.
		if tdp, err := cli.ReadEffectivePPT(readProfileFromSysfs()); err == nil {
			s.TDP = &tdp
		}
		// Indicate whether undervolt is available (ryzen_smu loaded).
		s.UndervoltAvailable = cli.SMUAvailable()
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
		if err := saveState(d.state); err != nil {
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
			if err := saveState(d.state); err != nil {
				slog.Warn("failed to save state", "err", err)
			}
		}
	} else {
		slog.Info("off")
		d.state.Lighting.Enabled = false
		d.state.Devices = nil
		if err := saveState(d.state); err != nil {
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
		if err := saveState(d.state); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
	} else if !strings.HasPrefix(req.Device, "/") {
		// Named per-device brightness: update or create entry, preserving other fields.
		if d.state.Devices == nil {
			d.state.Devices = make(map[string]api.LightingState)
		}
		ls := d.state.Lighting // base: fall back to all-device state
		if existing, ok := d.state.Devices[req.Device]; ok {
			ls = existing
		}
		ls.Brightness = req.Brightness
		ls.Enabled = on
		d.state.Devices[req.Device] = ls
		if err := saveState(d.state); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
	}
	return response{OK: true}
}

func (d *Daemon) handleProfile(req request) response {
	if req.Set == "" {
		return response{OK: false, Error: "profile requires a set field"}
	}

	profile := strings.ToLower(req.Set)

	// "custom" is a virtual profile: re-apply saved fan curve + TDP + UV.
	if profile == "custom" {
		d.mu.Lock()
		if d.state.FanCurve == nil && d.state.TDP == nil && d.state.Undervolt == nil {
			d.mu.Unlock()
			return response{OK: false, Error: "no custom settings saved; set fan curve, TDP, or undervolt first"}
		}
		d.state.Profile = "custom"
		// Re-apply saved fan curve to both fans.
		if fc := d.state.FanCurve; fc != nil && fc.Mode == 1 && len(fc.Points) == 8 {
			if err := cli.SetBothFanCurves(fc.Points); err != nil {
				slog.Warn("failed to reapply fan curve", "err", err)
			}
		}
		// Re-apply saved TDP.
		if t := d.state.TDP; t != nil {
			if err := cli.SetTDP(0, t.PL1SPL, t.PL2SPPT, t.FPPT); err != nil {
				slog.Warn("failed to reapply TDP", "err", err)
			}
			if t.PL1SPL > cli.TDPMaxSafe {
				_ = cli.SetBothFanCurves(cli.HighTDPFanCurve())
			}
		}
		// Re-apply saved undervolt.
		if uv := d.state.Undervolt; uv != nil && cli.SMUAvailable() {
			if err := cli.SetCurveOptimizer(uv.CPUCO, uv.IGPUCO); err != nil {
				slog.Warn("failed to reapply undervolt", "err", err)
			}
		}
		s := d.state
		d.mu.Unlock()
		slog.Info("profile", "set", "custom")
		if err := saveState(s); err != nil {
			slog.Warn("failed to save state", "err", err)
		}
		return response{OK: true}
	}

	// Stock profile: reset fan curves to auto and UV to stock, then write to
	// platform_profile. The firmware sets per-profile PPT and fan curves automatically.
	if err := cli.ResetAllFanCurves(); err != nil {
		slog.Warn("failed to reset fan curves to auto", "err", err)
	}
	if cli.SMUAvailable() {
		if err := cli.ResetCurveOptimizer(); err != nil {
			slog.Warn("failed to reset undervolt", "err", err)
		}
	}
	if err := cli.SetProfile(profile); err != nil {
		return response{OK: false, Error: "profile: " + err.Error()}
	}

	slog.Info("profile", "set", profile)
	d.mu.Lock()
	d.state.Profile = profile
	s := d.state
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
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
	s := d.state
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

func handlePanelOverdrive(req request) response {
	value, err := strconv.Atoi(req.Set)
	if err != nil || (value != 0 && value != 1) {
		return response{OK: false, Error: "panel overdrive must be 0 or 1"}
	}
	if err := cli.SetPanelOverdrive(value); err != nil {
		return response{OK: false, Error: "paneloverdrive: " + err.Error()}
	}
	slog.Info("paneloverdrive", "set", value)
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
	// Enforce minimum PWM floor when sustained TDP exceeds safe max.
	d.mu.Lock()
	tdp := d.state.TDP
	d.mu.Unlock()
	if tdp != nil && tdp.PL1SPL > cli.TDPMaxSafe {
		for _, p := range points {
			if p.PWM < cli.HighTDPMinPWM {
				return response{OK: false, Error: fmt.Sprintf(
					"fancurve: PWM %d at %d°C is below minimum %d (80%%) required when sustained TDP is above %dW",
					p.PWM, p.Temp, cli.HighTDPMinPWM, cli.TDPMaxSafe)}
			}
		}
	}
	if err := cli.SetBothFanCurves(points); err != nil {
		return response{OK: false, Error: "fancurve: " + err.Error()}
	}
	slog.Info("fancurve", "fans", "both")
	d.mu.Lock()
	d.state.FanCurve = &api.FanCurveState{Mode: 1, Points: points}
	d.state.Profile = "custom"
	s := d.state
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func (d *Daemon) handleFanCurveReset() response {
	if err := cli.ResetAllFanCurves(); err != nil {
		return response{OK: false, Error: "fancurve-reset: " + err.Error()}
	}
	slog.Info("fancurve-reset", "fans", "both")
	d.mu.Lock()
	d.state.FanCurve = nil
	s := d.state
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func handleTDPGet() response {
	tdp, err := cli.ReadEffectivePPT(readProfileFromSysfs())
	if err != nil {
		return response{OK: false, Error: "reading TDP: " + err.Error()}
	}
	data, _ := json.Marshal(tdp)
	return response{OK: true, Value: string(data)}
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

	// Safety: set fans to 80% minimum when sustained TDP exceeds safe max.
	if pl1 > cli.TDPMaxSafe {
		if err := cli.SetBothFanCurves(cli.HighTDPFanCurve()); err != nil {
			return response{OK: false, Error: "cannot set high-TDP fan curve: " + err.Error()}
		}
		slog.Warn("fans set to 80%+ curve for high TDP", "pl1", pl1)
	}

	if err := cli.SetTDP(watts, pl1, pl2, pl3); err != nil {
		return response{OK: false, Error: "tdp: " + err.Error()}
	}
	slog.Info("tdp", "pl1", pl1, "pl2", pl2, "pl3", pl3)

	d.mu.Lock()
	d.state.TDP = &api.TDPState{
		PL1SPL:       pl1,
		PL2SPPT:      pl2,
		FPPT:         pl3,
		APUSPPT:      pl2,
		PlatformSPPT: pl2,
	}
	d.state.Profile = "custom"
	fc := d.state.FanCurve
	s := d.state
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}

	// If sustained TDP is now safe, restore saved fan curve (undo high-TDP curve).
	if pl1 <= cli.TDPMaxSafe {
		if fc != nil && fc.Mode == 1 && len(fc.Points) == 8 {
			if err := cli.SetBothFanCurves(fc.Points); err != nil {
				slog.Warn("failed to restore fan curve after TDP change", "err", err)
			} else {
				slog.Info("fan curve restored after TDP reduced to safe levels")
			}
		}
	}

	return response{OK: true}
}

func (d *Daemon) handleTDPReset() response {
	// Reset fans to auto mode (undo any full-speed override from high TDP),
	// then switch to balanced profile. The firmware sets per-profile PPT
	// values and fan curves automatically on profile change.
	if err := cli.ResetAllFanCurves(); err != nil {
		slog.Warn("failed to reset fan curves after TDP reset", "err", err)
	}
	if err := cli.SetProfile("balanced"); err != nil {
		return response{OK: false, Error: "tdp-reset: switching to balanced profile: " + err.Error()}
	}
	slog.Info("tdp-reset", "profile", "balanced")
	d.mu.Lock()
	d.state.TDP = nil
	d.state.FanCurve = nil
	d.state.Profile = "balanced"
	s := d.state
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func (d *Daemon) handleUndervoltGet() response {
	if !cli.SMUAvailable() {
		return response{OK: false, Error: "ryzen_smu kernel module not detected"}
	}
	d.mu.Lock()
	uv := d.state.Undervolt
	profile := d.state.Profile
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
	if !cli.SMUAvailable() {
		return response{OK: false, Error: "ryzen_smu kernel module not detected; install ryzen_smu-dkms-git (AUR) or equivalent"}
	}

	cpuOffset := 0
	if req.Set != "" {
		v, err := strconv.Atoi(req.Set)
		if err != nil {
			return response{OK: false, Error: "invalid CPU undervolt value: must be an integer"}
		}
		cpuOffset = v
	}

	igpuOffset := 0
	if req.IGPU != "" {
		v, err := strconv.Atoi(req.IGPU)
		if err != nil {
			return response{OK: false, Error: "invalid iGPU undervolt value: must be an integer"}
		}
		igpuOffset = v
	}

	if err := cli.ValidateCOValues(cpuOffset, igpuOffset); err != nil {
		return response{OK: false, Error: err.Error()}
	}

	if err := cli.SetCurveOptimizer(cpuOffset, igpuOffset); err != nil {
		return response{OK: false, Error: "undervolt: " + err.Error()}
	}

	slog.Info("undervolt", "cpu", cpuOffset, "igpu", igpuOffset)
	d.mu.Lock()
	d.state.Undervolt = &api.UndervoltState{CPUCO: cpuOffset, IGPUCO: igpuOffset}
	d.state.Profile = "custom"
	s := d.state
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	return response{OK: true}
}

func (d *Daemon) handleUndervoltReset() response {
	if !cli.SMUAvailable() {
		return response{OK: false, Error: "ryzen_smu kernel module not detected"}
	}

	if err := cli.ResetCurveOptimizer(); err != nil {
		return response{OK: false, Error: "undervolt-reset: " + err.Error()}
	}

	slog.Info("undervolt-reset")
	d.mu.Lock()
	d.state.Undervolt = nil
	s := d.state
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

