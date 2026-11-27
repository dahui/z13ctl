package daemon

// release_guard_test.go — keeps platform_profile writes on the one path that
// tells the reconcile watcher the daemon made them. (Fan releases need no scan:
// device.Device.Fans is a driver.FanCurves, which has no Release, so a direct
// release does not compile; only Device.ReleaseFans and the engine reach it.)

import "testing"

// TestProfileWritesGoThroughTheRecorder fails if the daemon writes
// platform_profile anywhere but setPlatformProfile. An unrecorded write still
// works, but the reconcile watcher then logs the daemon's own switch as a
// warning that another tool rewrote the profile, which sends anyone reading
// the journal after the wrong culprit.
func TestProfileWritesGoThroughTheRecorder(t *testing.T) {
	sites := findCalls(t, []string{"."}, "Profiles.Set")
	if len(sites) == 0 {
		t.Fatal("found no Profiles.Set call at all; the scan would pass vacuously")
	}
	for _, c := range sites {
		if c.fnName() != "Daemon.setPlatformProfile" {
			t.Errorf("%s: %s writes platform_profile directly; use d.setPlatformProfile", c.pos, c.fnName())
		}
	}
}
