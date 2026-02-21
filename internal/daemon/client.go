package daemon

// client.go — socket client used by CLI commands to reach the running daemon.
//
// Each Send* function connects to the daemon's Unix socket, sends one JSON
// command, and reads one JSON response. If the daemon is not running
// (connection refused), the function returns (false, nil) so the caller can
// fall back to direct hardware access.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// socketPath returns the runtime path for the daemon's Unix socket.
func socketPath() string {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = "/tmp"
	}
	return runtime + "/z13ctl/z13ctl.sock"
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
