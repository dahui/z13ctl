package cmd

// tdp_test.go — the high-TDP floor notice describes the device's own floor
// curve, never one machine's numbers. No hardware: printFloorNotice takes the
// envelope and the PWM ceiling as values.
//
// Not parallel: it redirects os.Stdout.

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = orig
	out, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestFloorNoticeReadsTheDevicesCurve(t *testing.T) {
	// A floor unlike the Z13's: tops out at 230 by 85°C, on a 255 ceiling.
	env := driver.PowerEnvelope{
		TDPMaxSafe: 40,
		FloorCurve: []api.FanCurvePoint{{Temp: 40, PWM: 100}, {Temp: 85, PWM: 230}},
	}
	tests := []struct {
		name string
		want []api.FanCurvePoint
	}{
		{"no curve in force", nil},
		{"a curve the floor raises", []api.FanCurvePoint{
			{Temp: 30, PWM: 0}, {Temp: 50, PWM: 0}, {Temp: 90, PWM: 255},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() { printFloorNotice(env, 255, 50, tt.want, false) })
			if !strings.Contains(out, "100 PWM (39%) floor rising to 230 PWM (90%) at 85°C") {
				t.Errorf("notice does not describe the device's floor:\n%s", out)
			}
			for _, z13 := range []string{"80°C", "100%", "50%"} {
				if strings.Contains(out, z13) {
					t.Errorf("notice states the Z13's %q:\n%s", z13, out)
				}
			}
		})
	}

	if out := captureStdout(t, func() { printFloorNotice(env, 255, 40, nil, false) }); out != "" {
		t.Errorf("at the safe maximum the notice should be silent, got:\n%s", out)
	}
	flat := driver.PowerEnvelope{TDPMaxSafe: 40, FloorCurve: []api.FanCurvePoint{{Temp: 0, PWM: 127}}}
	if out := captureStdout(t, func() { printFloorNotice(flat, 255, 50, nil, false) }); !strings.Contains(out, "a flat 127 PWM (50%) floor") {
		t.Errorf("a flat floor should be described as flat, got:\n%s", out)
	}
}
