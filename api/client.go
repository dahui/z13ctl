package api

// client.go — socket client for communicating with the z13ctl daemon.
//
// Each Send* function connects to the daemon's Unix socket, sends one JSON
// command, and reads one JSON response. If the daemon is not running
// (connection refused), functions return (false, nil) so callers can fall
// back to direct hardware access.
//
// Subscribe opens a long-lived connection and returns a channel that receives
// event name strings streamed by the daemon. Event names and their meanings are
// in events.go.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SocketPath returns the runtime path for the daemon's Unix socket.
func SocketPath() string {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = "/tmp"
	}
	return runtime + "/z13ctl/z13ctl.sock"
}

// request is a command sent to the daemon over the socket.
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
	// Empty means the active profile, which is also what every client written
	// before this field existed sends.
	Profile string `json:"profile,omitempty"`
	AC      string `json:"ac,omitempty"`
	Battery string `json:"battery,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

// response is the reply to a command or a streamed event notification.
type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Value string `json:"value,omitempty"`
	State *State `json:"state,omitempty"`
	Event string `json:"event,omitempty"`
}

// dialTimeout bounds establishing the connection; commandTimeout bounds the
// whole request/response exchange once connected. Without the second one a
// daemon that accepts but never replies — hung on a sysfs write, or wedged in a
// handler — blocks the caller forever instead of failing.
// Declared as vars, not consts, so tests can shorten them; nothing outside
// tests should assign to them.
var (
	dialTimeout    = time.Second
	commandTimeout = 10 * time.Second
)

// sendCommand connects to the daemon and sends req, returning the response.
// Returns (false, nil, nil) if the daemon is not running.
func sendCommand(req request) (bool, *response, error) {
	conn, err := net.DialTimeout("unix", SocketPath(), dialTimeout)
	if err != nil {
		return false, nil, nil // daemon not running
	}
	defer func() { _ = conn.Close() }()

	// Generous enough for the slowest command (a TDP change rewrites both fan
	// curves first), short enough that the CLI cannot hang indefinitely.
	_ = conn.SetDeadline(time.Now().Add(commandTimeout))

	data, err := json.Marshal(req)
	if err != nil {
		return true, nil, err
	}
	if _, err := fmt.Fprintf(conn, "%s\n", data); err != nil {
		return true, nil, err
	}

	var resp response
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return true, nil, fmt.Errorf("reading daemon response: %w", err)
		}
		return true, nil, fmt.Errorf("no response from daemon")
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return true, nil, err
	}
	return true, &resp, nil
}

// SendApply sends an apply command to the daemon. color and color2 must be
// "RRGGBB" hex strings. device may be "keyboard", "lightbar", a /dev/hidrawN
// path, or "" to target all devices. Returns (true, nil) on success, (false,
// nil) if the daemon is not running (caller should fall back to direct HID access).
func SendApply(device, color, color2, mode, speed string, brightness int) (bool, error) {
	handled, resp, err := sendCommand(request{
		Cmd:        "apply",
		Mode:       mode,
		Color:      color,
		Color2:     color2,
		Speed:      speed,
		Brightness: brightness,
		Device:     device,
	})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendOff sends an off command to the daemon. device may be "keyboard",
// "lightbar", a /dev/hidrawN path, or "" to target all devices.
func SendOff(device string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "off", Device: device})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendBrightness sends a brightness-only command to the daemon. device may be
// "keyboard", "lightbar", a /dev/hidrawN path, or "" to target all devices.
func SendBrightness(device string, level int) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "brightness", Brightness: level, Device: device})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendProfileGet queries the daemon for the current performance profile by
// reading sysfs (not cached daemon state). Intended for GUI/plugin callers.
// Returns (false, "", nil) if the daemon is not running.
func SendProfileGet() (handled bool, profile string, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "profile-get"})
	if !handled || err != nil {
		return handled, "", err
	}
	if !resp.OK {
		return true, "", fmt.Errorf("%s", resp.Error)
	}
	return true, resp.Value, nil
}

// SendBatteryLimitGet queries the daemon for the current battery charge limit by
// reading sysfs (not cached daemon state). Intended for GUI/plugin callers.
// Returns (false, 0, nil) if the daemon is not running.
func SendBatteryLimitGet() (handled bool, limit int, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "batterylimit-get"})
	if !handled || err != nil {
		return handled, 0, err
	}
	if !resp.OK {
		return true, 0, fmt.Errorf("%s", resp.Error)
	}
	limit, parseErr := strconv.Atoi(strings.TrimSpace(resp.Value))
	if parseErr != nil {
		return true, 0, fmt.Errorf("invalid battery limit value %q: %w", resp.Value, parseErr)
	}
	return true, limit, nil
}

// SendProfileSet sends a profile set command to the daemon.
func SendProfileSet(profile string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "profile", Set: profile})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendBatteryLimitSet sends a battery limit set command to the daemon.
func SendBatteryLimitSet(limit int) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "batterylimit", Set: fmt.Sprintf("%d", limit)})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendBootSoundSet sends a boot sound set command to the daemon.
func SendBootSoundSet(value int) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "bootsound", Set: fmt.Sprintf("%d", value)})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendPanelOverdriveSet sends a panel overdrive set command to the daemon.
func SendPanelOverdriveSet(value int) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "paneloverdrive", Set: fmt.Sprintf("%d", value)})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendBootSoundGet queries the daemon for the current boot sound setting by
// reading sysfs. Returns (false, 0, nil) if the daemon is not running.
func SendBootSoundGet() (handled bool, value int, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "bootsound-get"})
	if !handled || err != nil {
		return handled, 0, err
	}
	if !resp.OK {
		return true, 0, fmt.Errorf("%s", resp.Error)
	}
	value, parseErr := strconv.Atoi(strings.TrimSpace(resp.Value))
	if parseErr != nil {
		return true, 0, fmt.Errorf("invalid boot sound value %q: %w", resp.Value, parseErr)
	}
	return true, value, nil
}

// SendPanelOverdriveGet queries the daemon for the current panel overdrive
// setting by reading sysfs. Returns (false, 0, nil) if the daemon is not running.
func SendPanelOverdriveGet() (handled bool, value int, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "paneloverdrive-get"})
	if !handled || err != nil {
		return handled, 0, err
	}
	if !resp.OK {
		return true, 0, fmt.Errorf("%s", resp.Error)
	}
	value, parseErr := strconv.Atoi(strings.TrimSpace(resp.Value))
	if parseErr != nil {
		return true, 0, fmt.Errorf("invalid panel overdrive value %q: %w", resp.Value, parseErr)
	}
	return true, value, nil
}

// SendFanCurveGet queries the daemon for the current fan curve data.
// Returns JSON value string with both fans' curve and mode.
func SendFanCurveGet() (handled bool, value string, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "fancurve-get"})
	if !handled || err != nil {
		return handled, "", err
	}
	if !resp.OK {
		return true, "", fmt.Errorf("%s", resp.Error)
	}
	return true, resp.Value, nil
}

// SendFanCurveSet sends a fan curve set command to the daemon, editing the
// active custom profile. The curve is applied to both fans simultaneously.
func SendFanCurveSet(curve string) (bool, error) {
	return SendFanCurveSetFor("", curve)
}

// SendFanCurveSetFor stores a fan curve in the named custom profile. An empty
// profile means the active one, in which case the curve is also written to
// hardware; naming a profile that is not active only records it.
func SendFanCurveSetFor(profile, curve string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "fancurve", Set: curve, Profile: profile})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendFanCurveReset resets both fans to firmware auto mode.
func SendFanCurveReset() (bool, error) {
	return SendFanCurveResetFor("")
}

// SendFanCurveResetFor clears the fan curve from the named custom profile. An
// empty profile means the active one, which also releases both fans to firmware
// auto mode.
func SendFanCurveResetFor(profile string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "fancurve-reset", Profile: profile})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendTdpGet queries the daemon for current TDP/PPT values.
// Returns JSON value string.
func SendTdpGet() (handled bool, value string, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "tdp-get"})
	if !handled || err != nil {
		return handled, "", err
	}
	if !resp.OK {
		return true, "", fmt.Errorf("%s", resp.Error)
	}
	return true, resp.Value, nil
}

// SendTdpSet sends a TDP set command to the daemon, editing the active custom
// profile.
func SendTdpSet(watts, pl1, pl2, pl3 string, force bool) (bool, error) {
	return SendTdpSetFor("", watts, pl1, pl2, pl3, force)
}

// SendTdpSetFor stores TDP limits in the named custom profile. An empty profile
// means the active one, in which case the limits are also written to hardware;
// naming a profile that is not active only records them.
func SendTdpSetFor(profile, watts, pl1, pl2, pl3 string, force bool) (bool, error) {
	handled, resp, err := sendCommand(request{
		Cmd:     "tdp",
		Set:     watts,
		PL1:     pl1,
		PL2:     pl2,
		PL3:     pl3,
		Force:   force,
		Profile: profile,
	})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendTdpReset sends a TDP reset command to the daemon.
func SendTdpReset() (bool, error) {
	return SendTdpResetFor("")
}

// SendTdpResetFor clears the TDP limits from the named custom profile. An empty
// profile means the active one, which also restores stock power limits.
func SendTdpResetFor(profile string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "tdp-reset", Profile: profile})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendUndervoltGet queries the daemon for the current Curve Optimizer offsets.
// Returns JSON value string. Returns (false, "", nil) if the daemon is not running.
func SendUndervoltGet() (handled bool, value string, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "undervolt-get"})
	if !handled || err != nil {
		return handled, "", err
	}
	if !resp.OK {
		return true, "", fmt.Errorf("%s", resp.Error)
	}
	return true, resp.Value, nil
}

// SendUndervoltSet sends a Curve Optimizer set command to the daemon, editing
// the active custom profile. cpu is a string representation of the CO offset
// (e.g. "-20").
func SendUndervoltSet(cpu string) (bool, error) {
	return SendUndervoltSetFor("", cpu)
}

// SendUndervoltSetFor stores a Curve Optimizer offset in the named custom
// profile. An empty profile means the active one, in which case the offset is
// also applied to hardware; naming a profile that is not active only records it.
func SendUndervoltSetFor(profile, cpu string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "undervolt", Set: cpu, Profile: profile})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendUndervoltReset resets Curve Optimizer to stock (0).
func SendUndervoltReset() (bool, error) {
	return SendUndervoltResetFor("")
}

// SendUndervoltResetFor clears the Curve Optimizer offset from the named custom
// profile. An empty profile means the active one, which also resets hardware.
func SendUndervoltResetFor(profile string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "undervolt-reset", Profile: profile})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendProfileCreate creates an empty custom profile. It does not activate it.
func SendProfileCreate(name string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "profile-create", Set: name})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendProfileSave copies the active custom profile under a new name. It does
// not activate the copy.
func SendProfileSave(name string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "profile-save", Set: name})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendProfileDelete removes a saved custom profile. The daemon refuses to
// delete the active profile or one referenced by autoswitch.
func SendProfileDelete(name string) (bool, error) {
	handled, resp, err := sendCommand(request{Cmd: "profile-delete", Set: name})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendProfileList queries the daemon for the saved custom profiles.
// Returns a JSON array of CustomProfile. Returns (false, "", nil) if the daemon
// is not running.
func SendProfileList() (handled bool, value string, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "profile-list"})
	if !handled || err != nil {
		return handled, "", err
	}
	if !resp.OK {
		return true, "", fmt.Errorf("%s", resp.Error)
	}
	return true, resp.Value, nil
}

// SendAutoswitchSet configures automatic profile selection by power source.
// An empty ac or battery target leaves the profile alone on that source.
func SendAutoswitchSet(enabled bool, ac, battery string) (bool, error) {
	handled, resp, err := sendCommand(request{
		Cmd:     "autoswitch",
		Enabled: enabled,
		AC:      ac,
		Battery: battery,
	})
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, fmt.Errorf("%s", resp.Error)
	}
	return true, nil
}

// SendAutoswitchGet queries the daemon for the autoswitch configuration.
// Returns a JSON AutoswitchState. Returns (false, "", nil) if the daemon is not
// running.
func SendAutoswitchGet() (handled bool, value string, err error) {
	var resp *response
	handled, resp, err = sendCommand(request{Cmd: "autoswitch-get"})
	if !handled || err != nil {
		return handled, "", err
	}
	if !resp.OK {
		return true, "", fmt.Errorf("%s", resp.Error)
	}
	return true, resp.Value, nil
}

// SendGetState fetches the daemon's full cached state for GUI initialization.
// Returns (false, nil, nil) if the daemon is not running.
func SendGetState() (bool, *State, error) {
	handled, resp, err := sendCommand(request{Cmd: "get-state"})
	if !handled || err != nil {
		return handled, nil, err
	}
	if !resp.OK {
		return true, nil, fmt.Errorf("%s", resp.Error)
	}
	return true, resp.State, nil
}

// Subscribe opens a long-lived subscription to the daemon and returns a channel
// that receives event name strings as they are streamed. Pass the events you
// want — EventGUIToggle, EventPowerSource, EventStateChanged — or nil for all of
// them. The daemon honours the list, so a client that asks only for
// EventGUIToggle will not be woken by anything else.
//
// Events carry no payload by design: the name says what happened, and
// SendGetState answers with current truth. Switch on the name rather than
// treating every event alike —
//
//	for ev := range ch {
//	    switch ev {
//	    case api.EventGUIToggle:
//	        toggleWindow()
//	    case api.EventPowerSource, api.EventStateChanged:
//	        refreshFromGetState()
//	    }
//	}
//
// The returned cancel func closes the underlying connection and stops the
// goroutine; the channel is closed when the connection drops or cancel is called.
// Returns (nil, nil, nil) if the daemon is not running.
func Subscribe(events []string) (eventCh <-chan string, cancel func(), err error) {
	conn, err := net.DialTimeout("unix", SocketPath(), dialTimeout)
	if err != nil {
		return nil, nil, nil // daemon not running
	}

	// Bound the handshake only. The deadline is cleared before streaming
	// begins, since a subscription is idle by design between button presses.
	_ = conn.SetDeadline(time.Now().Add(commandTimeout))

	req := request{Cmd: "subscribe", Events: events}
	data, err := json.Marshal(req)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if _, err := fmt.Fprintf(conn, "%s\n", data); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	// Read the initial OK acknowledgement.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("no response from daemon")
	}
	var ack response
	if err := json.Unmarshal(scanner.Bytes(), &ack); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if !ack.OK {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("subscribe: %s", ack.Error)
	}

	// Streaming is open-ended: clear the handshake deadline.
	_ = conn.SetDeadline(time.Time{})

	ch := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		defer close(ch)
		for scanner.Scan() {
			var ev response
			if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil || ev.Event == "" {
				continue
			}
			// Select on done as well as the send. A caller that stops reading
			// fills the buffer and parks this goroutine in a channel send,
			// where closing the connection cannot reach it — so cancel() alone
			// would leak the goroutine and never close ch.
			select {
			case ch <- ev.Event:
			case <-done:
				return
			}
		}
	}()

	var once sync.Once
	cancel = func() {
		once.Do(func() {
			close(done)
			_ = conn.Close()
		})
	}
	return ch, cancel, nil
}
