package daemon

// clone_test.go — regression coverage for the state snapshot that handlers hand
// to saveState after releasing d.mu.

import (
	"bufio"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/dahui/z13ctl/api"
)

func sampleState() api.State {
	return api.State{
		Lighting: api.LightingState{Mode: "static", Color: "FF0000", Brightness: 3},
		Devices: map[string]api.LightingState{
			"keyboard": {Mode: "breathe", Brightness: 2},
			"lightbar": {Mode: "static", Brightness: 1},
		},
		Profile:   "custom",
		TDP:       &api.TDPState{PL1SPL: 15, PL2SPPT: 20, FPPT: 25, APUSPPT: 20, PlatformSPPT: 20},
		Undervolt: &api.UndervoltState{CPUCO: -20, Active: true},
		FanCurve: &api.FanCurveState{
			Mode:   1,
			Points: []api.FanCurvePoint{{Temp: 40, PWM: 50}, {Temp: 50, PWM: 80}},
		},
	}
}

func TestCloneStateSharesNoMutableMemory(t *testing.T) {
	orig := sampleState()
	c := cloneState(orig)

	// Mutating the clone must not touch the original.
	c.Devices["keyboard"] = api.LightingState{Mode: "rainbow"}
	c.Devices["new"] = api.LightingState{}
	c.TDP.PL1SPL = 99
	c.Undervolt.Active = false
	c.FanCurve.Points[0].PWM = 255

	if got := orig.Devices["keyboard"].Mode; got != "breathe" {
		t.Errorf("original Devices[keyboard].Mode = %q, want \"breathe\" — the map is shared", got)
	}
	if _, ok := orig.Devices["new"]; ok {
		t.Error("a key added to the clone appeared in the original — the map is shared")
	}
	if orig.TDP.PL1SPL != 15 {
		t.Errorf("original TDP.PL1SPL = %d, want 15 — the pointer is shared", orig.TDP.PL1SPL)
	}
	if !orig.Undervolt.Active {
		t.Error("original Undervolt.Active was cleared — the pointer is shared")
	}
	if orig.FanCurve.Points[0].PWM != 50 {
		t.Errorf("original FanCurve.Points[0].PWM = %d, want 50 — the slice is shared", orig.FanCurve.Points[0].PWM)
	}
}

func TestCloneStatePreservesValues(t *testing.T) {
	orig := sampleState()
	c := cloneState(orig)

	if c.Profile != orig.Profile || c.Lighting != orig.Lighting {
		t.Errorf("clone scalars = %+v, want %+v", c, orig)
	}
	if len(c.Devices) != len(orig.Devices) {
		t.Errorf("clone has %d devices, want %d", len(c.Devices), len(orig.Devices))
	}
	for k, v := range orig.Devices {
		if c.Devices[k] != v {
			t.Errorf("clone Devices[%s] = %+v, want %+v", k, c.Devices[k], v)
		}
	}
	if *c.TDP != *orig.TDP {
		t.Errorf("clone TDP = %+v, want %+v", *c.TDP, *orig.TDP)
	}
	if *c.Undervolt != *orig.Undervolt {
		t.Errorf("clone Undervolt = %+v, want %+v", *c.Undervolt, *orig.Undervolt)
	}
	if c.FanCurve.Mode != orig.FanCurve.Mode || len(c.FanCurve.Points) != len(orig.FanCurve.Points) {
		t.Errorf("clone FanCurve = %+v, want %+v", *c.FanCurve, *orig.FanCurve)
	}
}

func TestCloneStateHandlesNilFields(t *testing.T) {
	c := cloneState(api.State{Profile: "balanced"})
	if c.Devices != nil || c.TDP != nil || c.Undervolt != nil || c.FanCurve != nil {
		t.Errorf("cloneState() populated nil fields: %+v", c)
	}
	if c.Profile != "balanced" {
		t.Errorf("Profile = %q, want \"balanced\"", c.Profile)
	}
}

// TestSaveStateSnapshotIsRaceFree is the regression guard for the crash this
// fixes: handlers release d.mu before calling saveState, so marshaling a
// snapshot that still aliases d.state races with any handler mutating it. A
// concurrent map read/write is a fatal runtime error, not a recoverable panic.
// Run under -race to detect a regression.
func TestSaveStateSnapshotIsRaceFree(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	d := &Daemon{state: sampleState()}

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(2)
		// Reader: the snapshot-then-save pattern used by every handler.
		go func() {
			defer wg.Done()
			for range 300 {
				d.mu.Lock()
				s := cloneState(d.state)
				d.mu.Unlock()
				_ = saveState(s)
			}
		}()
		// Writer: a handler mutating shared state under the lock.
		go func() {
			defer wg.Done()
			for i := range 300 {
				d.mu.Lock()
				d.state.Devices["lightbar"] = api.LightingState{Brightness: i % 4}
				d.state.Undervolt.Active = i%2 == 0
				d.state.FanCurve.Points[0].PWM = i % 256
				d.mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

// TestBroadcastEventShape pins the wire format of a streamed event. `ok` has no
// omitempty, so a response built without it ships {"ok":false,...} — and the
// documented protocol tells clients every response carries ok, so a client that
// checks it before dispatching would discard every button press.
func TestBroadcastEventShape(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	d := &Daemon{}
	d.addSubscriber(server)

	go d.broadcast(response{OK: true, Event: "gui-toggle"})

	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		t.Fatalf("reading broadcast: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("broadcast is not valid JSON: %v (%q)", err, line)
	}
	if got["event"] != "gui-toggle" {
		t.Errorf("event = %v, want \"gui-toggle\"", got["event"])
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true — a client honouring the documented contract would drop this event", got["ok"])
	}
}

// TestBroadcastPrunesDeadSubscribers covers the write-failure path: a closed
// subscriber must be dropped rather than retried forever.
func TestBroadcastPrunesDeadSubscribers(t *testing.T) {
	live, liveServer := net.Pipe()
	defer func() { _ = live.Close() }()
	dead, deadServer := net.Pipe()
	_ = dead.Close()
	_ = deadServer.Close()

	d := &Daemon{}
	d.addSubscriber(deadServer)
	d.addSubscriber(liveServer)

	done := make(chan struct{})
	go func() { defer close(done); d.broadcast(response{OK: true, Event: "gui-toggle"}) }()

	_ = live.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := bufio.NewReader(live).ReadBytes('\n'); err != nil {
		t.Fatalf("live subscriber did not receive the event: %v", err)
	}
	<-done

	d.subMu.Lock()
	n := len(d.subscribers)
	d.subMu.Unlock()
	if n != 1 {
		t.Errorf("subscribers after broadcast = %d, want 1 (dead one not pruned)", n)
	}
}
