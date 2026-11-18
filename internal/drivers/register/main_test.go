package register

import (
	"os"
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/device"
)

// TestMain answers "is ryzen_smu loaded" with yes for every test here, so the
// device files assemble the same on a machine without the module (CI) as on
// one with it. TestSMUMissingMakesUndervoltUnavailable checks the other answer.
func TestMain(m *testing.M) {
	smuMissing = func(string) string { return "" }
	os.Exit(m.Run())
}

// TestSMUMissingMakesUndervoltUnavailable: without ryzen_smu the Z13 assembles
// with no undervolt capability and the reason recorded, not a section every
// client would then have to hide.
func TestSMUMissingMakesUndervoltUnavailable(t *testing.T) {
	prev := smuMissing
	smuMissing = func(what string) string { return what + " needs the ryzen_smu kernel module" }
	t.Cleanup(func() { smuMissing = prev })

	d, err := device.Assemble(z13Config(t))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if d.Undervolt != nil {
		t.Error("undervolt assembled without ryzen_smu")
	}
	if got := d.Missing("undervolt", ""); !strings.HasPrefix(got, "Undervolting needs") {
		t.Errorf("Missing(undervolt) = %q, want the undervolt reason", got)
	}
	if d.Power == nil {
		t.Error("the Z13's power control is asus firmware, not the SMU, and must not depend on the module")
	}
}
