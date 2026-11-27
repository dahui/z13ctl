package api

// client.go — socket client for communicating with the voltaire daemon.
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
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnknownCommand reports that the daemon rejected a command it does not
// know, which is how a client detects a daemon older than itself. It is
// distinct from "the daemon is not running" (Send* returns handled=false) and
// from a command that ran and failed.
//
// The distinction matters during an upgrade: the package replaces the binary
// while the daemon keeps running the old one until the user restarts it, so a
// new CLI routinely meets an old daemon. Callers whose command has an
// equivalent direct-hardware path — the firmware toggles, which are not daemon
// state — should treat this as unhandled and fall back. Callers whose command
// only the daemon can perform should report it.
//
// Detect capabilities by probing for this error, never by comparing version
// numbers.
var ErrUnknownCommand = errors.New("daemon does not support this command")

// ErrECWedged reports that the daemon refused a command because the embedded
// controller has not answered since a resume (ECWedgedMessage). It clears on
// its own once the EC responds.
var ErrECWedged = errors.New(ECWedgedMessage)

// SocketPath returns the canonical runtime path for the daemon's Unix socket.
func SocketPath() string {
	return runtimeDir() + "/voltaire/voltaire.sock"
}

// SocketPaths returns every socket path the daemon serves, canonical first.
//
// The second entry is the pre-rename z13ctl path. The daemon listens on both
// through the whole 2.x line so clients that hardcode the old path — the
// Decky plugin speaks it directly, and pre-2.0 Go clients compiled it in —
// keep working across the rename; it is removed at 3.0. Clients dial in order
// and use the first that answers, which also covers the upgrade window where
// a pre-2.0 daemon is still running on the old path only.
func SocketPaths() []string {
	return []string{
		SocketPath(),
		runtimeDir() + "/z13ctl/z13ctl.sock",
	}
}

func runtimeDir() string {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = "/tmp"
	}
	return runtime
}

