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
	"github.com/dahui/voltaire/v2/internal/device"
	"net"
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

	d := &Daemon{hw: &device.Device{}}
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

	d := &Daemon{hw: &device.Device{}}
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

// TestEveryToggleWritePathNotifies: all three toggle commands — the generic
// feature command and the two named ones — must tell subscribers the state
// moved. handlePanelOverdrive did from the start while handleBootSound and
// handleFeature did not, which no client could observe until one rendered
// toggle rows from State.Features: the same switch then updated live or did
// not, depending on which command wrote it. All three now share writeToggle;
// this checks the behaviour over fakeDevice (it used to scan the source,
// because the alternative was a hardware write).
func TestEveryToggleWritePathNotifies(t *testing.T) {
	for _, req := range []request{
		{Cmd: "feature", ID: "boot_sound", Set: "1"},
		{Cmd: "bootsound", Set: "1"},
		{Cmd: "paneloverdrive", Set: "1"},
	} {
		t.Run(req.Cmd, func(t *testing.T) {
			d, _ := fakeDaemon(t)
			client, server := net.Pipe()
			defer func() { _ = client.Close() }()
			d.addSubscriber(server, []string{api.EventStateChanged})
			got := make(chan string, 1)
			go func() {
				_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
				line, _ := bufio.NewReader(client).ReadString('\n')
				got <- line
			}()
			if resp := d.dispatch(req); !resp.OK {
				t.Fatalf("%s: %s", req.Cmd, resp.Error)
			}
			if line := <-got; !strings.Contains(line, api.EventStateChanged) {
				t.Errorf("%s did not notify subscribers (read %q)", req.Cmd, line)
			}
		})
	}
}
