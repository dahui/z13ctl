package cli

// main_test.go — the package-wide guard that keeps tests off the real machine.

import (
	"os"
	"testing"
)

// TestMain points every sysfs root at a directory that does not exist before
// any test runs, so a test that forgets newFakeSysfs (or a helper that swaps
// only some of the roots) fails with "no such file" instead of reading — or
// writing — the developer's hardware. Each fake still swaps in its own tree.
//
// This was not hypothetical. When the PPT writes moved to asus-armoury, a helper
// that redirected only the asus-nb-wmi path let two tests find the real armoury
// attributes and write power limits to them; only the attribute's permissions
// stopped it. The ryzen_smu mailbox is under the same guard, and there a stray
// write is a CO reset.
func TestMain(m *testing.M) {
	const nowhere = "/nonexistent/z13ctl-test-sysfs"
	sysHwmonDir = nowhere + "/hwmon"
	sysProfileDir = nowhere + "/platform-profile"
	sysProfileACPI = nowhere + "/acpi_platform_profile"
	sysPowerSupplyDir = nowhere + "/power_supply"
	sysFirmwareAttrDir = nowhere + "/firmware-attributes"
	pptBasePath = nowhere + "/asus-nb-wmi"
	smuDriverPath = nowhere + "/ryzen_smu_drv"
	os.Exit(m.Run())
}
