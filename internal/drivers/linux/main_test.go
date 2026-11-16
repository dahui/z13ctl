package linux

// main_test.go — no test in this package may reach the machine's sysfs.

import (
	"os"
	"testing"
)

// TestMain points every sysfs root this package writes under at a directory
// that does not exist, before any test runs. A test that forgets newFakeSysfs
// then fails with "no such file" instead of switching the developer's power
// profile, charge limit or CPU boost — the same guard the Z13 driver package
// carried since the asus-armoury port, split with the package.
func TestMain(m *testing.M) {
	const nowhere = "/nonexistent/voltaire-test-sysfs"
	for _, v := range []*string{
		&sysProfileDir, &sysProfileACPI, &sysPowerSupplyDir, &sysPowercapDir, &sysCPUBoostPath,
	} {
		*v = nowhere + *v
	}
	os.Exit(m.Run())
}
