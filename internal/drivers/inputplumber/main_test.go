package inputplumber

// main_test.go — no test in this package may reach the machine's InputPlumber
// configuration.

import (
	"os"
	"testing"
)

// TestMain points every directory this package reads or writes at one that
// does not exist, before any test runs, so a test that forgets its temporary
// tree fails instead of writing an override into /etc/inputplumber.
func TestMain(m *testing.M) {
	const nowhere = "/nonexistent/voltaire-test-inputplumber"
	OverrideDir = nowhere + OverrideDir
	DMIDir = nowhere + DMIDir
	for i := range DataDirs {
		DataDirs[i] = nowhere + DataDirs[i]
	}
	os.Exit(m.Run())
}
