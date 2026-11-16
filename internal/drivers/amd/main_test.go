package amd

// main_test.go — no test in this package may reach the machine's sysfs.

import (
	"os"
	"testing"
)

// TestMain points every root this package writes under (and the hwmon root it
// reads k10temp from) at a directory that does not exist, before any test
// runs. A test that forgets newFakeSysfs then fails with "no such file"
// instead of reaching the developer's machine — which for smuDriverPath means
// the SMU mailbox, whose first write in a process is a Curve Optimizer reset
// (see SMUProbeUndervolt). The same guard the Z13 driver package has carried
// since the asus-armoury port, split with the package.
func TestMain(m *testing.M) {
	const nowhere = "/nonexistent/voltaire-test-sysfs"
	for _, v := range []*string{&smuDriverPath, &sysHwmonDir} {
		*v = nowhere + *v
	}
	os.Exit(m.Run())
}
