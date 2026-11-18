package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
)

// fakeRumble is a controller with a 0–5 strength that records every write.
type fakeRumble struct {
	value   int
	writes  []int
	absent  bool // no controller: every call fails
	readErr bool
}

func (f *fakeRumble) Range() (lo, hi int, err error) {
	if f.absent {
		return 0, 0, errors.New("no hid-oxp controller")
	}
	return 0, 5, nil
}

func (f *fakeRumble) Get() (int, error) {
	if f.absent || f.readErr {
		return 0, errors.New("no hid-oxp controller")
	}
	return f.value, nil
}

func (f *fakeRumble) Set(v int) error {
	if f.absent {
		return errors.New("no hid-oxp controller")
	}
	if v < 0 || v > 5 {
		return fmt.Errorf("rumble strength %d is outside 0–5", v)
	}
	f.value = v
	f.writes = append(f.writes, v)
	return nil
}

func rumbleDaemon(t *testing.T, r *fakeRumble, st api.State) *Daemon {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return &Daemon{hw: &device.Device{Rumble: r}, state: st}
}

// A set writes the controller and records the value as the instruction to
// replay; a refused value writes nothing and records nothing.
func TestRumbleSetRecordsTheInstruction(t *testing.T) {
	r := &fakeRumble{value: 5}
	d := rumbleDaemon(t, r, api.State{})
	if resp := d.handleRumble(request{Set: "2"}); !resp.OK {
		t.Fatalf("handleRumble(2): %s", resp.Error)
	}
	if r.value != 2 || d.state.Rumble == nil || *d.state.Rumble != 2 {
		t.Fatalf("controller %d, state %v; want 2 and 2", r.value, d.state.Rumble)
	}
	for _, bad := range []string{"6", "-1", "x", ""} {
		if resp := d.handleRumble(request{Set: bad}); resp.OK {
			t.Errorf("handleRumble(%q) accepted", bad)
		}
	}
	if len(r.writes) != 1 || *d.state.Rumble != 2 {
		t.Errorf("writes %v, state %d after refusals; want only the one write", r.writes, *d.state.Rumble)
	}
	if resp := d.handleRumbleGet(); !resp.OK || resp.Value != "2" {
		t.Errorf("rumble-get = %+v, want 2", resp)
	}
}

// The replay writes only an instruction the user gave, and only when the
// controller has lost it: no speculative writes, no redundant ones.
func TestRestoreRumbleReplaysOnlyAStoredValueThatWasLost(t *testing.T) {
	two := 2
	cases := []struct {
		name   string
		r      *fakeRumble
		state  api.State
		writes int
	}{
		{"never set", &fakeRumble{value: 5}, api.State{}, 0},
		{"controller kept it", &fakeRumble{value: 2}, api.State{Rumble: &two}, 0},
		{"controller lost it", &fakeRumble{value: 5}, api.State{Rumble: &two}, 1},
		{"unreadable, write anyway", &fakeRumble{value: 5, readErr: true}, api.State{Rumble: &two}, 1},
		{"no controller", &fakeRumble{absent: true}, api.State{Rumble: &two}, 0},
	}
	for _, tc := range cases {
		d := rumbleDaemon(t, tc.r, tc.state)
		d.restoreRumble("test")
		if len(tc.r.writes) != tc.writes {
			t.Errorf("%s: %d writes, want %d", tc.name, len(tc.r.writes), tc.writes)
		}
	}
}

// device-get offers the control only while the controller answers, on the
// kernel's range; get-state reads the live value and omits an unreadable one.
func TestRumbleInTheDocumentAndState(t *testing.T) {
	present := deviceInfoFor(&device.Device{Rumble: &fakeRumble{value: 3}})
	if present.Rumble == nil || present.Rumble.Min != 0 || present.Rumble.Max != 5 {
		t.Errorf("device-get rumble = %+v, want 0–5", present.Rumble)
	}
	if absent := deviceInfoFor(&device.Device{Rumble: &fakeRumble{absent: true}}); absent.Rumble != nil {
		t.Errorf("device-get offered rumble with no controller: %+v", absent.Rumble)
	}
	d := rumbleDaemon(t, &fakeRumble{value: 3}, api.State{})
	if v := d.readRumble(); v == nil || *v != 3 {
		t.Errorf("readRumble = %v, want 3", v)
	}
	d = rumbleDaemon(t, &fakeRumble{readErr: true}, api.State{})
	if v := d.readRumble(); v != nil {
		t.Errorf("readRumble = %d on a failed read, want absent", *v)
	}
}
