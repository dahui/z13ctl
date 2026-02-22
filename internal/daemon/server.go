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
	Device     string   `json:"device,omitempty"`  // "keyboard", "lightbar", or /dev/hidrawN; empty = all
	Events     []string `json:"events,omitempty"`
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
	case "get-state":
		d.mu.Lock()
		s := d.state
		d.mu.Unlock()
		// Populate firmware-managed fields from sysfs (not cached in daemon state).
		s.BootSound = readIntSysfs(cli.FindBootSoundPath())
		s.PanelOverdrive = readIntSysfs(cli.FindPanelOverdrivePath())
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
	if err := cli.SetProfile(req.Set); err != nil {
		return response{OK: false, Error: "profile: " + err.Error()}
	}
	slog.Info("profile", "set", req.Set)
	d.mu.Lock()
	d.state.Profile = req.Set
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

func writeResponse(conn net.Conn, r response) {
	data, _ := json.Marshal(r)
	_, _ = fmt.Fprintf(conn, "%s\n", data)
}

