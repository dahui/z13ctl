package cli

// vramhelper.go — the iGPU memory setting for callers that are not root.
//
// /proc/acpi/call is root-only, and granting a group write access to it would
// grant any ACPI method (see vram.go). The z13ctl-vram helper is the narrow
// alternative: a root service, socket-activated with one process per
// connection, that answers exactly two requests — read the setting, or store
// one of the sizes the firmware lists. Its socket is group-writable like the
// other z13ctl grants, so the CLI and the daemon reach it without sudo.
//
// Wire format, one JSON line each way:
//
//	{"cmd":"vram-get"}              -> {"ok":true,"vram":{...api.VRAMState}}
//	{"cmd":"vram","set":"0.5G"}     -> {"ok":true,"vram":{...}}
//	                                   {"ok":false,"error":"..."}

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dahui/z13ctl/api"
)

// isRoot decides between calling the firmware directly and asking the helper.
// A var so tests can take either path.
var isRoot = func() bool { return os.Geteuid() == 0 }

// Bounds on a helper exchange. Each DSTS or DEVS is an SMI round trip, so a
// set (read, write, read the list again) takes well under a second; the
// deadline only stops a wedged helper from hanging the daemon.
var (
	vramHelperDialTimeout = 2 * time.Second
	vramHelperTimeout     = 15 * time.Second
)

type vramHelperRequest struct {
	Cmd string `json:"cmd"`
	Set string `json:"set,omitempty"`
}

type vramHelperResponse struct {
	OK    bool           `json:"ok"`
	Error string         `json:"error,omitempty"`
	VRAM  *api.VRAMState `json:"vram,omitempty"`
}

// VRAMHelperSocket is where the z13ctl-vram helper listens.
func VRAMHelperSocket() string { return vramRunDir + "/vram.sock" }

func vramNextPath() string { return vramRunDir + "/vram-next" }

// VRAMGet returns the iGPU memory state: from the firmware when running as
// root, otherwise through the z13ctl-vram helper.
func VRAMGet() (api.VRAMState, error) {
	if isRoot() {
		return ReadVRAMInfo()
	}
	return callVRAMHelper(vramHelperRequest{Cmd: "vram-get"})
}

// VRAMSet stores size for the next boot and returns the state afterwards,
// directly when root and through the z13ctl-vram helper otherwise.
func VRAMSet(size string) (api.VRAMState, error) {
	if isRoot() {
		return SetVRAMSize(size)
	}
	return callVRAMHelper(vramHelperRequest{Cmd: "vram", Set: size})
}

// ReadVRAMInfo reads the firmware state and the size stored for the next boot.
// Root only.
func ReadVRAMInfo() (api.VRAMState, error) {
	unlock, err := lockVRAM()
	if err != nil {
		return api.VRAMState{}, err
	}
	defer unlock()
	st, err := ReadVRAM()
	if err != nil {
		return api.VRAMState{}, err
	}
	next, ok := readVRAMNext(st)
	return vramInfo(st, next, ok), nil
}

// SetVRAMSize parses size against the firmware's options, stores it for the
// next boot and records it under /run so later reads can report it. Root only.
func SetVRAMSize(size string) (api.VRAMState, error) {
	unlock, err := lockVRAM()
	if err != nil {
		return api.VRAMState{}, err
	}
	defer unlock()
	st, err := ReadVRAM()
	if err != nil {
		return api.VRAMState{}, err
	}
	value, err := ParseVRAMSize(size, st)
	if err != nil {
		return api.VRAMState{}, err
	}
	// Written even when it matches st.Current: that is the size in use this
	// boot, and a size stored by an earlier set stays pending until overwritten.
	if err := SetVRAM(value); err != nil {
		return api.VRAMState{}, err
	}
	// Losing the record only hides "next boot" from --get; the firmware has
	// the value either way, so a failure here is not the caller's error.
	if value == st.Current {
		_ = os.Remove(vramNextPath())
	} else {
		_ = os.WriteFile(vramNextPath(), []byte(strconv.Itoa(int(value))+"\n"), 0o644)
	}
	return vramInfo(st, value, true), nil
}

func vramInfo(st VRAMState, next uint16, haveNext bool) api.VRAMState {
	info := api.VRAMState{Current: st.Label(st.Current), Options: st.OptionLabels()}
	if haveNext && next != st.Current {
		info.Next = st.Label(next)
	}
	return info
}

// readVRAMNext returns the size recorded by the last set this boot, ignoring a
// record that is not one of st's options.
func readVRAMNext(st VRAMState) (uint16, bool) {
	data, err := os.ReadFile(vramNextPath())
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 16)
	if err != nil || !st.Offers(uint16(v)) {
		return 0, false
	}
	return uint16(v), true
}

// lockVRAM serialises firmware access: acpi_call keeps one result for the whole
// system, and the helper runs one process per connection.
func lockVRAM() (func(), error) {
	if err := os.MkdirAll(vramRunDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(vramRunDir+"/vram.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

// ServeVRAMHelper answers one request read from r and writes the reply to w.
// It is the whole of the root helper; z13ctl-vram@.service runs it with the
// accepted connection as stdin and stdout.
func ServeVRAMHelper(r io.Reader, w io.Writer) error {
	reply := func(resp vramHelperResponse) error {
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", data)
		return err
	}

	line, err := bufio.NewReaderSize(io.LimitReader(r, 4096), 4096).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	var req vramHelperRequest
	if err = json.Unmarshal([]byte(line), &req); err != nil {
		return reply(vramHelperResponse{Error: "invalid request: " + err.Error()})
	}

	var info api.VRAMState
	switch req.Cmd {
	case "vram-get":
		info, err = ReadVRAMInfo()
	case "vram":
		info, err = SetVRAMSize(req.Set)
	default:
		return reply(vramHelperResponse{Error: fmt.Sprintf("unknown command %q", req.Cmd)})
	}
	if err != nil {
		return reply(vramHelperResponse{Error: err.Error()})
	}
	return reply(vramHelperResponse{OK: true, VRAM: &info})
}

func callVRAMHelper(req vramHelperRequest) (api.VRAMState, error) {
	conn, err := net.DialTimeout("unix", VRAMHelperSocket(), vramHelperDialTimeout)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ECONNREFUSED):
			return api.VRAMState{}, fmt.Errorf("the z13ctl-vram helper is not running (%s): "+
				"enable it with 'sudo systemctl enable --now z13ctl-vram.socket', or run this as root", VRAMHelperSocket())
		case errors.Is(err, os.ErrPermission):
			return api.VRAMState{}, fmt.Errorf("no access to %s: your user must be in the group z13ctl setup granted (default 'users'), or run this as root", VRAMHelperSocket())
		}
		return api.VRAMState{}, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(vramHelperTimeout))

	data, err := json.Marshal(req)
	if err != nil {
		return api.VRAMState{}, err
	}
	if _, err = fmt.Fprintf(conn, "%s\n", data); err != nil {
		return api.VRAMState{}, fmt.Errorf("sending to the z13ctl-vram helper: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && line == "" {
		return api.VRAMState{}, fmt.Errorf("no reply from the z13ctl-vram helper: %w", err)
	}
	var resp vramHelperResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return api.VRAMState{}, fmt.Errorf("invalid reply from the z13ctl-vram helper: %w", err)
	}
	if !resp.OK {
		return api.VRAMState{}, errors.New(resp.Error)
	}
	if resp.VRAM == nil {
		return api.VRAMState{}, errors.New("the z13ctl-vram helper replied without a state")
	}
	return *resp.VRAM, nil
}