// dialDaemon tries each socket path in order and returns the first connection
// that succeeds. The returned error is the canonical path's, which is the one
// worth reporting when no daemon answers anywhere.
func dialDaemon() (net.Conn, error) {
	var firstErr error
	for _, sock := range SocketPaths() {
		conn, err := net.DialTimeout("unix", sock, dialTimeout)
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

// unknownCommandError carries the daemon's own wording while matching
// ErrUnknownCommand, so a client can branch on the cause without any Send*
// caller having to reword the daemon.
type unknownCommandError struct{ text string }

func (e *unknownCommandError) Error() string { return e.text }
func (e *unknownCommandError) Unwrap() error { return ErrUnknownCommand }

// respErr turns a daemon error reply into an error, tagging the
// unknown-command case so callers can distinguish an older daemon from a
// command that ran and failed. Every Send* goes through it; a new command
// added to the protocol therefore gets the distinction for free.
func respErr(resp *Response) error {
	switch {
	case strings.HasPrefix(resp.Error, "unknown command"):
		return &unknownCommandError{text: resp.Error}
	case resp.Error == ECWedgedMessage:
		return fmt.Errorf("%w", ErrECWedged)
	}
	return fmt.Errorf("%s", resp.Error)
}

// sendJSON sends req and decodes the response's value (JSON carried in the
// value field, the convention of the commands that predate typed replies).
func sendJSON[T any](req Request) (handled bool, out *T, err error) {
	handled, value, err := sendValue(req)
	if !handled || err != nil {
		return handled, nil, err
	}
	out = new(T)
	if err := json.Unmarshal([]byte(value), out); err != nil {
		return true, nil, fmt.Errorf("decoding %s reply: %w", req.Cmd, err)
	}
	return true, out, nil
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

// Response size bounds for the reader. initialResponseBuf is what almost every
// command fits in; maxResponseBytes is the ceiling a bulk reply may reach.
const (
	initialResponseBuf = 64 * 1024
	maxResponseBytes   = 4 * 1024 * 1024
)

// sendCommand connects to the daemon and sends req, returning the response.
// Returns (false, nil, nil) if the daemon is not running.
func sendCommand(req Request) (bool, *Response, error) {
	conn, err := dialDaemon()
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

	var resp Response
	scanner := bufio.NewScanner(conn)
	// bufio.Scanner's default ceiling is 64 KiB, which every command fitted
	// under until telemetry-history: a device declaring an hour of 1 Hz history
	// answers with 3600 samples, and the response would be rejected as "token
	// too long" — a failure that looks like a broken daemon and depends on
	// device data. The ceiling is what stops a wedged or hostile daemon growing
	// the client's memory without bound, so it is raised rather than removed.
	scanner.Buffer(make([]byte, 0, initialResponseBuf), maxResponseBytes)
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

// sendOK sends req and reports the outcome of a command that answers with
// nothing but ok: (false, nil) when the daemon is not running.
func sendOK(req Request) (bool, error) {
	handled, resp, err := sendCommand(req)
	if !handled || err != nil {
		return handled, err
	}
	if !resp.OK {
		return true, respErr(resp)
	}
	return true, nil
}

// sendValue sends req and returns the response's value.
func sendValue(req Request) (handled bool, value string, err error) {
	handled, resp, err := sendCommand(req)
	if !handled || err != nil {
		return handled, "", err
	}
	if !resp.OK {
		return true, "", respErr(resp)
	}
	return true, resp.Value, nil
}

// sendInt sends req and parses the response's value as an integer; what
// names the value in the error.
func sendInt(req Request, what string) (handled bool, n int, err error) {
	handled, value, err := sendValue(req)
	if !handled || err != nil {
		return handled, 0, err
	}
	n, err = strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return true, 0, fmt.Errorf("invalid %s value %q: %w", what, value, err)
	}
	return true, n, nil
}

// SendApply sends an apply command to the daemon. color and color2 must be
// "RRGGBB" hex strings. device may be "keyboard", "lightbar", a /dev/hidrawN
// path, or "" to target all devices. Returns (true, nil) on success, (false,
// nil) if the daemon is not running (caller should fall back to direct HID access).
func SendApply(device, color, color2, mode, speed string, brightness int) (bool, error) {
	return sendOK(Request{
		Cmd:        CmdApply,
		Mode:       mode,
		Color:      color,
		Color2:     color2,
		Speed:      speed,
		Brightness: brightness,
		Device:     device,
	})
}

// SendOff sends an off command to the daemon. device may be "keyboard",
// "lightbar", a /dev/hidrawN path, or "" to target all devices.
func SendOff(device string) (bool, error) {
	return sendOK(Request{Cmd: CmdOff, Device: device})
}

// SendBrightness sends a brightness-only command to the daemon. device may be
// "keyboard", "lightbar", a /dev/hidrawN path, or "" to target all devices.
func SendBrightness(device string, level int) (bool, error) {
	return sendOK(Request{Cmd: CmdBrightness, Brightness: level, Device: device})
}

// SendProfileGet queries the daemon for the current performance profile by
// reading sysfs (not cached daemon state). Intended for GUI/plugin callers.
// Returns (false, "", nil) if the daemon is not running.
func SendProfileGet() (handled bool, profile string, err error) {
	return sendValue(Request{Cmd: CmdProfileGet})
}

// SendBatteryLimitGet queries the daemon for the current battery charge limit by
// reading sysfs (not cached daemon state). Intended for GUI/plugin callers.
// Returns (false, 0, nil) if the daemon is not running.
func SendBatteryLimitGet() (handled bool, limit int, err error) {
	return sendInt(Request{Cmd: CmdBatteryLimitGet}, "battery limit")
}

// SendProfileSet sends a profile set command to the daemon.
func SendProfileSet(profile string) (bool, error) {
	return sendOK(Request{Cmd: CmdProfile, Set: profile})
}

// SendBatteryLimitSet sends a battery limit set command to the daemon.
func SendBatteryLimitSet(limit int) (bool, error) {
	return sendOK(Request{Cmd: CmdBatteryLimit, Set: fmt.Sprintf("%d", limit)})
}

// SendButtonSet chooses which hardware button opens voltaire: a button id
// from DeviceInfo.HardwareButtons, DrawerButtonNone, or DrawerButtonDefault to
// go back to the device's default. The daemon stores the choice; no hardware
// is touched. An older daemon answers "unknown command".
func SendButtonSet(choice string) (bool, error) {
	return sendOK(Request{Cmd: CmdButtonSet, Set: choice})
}

// SendCPUBoostSet turns the CPU's opportunistic boost clocks on or off.
// Returns (false, nil) if the daemon is not running.
//
// Unlike the firmware toggles this looks like, the setting is not kept by the
// machine: cpufreq comes back with boost enabled on every boot, so the daemon
// records the choice and restores it at startup.
func SendCPUBoostSet(on bool) (bool, error) {
	v := "0"
	if on {
		v = "1"
	}
	return sendOK(Request{Cmd: CmdCPUBoost, Set: v})
}

// SendCPUBoostGet reads whether boost clocks are currently enabled, from the
// hardware rather than from cached state. Returns (false, false, nil) if the
// daemon is not running.
func SendCPUBoostGet() (handled, on bool, err error) {
	handled, v, err := sendInt(Request{Cmd: CmdCPUBoostGet}, "cpu boost")
	return handled, v != 0, err
}

// SendRumbleSet sets the game controller's vibration strength, on device-get's
// rumble.min..max scale. Returns (false, nil) if the daemon is not running.
// The daemon records the value and puts it back at startup and after a
// suspend.
func SendRumbleSet(strength int) (bool, error) {
	return sendOK(Request{Cmd: CmdRumble, Set: strconv.Itoa(strength)})
}

// SendRumbleGet reads the controller's vibration strength from the hardware.
// Returns (false, 0, nil) if the daemon is not running.
func SendRumbleGet() (handled bool, strength int, err error) {
	return sendInt(Request{Cmd: CmdRumbleGet}, "rumble")
}

// SendFanCurveGet reads the fan curve in force and the fans' mode from the
// hardware. Returns (false, nil, nil) if the daemon is not running.
func SendFanCurveGet() (bool, *FanCurveState, error) {
	return sendJSON[FanCurveState](Request{Cmd: CmdFanCurveGet})
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
	return sendOK(Request{Cmd: CmdFanCurve, Set: curve, Profile: profile})
}

// SendFanCurveReset resets both fans to firmware auto mode.
func SendFanCurveReset() (bool, error) {
	return SendFanCurveResetFor("")
}

// SendFanCurveResetFor clears the fan curve from the named custom profile. An
// empty profile means the active one, which also releases both fans to firmware
// auto mode.
func SendFanCurveResetFor(profile string) (bool, error) {
	return sendOK(Request{Cmd: CmdFanCurveReset, Profile: profile})
}

// SendTdpGet reads the power limits: a firmware profile's own row, or a custom
// profile's limits as the hardware reads back. Returns (false, nil, nil) if the
// daemon is not running; errors.Is(err, ErrECWedged) while the EC is not
// answering.
func SendTdpGet() (bool, *TDPState, error) {
	return sendJSON[TDPState](Request{Cmd: CmdTDPGet})
}

// SendTdpSet sends a TDP set command to the daemon, editing the active custom
// profile.
func SendTdpSet(watts, pl1, pl2, pl3 string, force bool) (bool, error) {
	return SendTdpSetFor("", watts, pl1, pl2, pl3, force)
}

// SendTdpSetFor stores TDP limits in the named custom profile. An empty profile
// means the active one, in which case the limits are also written to hardware;
// naming a profile that is not active only records them. The accepted ranges
// are in the device-get document's PowerInfo: a PL2 or PL3 below its minimum is
// raised to it, and a PL1 outside its range, or above TDPMaxSafe without force,
// is refused.
func SendTdpSetFor(profile, watts, pl1, pl2, pl3 string, force bool) (bool, error) {
	return sendOK(Request{
		Cmd:     CmdTDP,
		Set:     watts,
		PL1:     pl1,
		PL2:     pl2,
		PL3:     pl3,
		Force:   force,
		Profile: profile,
	})
}

// SendTdpReset sends a TDP reset command to the daemon.
func SendTdpReset() (bool, error) {
	return SendTdpResetFor("")
}

// SendTdpResetFor clears the TDP limits from the named custom profile. An empty
// profile means the active one, which also switches to balanced and hands the
// power limits back to the firmware.
func SendTdpResetFor(profile string) (bool, error) {
	return sendOK(Request{Cmd: CmdTDPReset, Profile: profile})
}

// SendTuningReset clears every tuning override at once — fan curve, power
// limits and Curve Optimizer offset — and lands on the device's default
// firmware profile (DeviceInfo.Profiles.Default; "balanced" on the Z13).
//
// It is not the same as issuing the three resets in sequence. Each of those has
// to lower power before releasing the fans, so doing it by hand in the wrong
// order leaves a window at a high sustained limit with no fan floor; sending one
// command puts that ordering inside the daemon where it cannot be got wrong.
//
// Returns (false, nil) if the daemon is not running. A daemon older than this
// command answers "unknown command", so a client offering it should be prepared
// to fall back to the three individual resets.
func SendTuningReset() (bool, error) {
	return SendTuningResetFor("")
}

// SendTuningResetFor clears every tuning override from the named custom
// profile. An empty profile means the active one, which also writes hardware;
// naming a profile that is not running stores the change only.
func SendTuningResetFor(profile string) (bool, error) {
	return sendOK(Request{Cmd: CmdTuningReset, Profile: profile})
}

// SendUndervoltGet reads the stored Curve Optimizer offset and the profile
// running (CO has no hardware readback). Returns (false, nil, nil) if the
// daemon is not running.
func SendUndervoltGet() (bool, *UndervoltReading, error) {
	return sendJSON[UndervoltReading](Request{Cmd: CmdUndervoltGet})
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
	return sendOK(Request{Cmd: CmdUndervolt, Set: cpu, Profile: profile})
}

// SendUndervoltReset resets Curve Optimizer to stock (0).
func SendUndervoltReset() (bool, error) {
	return SendUndervoltResetFor("")
}

// SendUndervoltResetFor clears the Curve Optimizer offset from the named custom
// profile. An empty profile means the active one, which also resets hardware if
// an offset is applied.
func SendUndervoltResetFor(profile string) (bool, error) {
	return sendOK(Request{Cmd: CmdUndervoltReset, Profile: profile})
}

// SendProfileCreate creates an empty custom profile. It does not activate it.
func SendProfileCreate(name string) (bool, error) {
	return sendOK(Request{Cmd: CmdProfileCreate, Set: name})
}

// SendProfileSave copies the active custom profile under a new name. It does
// not activate the copy.
func SendProfileSave(name string) (bool, error) {
	return sendOK(Request{Cmd: CmdProfileSave, Set: name})
}

// SendProfileDelete removes a saved custom profile. The daemon refuses to
// delete the active profile or one referenced by autoswitch.
func SendProfileDelete(name string) (bool, error) {
	return sendOK(Request{Cmd: CmdProfileDelete, Set: name})
}

// SendProfileList returns the saved custom profiles, sorted by name, each
// marked whether it is running; "custom" is always listed. Returns
// (false, nil, nil) if the daemon is not running.
func SendProfileList() (bool, []ProfileListEntry, error) {
	handled, list, err := sendJSON[[]ProfileListEntry](Request{Cmd: CmdProfileList})
	if list == nil {
		return handled, nil, err
	}
	return handled, *list, err
}

// SendAutoswitchSet configures automatic profile selection by power source.
// An empty ac or battery target leaves the profile alone on that source.
func SendAutoswitchSet(enabled bool, ac, battery string) (bool, error) {
	return sendOK(Request{
		Cmd:     CmdAutoswitch,
		Enabled: enabled,
		AC:      ac,
		Battery: battery,
	})
}

// SendAutoswitchGet returns the autoswitch configuration and the power source
// now. Returns (false, nil, nil) if the daemon is not running.
func SendAutoswitchGet() (bool, *AutoswitchReading, error) {
	return sendJSON[AutoswitchReading](Request{Cmd: CmdAutoswitchGet})
}

// SendGetState fetches the daemon's full cached state for GUI initialization.
// Returns (false, nil, nil) if the daemon is not running.
func SendGetState() (bool, *State, error) {
	handled, resp, err := sendCommand(Request{Cmd: CmdGetState})
	if !handled || err != nil {
		return handled, nil, err
	}
	if !resp.OK {
		return true, nil, respErr(resp)
	}
	return true, resp.State, nil
}

// SendDeviceGet queries the daemon for the assembled device's identity,
// capabilities, and limits. The document is static for the daemon's lifetime,
// so clients typically fetch it once at startup and render their controls from
// it — a nil section means the capability does not exist on this device.
// handled=false means the daemon is not running; there is no fallback, since
// the capability data lives with the daemon's assembled device.
func SendDeviceGet() (handled bool, info *DeviceInfo, err error) {
	handled, resp, err := sendCommand(Request{Cmd: CmdDeviceGet})
	if !handled || err != nil {
		return handled, nil, err
	}
	if !resp.OK {
		return true, nil, respErr(resp)
	}
	return true, resp.Device, nil
}

// SendTelemetryHistory fetches the samples the daemon recorded within the last
// seconds, oldest first. A non-positive seconds asks for the whole retained
// history, whose length DeviceInfo.Telemetry.HistorySeconds reports in advance.
//
// The result is empty, not an error, when the daemon has been up for less than
// a sample interval or the device keeps no history. handled=false means the
// daemon is not running; there is no fallback, since the history only exists in
// the daemon's memory — nothing persists it, so a restart starts it over.
func SendTelemetryHistory(seconds int) (handled bool, samples []TelemetrySample, err error) {
	handled, resp, err := sendCommand(Request{Cmd: CmdTelemetryHistory, Seconds: seconds})
	if !handled || err != nil {
		return handled, nil, err
	}
	if !resp.OK {
		return true, nil, respErr(resp)
	}
	return true, resp.History, nil
}

// SendFeatureGet queries the daemon for a firmware toggle's current value by
// its wire ID (DeviceInfo.Toggles). handled=false means the daemon is not
// running and the caller should fall back to direct sysfs access.
func SendFeatureGet(id string) (handled bool, value int, err error) {
	return sendInt(Request{Cmd: CmdFeatureGet, ID: id}, "feature "+id)
}

// SendFeatureSet sets a firmware toggle by its wire ID (DeviceInfo.Toggles).
// For "bool" toggles the value must be 0 or 1.
func SendFeatureSet(id string, value int) (bool, error) {
	return sendOK(Request{Cmd: CmdFeature, ID: id, Set: strconv.Itoa(value)})
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
	conn, err := dialDaemon()
	if err != nil {
		return nil, nil, nil // daemon not running
	}

	// Bound the handshake only. The deadline is cleared before streaming
	// begins, since a subscription is idle by design between button presses.
	_ = conn.SetDeadline(time.Now().Add(commandTimeout))

	req := Request{Cmd: CmdSubscribe, Events: events}
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
	var ack Response
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
			var ev Response
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
