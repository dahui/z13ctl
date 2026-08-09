package asusz13

// power_test.go — mains discovery against the fake sysfs tree. The decoy
// devices seeded by newFakeSysfs are the point of most of these cases: on a
// real Z13 the detachable keyboard and the USB-C ports both expose an "online"
// file, so anything that globs */online reports mains power whenever the cover
// is attached. The profile-name rules that used to live here moved to
// internal/cli with ValidateProfileName.

import (
	"os"
	"testing"
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
