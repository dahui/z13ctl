package cli

// power_test.go — mains discovery and profile-name rules, against the fake
// sysfs tree. The decoy devices seeded by newFakeSysfs are the point of most of
// these cases: on a real Z13 the detachable keyboard and the USB-C ports both
// expose an "online" file, so anything that globs */online reports mains power
// whenever the cover is attached.

import (
	"os"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
)

func TestFindACOnlinePathIgnoresNonMainsSupplies(t *testing.T) {
	f := newFakeSysfs(t)

	got := FindACOnlinePath()
	want := f.ac + "/online"
	if got != want {
		t.Fatalf("FindACOnlinePath() = %q, want %q", got, want)
	}
	// Guard the premise: the decoys really are present and really do have an
	// online file, or this test passes for the wrong reason.
	for _, decoy := range []string{
		"hid-0018:04F3:43C7.0008-battery-7",
		"ucsi-source-psy-USBC000:001",
	} {
		p := f.root + "/power_supply/" + decoy + "/online"
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("decoy %s has no online file: %v", decoy, err)
		}
	}
}

func TestOnACPower(t *testing.T) {
	f := newFakeSysfs(t)

	f.setACOnline(t, true)
	on, err := OnACPower()
	if err != nil || !on {
		t.Errorf("OnACPower() with the adapter plugged = (%v, %v), want (true, nil)", on, err)
	}

	f.setACOnline(t, false)
	on, err = OnACPower()
	if err != nil || on {
		t.Errorf("OnACPower() with the adapter unplugged = (%v, %v), want (false, nil) — a decoy's online=1 leaked through", on, err)
	}
}

func TestOnACPowerAnyMainsOnlineWins(t *testing.T) {
	f := newFakeSysfs(t)
	f.setACOnline(t, false)
	// A dock registers as a second Mains supply.
	f.writeFile(t, f.root+"/power_supply/ADP1/type", "Mains")
	f.writeFile(t, f.root+"/power_supply/ADP1/online", "1")

	on, err := OnACPower()
	if err != nil || !on {
		t.Errorf("OnACPower() with a second adapter online = (%v, %v), want (true, nil)", on, err)
	}
}

// TestOnACPowerWithNoMainsSupply pins the contract the watcher depends on: no
// adapter is *unknown*, not "on battery". Returning false here would have the
// daemon apply the battery profile on a machine that has no battery.
func TestOnACPowerWithNoMainsSupply(t *testing.T) {
	f := newFakeSysfs(t)
	if err := os.RemoveAll(f.ac); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := OnACPower(); err == nil {
		t.Error("OnACPower() with no Mains supply = nil error, want an error so callers treat the source as unknown")
	}
}

func TestOnACPowerWithUnreadableOnline(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.ac+"/online", "not-a-number")

	if _, err := OnACPower(); err == nil {
		t.Error("OnACPower() with a garbage online value = nil error, want an error")
	}
}

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"plain", "gaming", false},
		{"digits and separators", "battery-uv_2", false},
		{"empty", "", true},
		{"uppercase", "Gaming", true},
		{"stock quiet", "quiet", true},
		{"stock balanced", "balanced", true},
		{"stock performance", "performance", true},
		{"reserved custom", "custom", true},
		{"stock name with different case", "Balanced", true},
		{"leading space", " gaming", true},
		{"trailing space", "gaming ", true},
		{"space inside", "my profile", true},
		{"punctuation", "gaming!", true},
		{"slash would escape the state map", "a/b", true},
		{"too long", strings.Repeat("a", maxProfileNameLen+1), true},
		{"at the length limit", strings.Repeat("a", maxProfileNameLen), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProfileName(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProfileName(%q) = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

// TestNoStockProfileNameIsAcceptedAsCustom is the invariant ReadEffectivePPT
// silently relies on: it treats any name absent from StockProfilePPT as custom
// and disables its stale-5W fallback, so a custom profile named "balanced"
// would make the machine misreport its power limits.
func TestNoStockProfileNameIsAcceptedAsCustom(t *testing.T) {
	for name := range StockProfilePPT {
		if !IsStockProfile(name) {
			t.Errorf("IsStockProfile(%q) = false, but it is a key in StockProfilePPT", name)
		}
		if err := ValidateProfileName(name); err == nil {
			t.Errorf("ValidateProfileName(%q) = nil, want an error — a custom profile may not shadow a firmware profile", name)
		}
		var s api.State
		if s.IsCustomProfile(name) {
			t.Errorf("State.IsCustomProfile(%q) = true, want false", name)
		}
	}
}

// TestIsCustomProfileIgnoresAShadowingMapEntry covers the hand-edited state
// file: even with a "balanced" entry in the profile map, the firmware profile
// must win.
func TestIsCustomProfileIgnoresAShadowingMapEntry(t *testing.T) {
	s := api.State{CustomProfiles: map[string]api.CustomProfile{
		"balanced": {Name: "balanced"},
		"gaming":   {Name: "gaming"},
	}}
	if s.IsCustomProfile("balanced") {
		t.Error("IsCustomProfile(\"balanced\") = true, want false even with a map entry of that name")
	}
	if !s.IsCustomProfile("gaming") {
		t.Error("IsCustomProfile(\"gaming\") = false, want true")
	}
	if !s.IsCustomProfile(api.DefaultCustomProfile) {
		t.Error("IsCustomProfile(\"custom\") = false, want true even with no map entry")
	}
	if s.IsCustomProfile("never-saved") {
		t.Error("IsCustomProfile(\"never-saved\") = true, want false")
	}
}

func TestCheckCurveAgainstTDP(t *testing.T) {
	below := []api.FanCurvePoint{{Temp: 30, PWM: HighTDPMinPWM - 1}, {Temp: 40, PWM: 255}}
	above := []api.FanCurvePoint{{Temp: 30, PWM: HighTDPMinPWM}, {Temp: 40, PWM: 255}}

	if err := CheckCurveAgainstTDP(below, TDPMaxSafe); err != nil {
		t.Errorf("CheckCurveAgainstTDP(below floor, %dW) = %v, want nil — the floor only applies above the safe max", TDPMaxSafe, err)
	}
	if err := CheckCurveAgainstTDP(below, TDPMaxSafe+1); err == nil {
		t.Errorf("CheckCurveAgainstTDP(below floor, %dW) = nil, want an error", TDPMaxSafe+1)
	}
	if err := CheckCurveAgainstTDP(above, TDPMaxSafe+1); err != nil {
		t.Errorf("CheckCurveAgainstTDP(at floor, %dW) = %v, want nil", TDPMaxSafe+1, err)
	}
	if err := CheckCurveAgainstTDP(nil, TDPMaxSafe+1); err != nil {
		t.Errorf("CheckCurveAgainstTDP(nil, %dW) = %v, want nil — a profile with no curve imposes no floor of its own", TDPMaxSafe+1, err)
	}
}
