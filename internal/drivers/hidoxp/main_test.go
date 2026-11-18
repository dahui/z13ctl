package hidoxp

import (
	"os"
	"testing"
)

// TestMain points the sysfs root nowhere before any test runs, so a test that
// forgets the fake fails with "no such file" instead of writing the machine's
// controller.
func TestMain(m *testing.M) {
	sysDriverDir = "/nonexistent/hid-oxp"
	os.Exit(m.Run())
}
