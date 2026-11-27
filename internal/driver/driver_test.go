package driver_test

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

func TestFanModeName(t *testing.T) {
	tests := []struct {
		mode driver.FanMode
		want string
	}{
		{0, "full-speed"},
		{1, "custom"},
		{2, "auto"},
		{99, "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("FanMode(%d).String() = %q, want %q", int(tt.mode), got, tt.want)
		}
	}
}
