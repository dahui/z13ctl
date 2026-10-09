package asusz13

// main_test.go — no test in this package may reach the machine's sysfs.

import (
	"os"
	"testing"
)

// TestMain points every sysfs root this package writes under at a directory
// that does not exist, before any test runs. A test that forgets newFakeSysfs
// then fails with "no such file" instead of touching the developer's machine.
//
// Added after the asus-armoury port: tdp_test.go's helper swapped only
// pptBasePath, so three tests found the real armoury PPT attributes and tried
// to write power limits to them. The attributes' 0644 root:root permissions
// were all that stopped it — z13ctl's identical helper did the same thing during
// its own migration.
func TestMain(m *testing.M) {
	const nowhere = "/nonexistent/voltaire-test-sysfs"
	for _, v := range []*string{
		&sysHwmonDir, &sysProfileDir, &sysProfileACPI, &sysPowerSupplyDir,
		&sysPowercapDir, &sysFirmwareAttrDir, &pptBasePath, &smuDriverPath,
		&sysCPUBoostPath,
	} {
		*v = nowhere + *v
	}
	os.Exit(m.Run())
}
