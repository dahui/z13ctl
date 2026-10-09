package daemon

// undervolt_gate_test.go — the one rule protecting the SMU mailbox: never send
// a Curve Optimizer reset for an offset that was never applied.
//
// cli.SMUProbeUndervolt() answers "does this machine support CO at all", which
// is true on every Z13 with ryzen_smu loaded. Gating a reset on it meant every
// route to a stock profile wrote the MP1 mailbox to clear something that was
// never set. One of those hard-locked this SoC on 2026-08-14 with no offset
// saved or active anywhere. uvApplied() is the gate; these tests are what keep
// an eighth call site from quietly going back to the wrong one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
)

func TestUndervoltActiveMirrorsSetUndervoltActive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		s    api.State
		want bool
	}{
		{"no profiles at all", api.State{}, false},
		{
			"a profile with no undervolt",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming"},
			}},
			false,
		},
		{
			// The case that froze the machine: an offset is saved but has never
			// been written to hardware, so there is nothing to clear.
			"saved but not active",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20}},
			}},
			false,
		},
		{
			"applied",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20, Active: true}},
			}},
			true,
		},
		{
			// CO is global hardware, so at most one profile's offset can be
			// applied — but the answer is "any", not "the active profile's".
			"one of several applied",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"quietish": {Name: "quietish", Undervolt: &api.UndervoltState{CPUCO: -5}},
				"gaming":   {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20, Active: true}},
			}},
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := undervoltActive(tc.s); got != tc.want {
				t.Errorf("undervoltActive = %v, want %v", got, tc.want)
			}
		})
	}
}

// setUndervoltActive and undervoltActive are the write and the read of one
// fact. Round-tripping is what stops them drifting apart — a getter that
// consulted a different field would keep passing its own table forever.
func TestUndervoltActiveRoundTrips(t *testing.T) {
	t.Parallel()

	s := api.State{CustomProfiles: map[string]api.CustomProfile{
		"a": {Name: "a", Undervolt: &api.UndervoltState{CPUCO: -10}},
		"b": {Name: "b", Undervolt: &api.UndervoltState{CPUCO: -20}},
	}}
	if undervoltActive(s) {
		t.Fatal("fresh profiles must not report an applied offset")
	}
	setUndervoltActive(s, true)
	if !undervoltActive(s) {
		t.Error("after setUndervoltActive(true) the state must read as applied")
	}
	setUndervoltActive(s, false)
	if undervoltActive(s) {
		t.Error("after setUndervoltActive(false) the state must not read as applied")
	}
}

// TestUvAppliedSkipsTheProbeWhenNothingIsApplied pins the order inside
// uvApplied: state first. cli.SMUProbeUndervolt sends the same payload as a
// reset the first time it runs, so asking it on a machine with nothing applied
// would be the speculative write by another route. Not parallel: it swaps the
// package-level probe, and the stub is what keeps a regression here from
// writing the SMU of the machine running the tests.
func TestUvAppliedSkipsTheProbeWhenNothingIsApplied(t *testing.T) {
	probed := false
	orig := uvProbe
	uvProbe = func() bool { probed = true; return true }
	t.Cleanup(func() { uvProbe = orig })

	d := &Daemon{state: api.State{CustomProfiles: map[string]api.CustomProfile{
		"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20}},
	}}}
	if d.uvApplied() || probed {
		t.Errorf("nothing applied: uvApplied probed=%v, want false without probing", probed)
	}

	setUndervoltActive(d.state, true)
	if !d.uvApplied() || !probed {
		t.Errorf("offset applied: uvApplied probed=%v, want true after probing", probed)
	}
}

// TestEveryUndervoltResetIsGatedOnApplied is a source check: the alternative is
// a live SMU write, and the failure it guards is a machine that stops
// responding rather than a test that goes red.
//
// It looks backwards from each cli.ResetCurveOptimizer() call for the nearest
// enclosing `if` or `case`, and requires that condition to be the
// applied-offset check. Comments are stripped first — several of these call
// sites *mention* SMUProbeUndervolt in prose, so a check that read comments
// could pass on the documentation alone. The nearest condition rather than a
// window of lines, because applyCustomHW's apply branch legitimately probes a
// few lines above its reset. The negative control for this test is to change
// any one site back to cli.SMUProbeUndervolt() and confirm it fails.
func TestEveryUndervoltResetIsGatedOnApplied(t *testing.T) {
	t.Parallel()

	const window = 8 // lines to look back for the enclosing guard

	// Both packages: cmd/ holds the two no-daemon resets, taken whenever the
	// daemon is down. A guard scoped to one package certifies the package, not
	// the rule.
	dirs := []string{".", "../../cmd"}

	var files []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			files = append(files, filepath.Join(dir, name))
		}
	}

	found := 0
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		var code []string
		for _, line := range strings.Split(string(raw), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			code = append(code, line)
		}

		for i, line := range code {
			if !strings.Contains(line, "cli.ResetCurveOptimizer()") {
				continue
			}
			found++
			guard := ""
			for j := i - 1; j >= 0 && j >= i-window; j-- {
				trimmed := strings.TrimSpace(code[j])
				if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "} else if ") ||
					strings.HasPrefix(trimmed, "case ") {
					guard = trimmed
					break
				}
			}
			// uvApplied inside the daemon, daemon.UndervoltApplied from cmd/ —
			// the same predicate over the same state, by necessity: the CLI has
			// no Daemon to ask and must not answer differently.
			if !strings.Contains(guard, "uvApplied()") && !strings.Contains(guard, "UndervoltApplied()") {
				t.Errorf("%s:%d — cli.ResetCurveOptimizer() is not gated on an applied-offset check "+
					"(nearest condition: %q); an offset that was never applied must never be cleared",
					name, i+1, guard)
			}
		}
	}

	// Guard the premise: if a refactor renames the call, this test would pass by
	// finding nothing at all. Five in internal/daemon plus two in cmd/.
	if found < 7 {
		t.Fatalf("found %d cli.ResetCurveOptimizer() call sites, expected at least 7 — "+
			"this guard needs updating with the rename", found)
	}
}

// TestOnlyWritersProbeTheSMU: cli.SMUProbeUndervolt's first run in a process is
// a CO reset, so it may be called only where an offset is about to be written
// anyway. Anything that merely asks — get-state's undervolt_available,
// undervolt-get, an undervolt-reset with nothing applied — uses
// cli.SMUUndervoltAvailable, which never writes. get-state probing was how a GUI
// poll sent the speculative MP1 write after every daemon start. uvApplied
// reaches the probe through the uvProbe var, and only with an offset applied.
func TestOnlyWritersProbeTheSMU(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"func (d *Daemon) handleUndervolt(": true, // writes the requested offset
		"func (d *Daemon) applyCustomHW(":   true, // writes the profile's offset
		"func (d *Daemon) reconcileOnce(":   true, // re-applies the offset after a lost resume
	}

	for _, dir := range []string{".", "../../cmd"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			raw, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			fn := ""
			for i, line := range strings.Split(string(raw), "\n") {
				if j := strings.Index(line, "//"); j >= 0 {
					line = line[:j]
				}
				if strings.HasPrefix(line, "func ") {
					fn = line
				}
				if !strings.Contains(line, "SMUProbeUndervolt()") {
					continue
				}
				ok := false
				for prefix := range allowed {
					if strings.HasPrefix(fn, prefix) {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s:%d — %q calls cli.SMUProbeUndervolt(), which writes a CO reset on its "+
						"first run; a caller that is only asking must use cli.SMUUndervoltAvailable()",
						path, i+1, fn)
				}
			}
		}
	}
}
