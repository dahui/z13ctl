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
	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/cli"
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
		Autoswitch: &api.AutoswitchState{Enabled: true, AC: "balanced", Battery: "battery-uv"},
		CustomProfiles: map[string]api.CustomProfile{
			"custom": {
				Name:      "custom",
				TDP:       &api.TDPState{PL1SPL: 15, PL2SPPT: 20, FPPT: 25, APUSPPT: 20, PlatformSPPT: 20},
				Undervolt: &api.UndervoltState{CPUCO: -20, Active: true},
				FanCurve: &api.FanCurveState{
					Mode:   1,
					Points: []api.FanCurvePoint{{Temp: 40, PWM: 50}, {Temp: 50, PWM: 80}},
				},
			},
			"battery-uv": {
				Name:      "battery-uv",
				TDP:       &api.TDPState{PL1SPL: 35, PL2SPPT: 40, FPPT: 45},
				Undervolt: &api.UndervoltState{CPUCO: -25},
			},
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

// TestCloneStateDeepCopiesCustomProfiles is the guard for the riskiest part of
// named profiles: a new map header is not enough, because each entry carries
// three pointers and a slice. A shallow entry copy reproduces exactly the
// concurrent map/pointer mutation that the Go runtime turns into an
// unrecoverable crash rather than a catchable panic.
func TestCloneStateDeepCopiesCustomProfiles(t *testing.T) {
	orig := sampleState()
	c := cloneState(orig)

	c.CustomProfiles["custom"].TDP.PL1SPL = 99
	c.CustomProfiles["custom"].FanCurve.Points[0].PWM = 255
	c.CustomProfiles["battery-uv"].Undervolt.CPUCO = -1
	c.CustomProfiles["added"] = api.CustomProfile{Name: "added"}
	c.Autoswitch.Battery = "something-else"

	if orig.CustomProfiles["custom"].TDP.PL1SPL != 15 {
		t.Errorf("original profile TDP = %d, want 15 — the entry's pointer is shared",
			orig.CustomProfiles["custom"].TDP.PL1SPL)
	}
	if orig.CustomProfiles["custom"].FanCurve.Points[0].PWM != 50 {
		t.Errorf("original profile curve point = %d, want 50 — the entry's slice is shared",
			orig.CustomProfiles["custom"].FanCurve.Points[0].PWM)
	}
	if orig.CustomProfiles["battery-uv"].Undervolt.CPUCO != -25 {
		t.Errorf("original profile CO = %d, want -25 — the entry's pointer is shared",
			orig.CustomProfiles["battery-uv"].Undervolt.CPUCO)
	}
	if _, ok := orig.CustomProfiles["added"]; ok {
		t.Error("a key added to the clone appeared in the original — the map is shared")
	}
	if orig.Autoswitch.Battery != "battery-uv" {
		t.Errorf("original Autoswitch.Battery = %q, want \"battery-uv\" — the pointer is shared", orig.Autoswitch.Battery)
	}
}

// TestWithLegacyProjection covers the compatibility shim: CustomProfiles is the
// truth, and the top-level fields are filled in from the active profile only at
// the serialization boundaries so clients written before named profiles existed
// keep seeing the settings that are in force.
func TestWithLegacyProjection(t *testing.T) {
	s := api.State{
		Profile: "battery-uv",
		CustomProfiles: map[string]api.CustomProfile{
			"battery-uv": {Name: "battery-uv", TDP: &api.TDPState{PL1SPL: 35}},
		},
	}
	got := withLegacyProjection(s)
	if got.TDP == nil || got.TDP.PL1SPL != 35 {
		t.Errorf("projected TDP = %+v, want the active profile's 35W", got.TDP)
	}
	if got.FanCurve != nil || got.Undervolt != nil {
		t.Error("projected fields the active profile does not set")
	}

	// On a firmware profile the projection falls back to "custom" — what a bare
	// "undervolt --set" edits and "profile --set custom" recalls. Projecting
	// nothing would lose the saved-but-inactive undervolt a GUI displays, and
	// would write those settings away on a downgrade taken while on balanced.
	s.Profile = "balanced"
	s.TDP = &api.TDPState{PL1SPL: 99} // a stale projection from an earlier active profile
	s.CustomProfiles[api.DefaultCustomProfile] = api.CustomProfile{
		Name:      api.DefaultCustomProfile,
		Undervolt: &api.UndervoltState{CPUCO: -20},
	}
	got = withLegacyProjection(s)
	if got.TDP != nil {
		t.Errorf("projected TDP = %+v, want nil — \"custom\" holds no TDP", got.TDP)
	}
	if got.Undervolt == nil || got.Undervolt.CPUCO != -20 {
		t.Errorf("projected undervolt = %+v, want the saved -20 offset", got.Undervolt)
	}
	if got.Undervolt != nil && got.Undervolt.Active {
		t.Error("the projection reads as applied while a firmware profile is active")
	}

	// Nothing saved at all: the fields stay nil rather than becoming zero values.
	got = withLegacyProjection(api.State{Profile: "balanced"})
	if got.FanCurve != nil || got.TDP != nil || got.Undervolt != nil {
		t.Errorf("projection invented fields with nothing saved: %+v", got)
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
	if c.CustomProfiles != nil || c.Autoswitch != nil {
		t.Errorf("cloneState() populated nil profile fields: %+v", c)
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
				// The profile map is mutated by every setting handler and by the
				// autoswitch watcher, through both the map header and the
				// pointers inside each entry.
				d.state.CustomProfiles["custom"].TDP.PL1SPL = 10 + i%40
				d.state.CustomProfiles["custom"].FanCurve.Points[0].PWM = i % 256
				setUndervoltActive(d.state, i%2 == 0)
				d.state.Autoswitch.Battery = "battery-uv"
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
	d.addSubscriber(server, nil)

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
	d.addSubscriber(deadServer, nil)
	d.addSubscriber(liveServer, nil)

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

// TestNormalizeLightingState covers the repair applied to partially-populated
// per-device entries. handleOff stores {Enabled: false} for a named zone, so a
// later brightness command on that zone produced an enabled state with no mode,
// colour or speed — which ModeFromString rejects, making every subsequent
// restore fail.
func TestNormalizeLightingState(t *testing.T) {
	fallback := api.LightingState{Mode: "cycle", Color: "00FF00", Color2: "0000FF", Speed: "fast"}
	def := defaultState().Lighting

	tests := []struct {
		name     string
		in       api.LightingState
		fallback api.LightingState
		want     api.LightingState
	}{
		{
			name:     "empty entry takes every field from the fallback",
			in:       api.LightingState{Enabled: true, Brightness: 2},
			fallback: fallback,
			want: api.LightingState{
				Enabled: true, Brightness: 2,
				Mode: "cycle", Color: "00FF00", Color2: "0000FF", Speed: "fast",
			},
		},
		{
			name:     "populated fields are preserved",
			in:       api.LightingState{Enabled: true, Mode: "strobe", Color: "FF0000", Color2: "111111", Speed: "slow"},
			fallback: fallback,
			want:     api.LightingState{Enabled: true, Mode: "strobe", Color: "FF0000", Color2: "111111", Speed: "slow"},
		},
		{
			name:     "empty fallback falls through to the defaults",
			in:       api.LightingState{Enabled: true},
			fallback: api.LightingState{},
			want: api.LightingState{
				Enabled: true,
				Mode:    def.Mode, Color: def.Color, Color2: def.Color2, Speed: def.Speed,
			},
		},
		{
			name:     "partial entry only fills the gaps",
			in:       api.LightingState{Enabled: true, Mode: "breathe"},
			fallback: fallback,
			want: api.LightingState{
				Enabled: true, Mode: "breathe",
				Color: "00FF00", Color2: "0000FF", Speed: "fast",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLightingState(tt.in, tt.fallback); got != tt.want {
				t.Errorf("normalizeLightingState() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestNormalizedStateIsAppliable is the regression guard proper: whatever
// normalizeLightingState returns must survive the parsing applyZone does, or
// lighting restore fails for every zone on every daemon start, resume, and
// keyboard hotplug.
func TestNormalizedStateIsAppliable(t *testing.T) {
	// The exact state the off-then-brightness sequence used to persist.
	broken := api.LightingState{Enabled: true, Brightness: 2}

	ls := normalizeLightingState(broken, defaultState().Lighting)

	if _, err := aura.ModeFromString(ls.Mode); err != nil {
		t.Errorf("ModeFromString(%q) = %v, want nil", ls.Mode, err)
	}
	if _, err := aura.SpeedFromString(ls.Speed); err != nil {
		t.Errorf("SpeedFromString(%q) = %v, want nil", ls.Speed, err)
	}
	if _, _, _, err := cli.ParseColor(ls.Color); err != nil {
		t.Errorf("ParseColor(%q) = %v, want nil", ls.Color, err)
	}
	if _, _, _, err := cli.ParseColor(ls.Color2); err != nil {
		t.Errorf("ParseColor(%q) = %v, want nil", ls.Color2, err)
	}
}
