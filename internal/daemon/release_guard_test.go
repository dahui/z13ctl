package daemon

// release_guard_test.go — keeps fan releases on the one path that re-applies the
// power limit afterwards, and platform_profile writes on the one path that tells
// the reconcile watcher the daemon made them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDirectFanRelease fails if the daemon or the CLI calls the raw
// driver.FanController.Release instead of device.Device.ReleaseFans.
//
// A release makes the firmware re-apply the platform profile's own power limits
// (issue #22), so a raw release silently discards whatever custom TDP was in
// force — while the ppt_* readback goes on reporting it. ReleaseFans(keep)
// re-applies the limit afterwards. The interface method cannot simply be removed,
// because safety.Engine needs it, so this scan is what keeps it out of reach; on
// main the old name was removed outright, which the compiler enforces instead.
func TestNoDirectFanRelease(t *testing.T) {
	for _, dir := range []string{".", "../../cmd"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no Go files under %s; the scan would pass vacuously", dir)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(src), "\n") {
				if strings.Contains(line, "Fans.Release()") && !strings.HasPrefix(strings.TrimSpace(line), "//") {
					t.Errorf("%s:%d calls Fans.Release() directly; use hw.ReleaseFans(keep) so the power limit survives the release:\n\t%s",
						f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestProfileWritesGoThroughTheRecorder fails if the daemon writes
// platform_profile anywhere but setPlatformProfile. An unrecorded write still
// works, but the reconcile watcher then logs the daemon's own switch as a
// warning that another tool rewrote the profile, which sends anyone reading
// the journal after the wrong culprit.
func TestProfileWritesGoThroughTheRecorder(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.Contains(line, "Profiles.Set(") || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			calls++
			if f != "profile.go" || !strings.Contains(line, "d.hw.Profiles.Set(name)") {
				t.Errorf("%s:%d writes platform_profile directly; use d.setPlatformProfile:\n\t%s",
					f, i+1, strings.TrimSpace(line))
			}
		}
	}
	if calls == 0 {
		t.Fatal("found no Profiles.Set call at all; the scan would pass vacuously")
	}
}
