package daemon

// toggle_test.go — the firmware-toggle write paths' one shared obligation:
// telling clients the value moved.
//
// The Set calls themselves are hardware writes, so these cases drive
// notifyToggleChanged rather than the handlers around it — the standing rule
// that a daemon test must return before any hardware access (see
// TestHandleTDPForceBoundaryRejections). What the handlers add on top is one
// call each, and TestEveryToggleWritePathNotifies is what keeps a fourth one
// from being added without it.

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dahui/voltaire/api/v2"
)

// TestNotifyToggleChangedEmitsStateChanged is the property the settings page
// depends on: a toggle written by *any* client is a state change every other
// client is told about. Toggle values reach clients through get-state, so a
// client that re-reads sees truth — what it had no way to learn was that there
// was anything to re-read.
func TestNotifyToggleChangedEmitsStateChanged(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	d := &Daemon{}
	d.addSubscriber(server, []string{api.EventStateChanged})

	go d.notifyToggleChanged("boot_sound", 1)

	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		t.Fatalf("no broadcast after a toggle write: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("broadcast is not valid JSON: %v (%q)", err, line)
	}
	if got["event"] != api.EventStateChanged {
		t.Errorf("event = %v, want %q", got["event"], api.EventStateChanged)
	}
}

// TestNotifyToggleChangedProjectsTheNamedFields: the two ids pre-2.0 clients
// know keep their named State fields in step, and an id outside that vocabulary
// changes neither — its value travels in State.Features, which is read from
// hardware, so there is nothing here to write for it.
func TestNotifyToggleChangedProjectsTheNamedFields(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	d := &Daemon{}
	d.notifyToggleChanged("boot_sound", 1)
	d.notifyToggleChanged("panel_overdrive", 1)
	if d.state.BootSound != 1 || d.state.PanelOverdrive != 1 {
		t.Errorf("state = boot %d, overdrive %d; want both 1",
			d.state.BootSound, d.state.PanelOverdrive)
	}

	d.notifyToggleChanged("boot_sound", 0)
	if d.state.BootSound != 0 {
		t.Errorf("BootSound = %d after clearing it, want 0", d.state.BootSound)
	}
	if d.state.PanelOverdrive != 1 {
		t.Errorf("PanelOverdrive = %d; one toggle's write moved another's value",
			d.state.PanelOverdrive)
	}

	before := d.state
	d.notifyToggleChanged("some_future_toggle", 1)
	if d.state.BootSound != before.BootSound || d.state.PanelOverdrive != before.PanelOverdrive {
		t.Error("an unknown toggle id wrote one of the named fields")
	}
}

// TestEveryToggleWritePathNotifies is a source check, and it is a source check
// because the alternative is a hardware write.
//
// All three write paths — the generic feature command and the two named ones —
// must call notifyToggleChanged. handlePanelOverdrive did the equivalent inline
// from the start while handleBootSound and handleFeature did nothing, which no
// client could observe until one rendered toggle rows from State.Features: the
// same switch then updated live or did not, depending on which command wrote
// it. A fourth toggle handler added without the call is the same bug again.
func TestEveryToggleWritePathNotifies(t *testing.T) {
	sources := map[string][]string{
		"internal/daemon/server.go":     {"handleBootSound", "handlePanelOverdrive"},
		"internal/daemon/deviceinfo.go": {"handleFeature"},
	}
	for file, funcs := range sources {
		src, err := os.ReadFile("../../" + file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for _, fn := range funcs {
			body := funcBody(t, string(src), "func (d *Daemon) "+fn+"(")
			if !strings.Contains(body, "notifyToggleChanged") {
				t.Errorf("%s writes a firmware toggle without calling notifyToggleChanged; "+
					"every client showing that toggle stays stale until something else "+
					"happens to refresh it", fn)
			}
		}
	}
}

// funcBody returns the source between a function's signature and the next
// top-level closing brace, with line comments stripped.
//
// The stripping is not tidiness. Written without it this guard passed with the
// call deleted, because each of these functions *mentions* notifyToggleChanged
// in a comment — so the check was reading prose and reporting it as code. That
// was caught by running the negative control, which is the only thing that
// would have caught it.
func funcBody(t *testing.T, src, signature string) string {
	t.Helper()
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("%s not found; this guard needs updating with the rename", signature)
	}
	rest := src[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		rest = rest[:end]
	}
	var code strings.Builder
	for _, line := range strings.Split(rest, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}
	return code.String()
}
