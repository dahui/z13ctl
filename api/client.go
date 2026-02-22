package api

// client.go — socket client for communicating with the z13ctl daemon.
//
// Each Send* function connects to the daemon's Unix socket, sends one JSON
// command, and reads one JSON response. If the daemon is not running
// (connection refused), functions return (false, nil) so callers can fall
// back to direct hardware access.
//
// Subscribe opens a long-lived connection and returns a channel that receives
// event name strings streamed by the daemon.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// SocketPath returns the runtime path for the daemon's Unix socket.
func SocketPath() string {
	return socketPath()
}

// socketPath returns the runtime path for the daemon's Unix socket.
func socketPath() string {
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
	State *State `json:"state,omitempty"`
	Event string `json:"event,omitempty"`
}

// sendCommand connects to the daemon and sends req, returning the response.
// Returns (false, nil, nil) if the daemon is not running.
func sendCommand(req request) (bool, *response, error) {
	conn, err := net.DialTimeout("unix", socketPath(), time.Second)
	if err != nil {
		return false, nil, nil // daemon not running
	}
	defer func() { _ = conn.Close() }()

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
	if _, scanErr := fmt.Sscan(resp.Value, &limit); scanErr != nil {
		return true, 0, fmt.Errorf("invalid battery limit value %q: %w", resp.Value, scanErr)
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
	if _, scanErr := fmt.Sscan(resp.Value, &value); scanErr != nil {
		return true, 0, fmt.Errorf("invalid boot sound value %q: %w", resp.Value, scanErr)
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
	if _, scanErr := fmt.Sscan(resp.Value, &value); scanErr != nil {
		return true, 0, fmt.Errorf("invalid panel overdrive value %q: %w", resp.Value, scanErr)
	}
	return true, value, nil
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
// that receives event name strings (e.g. "gui-toggle") as they are streamed.
// The returned cancel func closes the underlying connection and stops the
// goroutine; the channel is closed when the connection drops or cancel is called.
// Returns (nil, nil, nil) if the daemon is not running.
func Subscribe(events []string) (<-chan string, func(), error) {
	conn, err := net.DialTimeout("unix", socketPath(), time.Second)
	if err != nil {
		return nil, nil, nil // daemon not running
	}

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

	ch := make(chan string, 8)
	go func() {
		defer close(ch)
		for scanner.Scan() {
			var ev response
			if err := json.Unmarshal(scanner.Bytes(), &ev); err == nil && ev.Event != "" {
				ch <- ev.Event
			}
		}
	}()

	cancel := func() { _ = conn.Close() }
	return ch, cancel, nil
}
