package driver_test

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

func TestFanModeName(t *testing.T) {
	tests := []struct {
		mode int
		want string
	}{
		{0, "full-speed"},
		{1, "custom"},
		{2, "auto"},
		{99, "unknown(99)"},
	}
	for _, tt := range tests {
		got := driver.FanModeName(tt.mode)
		if got != tt.want {
			t.Errorf("FanModeName(%d) = %q, want %q", tt.mode, got, tt.want)
		}
	}
}
