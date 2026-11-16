package linux

// profile_sysfs_test.go — platform_profile discovery and writes, and the
// battery path glob, against the fake tree.

import (
	"github.com/dahui/voltaire/v2/internal/driver"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestFindProfilePathPrefersDeviceSupportingQuiet(t *testing.T) {
	f := newFakeSysfs(t)
	// A non-ASUS device (no "quiet") plus the ASUS one; ASUS must win.
	f.withProfileDevice(t, "platform-profile-1", "low-power balanced performance", "balanced")
	asus := f.withProfileDevice(t, "platform-profile-2", "quiet balanced performance", "balanced")

	if got, want := FindProfilePath(), asus+"/profile"; got != want {
		t.Errorf("FindProfilePath() = %q, want the quiet-capable device %q", got, want)
	}
}

func TestFindProfilePathFallsBackToACPI(t *testing.T) {
	newFakeSysfs(t)
	swap(t, &sysProfileDir, t.TempDir()+"/absent")
	if got := FindProfilePath(); got != sysProfileACPI {
		t.Errorf("FindProfilePath() = %q, want the ACPI fallback %q", got, sysProfileACPI)
	}
}

// TestSetProfileMapsQuietPerDevice covers the mapping that exists because
// amd-pmf advertises "low-power" where asus-wmi advertises "quiet".
func TestSetProfileMapsQuietPerDevice(t *testing.T) {
	f := newFakeSysfs(t)
	pmf := f.withProfileDevice(t, "platform-profile-1", "low-power balanced performance", "balanced")
	asus := f.withProfileDevice(t, "platform-profile-2", "quiet balanced performance", "balanced")

	if err := SetProfile("quiet"); err != nil {
		t.Fatalf("SetProfile(quiet) = %v, want nil", err)
	}
	if got := readTrimmed(t, asus+"/profile"); got != "quiet" {
		t.Errorf("asus device profile = %q, want \"quiet\"", got)
	}
	if got := readTrimmed(t, pmf+"/profile"); got != "low-power" {
		t.Errorf("amd-pmf device profile = %q, want \"low-power\" (it does not support \"quiet\")", got)
	}
	if got := *f.ppdCalls; len(got) != 1 || got[0] != "power-saver" {
		t.Errorf("powerprofilesctl calls = %v, want [power-saver]", got)
	}
}

func TestSetProfileWritesUniversalNamesUnmapped(t *testing.T) {
	f := newFakeSysfs(t)
	pmf := f.withProfileDevice(t, "platform-profile-1", "low-power balanced performance", "balanced")
	asus := f.withProfileDevice(t, "platform-profile-2", "quiet balanced performance", "balanced")

	if err := SetProfile("performance"); err != nil {
		t.Fatalf("SetProfile(performance) = %v, want nil", err)
	}
	for _, p := range []string{asus, pmf} {
		if got := readTrimmed(t, p+"/profile"); got != "performance" {
			t.Errorf("%s profile = %q, want \"performance\"", p, got)
		}
	}
}

func TestBatteryPathsGlobBATStar(t *testing.T) {
	f := newFakeSysfs(t)
	// Only BAT1 exists — the glob must find it rather than defaulting to BAT0.
	if err := os.MkdirAll(f.root+"/power_supply/BAT1", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.RemoveAll(f.battery); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	f.writeFile(t, f.root+"/power_supply/BAT1/charge_control_end_threshold", "80")
	f.writeFile(t, f.root+"/power_supply/BAT1/capacity", "55")

	if got, want := FindBatteryThresholdPath(), f.root+"/power_supply/BAT1/charge_control_end_threshold"; got != want {
		t.Errorf("FindBatteryThresholdPath() = %q, want %q", got, want)
	}
	if got, want := FindBatteryCapacityPath(), f.root+"/power_supply/BAT1/capacity"; got != want {
		t.Errorf("FindBatteryCapacityPath() = %q, want %q", got, want)
	}
}

// TestSetProfileWritesPrimaryWhenNoClassDevice is the regression guard for a
// silent no-op: when no device under sysProfileDir exposes a profile file,
// FindProfilePath falls back to the ACPI alias, which the write loop never
// visits. SetProfile then returned nil having written nothing at all — and
// still told power-profiles-daemon the switch had happened.
func TestSetProfileWritesPrimaryWhenNoClassDevice(t *testing.T) {
	f := newFakeSysfs(t)

	// An empty platform-profile directory forces the ACPI-alias fallback.
	acpi := f.root + "/acpi_platform_profile"
	swap(t, &sysProfileDir, f.root+"/empty-platform-profile")
	if err := os.MkdirAll(f.root+"/empty-platform-profile", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f.writeFile(t, acpi, "balanced")

	if err := SetProfile("performance"); err != nil {
		t.Fatalf("SetProfile() = %v, want nil", err)
	}

	data, err := os.ReadFile(acpi)
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", acpi, err)
	}
	if got := strings.TrimSpace(string(data)); got != "performance" {
		t.Errorf("ACPI alias = %q, want %q — SetProfile reported success without writing", got, "performance")
	}
}

// TestSetProfileReportsPrimaryWriteFailure: with nothing writable, the caller
// must see an error rather than a false success.
func TestSetProfileReportsPrimaryWriteFailure(t *testing.T) {
	f := newFakeSysfs(t)

	swap(t, &sysProfileDir, f.root+"/missing-platform-profile")
	swap(t, &sysProfileACPI, f.root+"/missing-dir/platform_profile")

	if err := SetProfile("quiet"); err == nil {
		t.Error("SetProfile() = nil, want an error when no profile file can be written")
	}
	if calls := *f.ppdCalls; len(calls) != 0 {
		t.Errorf("powerprofilesctl was called %v after a failed write, want no calls", calls)
	}
}

// TestProfileControllerNamesThePolicyAttributes: both notifying attributes, so
// the daemon sees fan releases (throttle_thermal_policy) as well as profile
// writes.
func TestProfileControllerNamesThePolicyAttributes(t *testing.T) {
	newFakeSysfs(t)
	// A vendor attribute the registry passes in (asus-wmi's
	// throttle_thermal_policy on the Z13 — internal/drivers/register pins the
	// real pair) is watched after the generic ACPI one.
	const vendor = "/sys/devices/platform/vendor-wmi/policy"
	n, ok := NewProfileController(nil, "", "", nil, vendor).(driver.PolicyWriteNotifier)
	if !ok {
		t.Fatal("the platform-profile driver does not implement driver.PolicyWriteNotifier")
	}
	paths := n.PolicyWritePaths()
	want := []string{sysProfileACPI, vendor}
	if !slices.Equal(paths, want) {
		t.Errorf("PolicyWritePaths() = %q, want %q", paths, want)
	}
	// Without a vendor attribute only the generic ACPI one is watched.
	bare := NewProfileController(nil, "", "", nil).(driver.PolicyWriteNotifier).PolicyWritePaths()
	if !slices.Equal(bare, []string{sysProfileACPI}) {
		t.Errorf("PolicyWritePaths() with no extras = %q, want only %q", bare, sysProfileACPI)
	}
}
