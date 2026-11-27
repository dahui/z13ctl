package safety

// tdpstatefor_test.go — TDP flag resolution (moved here from internal/cli,
// whose TDPStateFor only forwarded to it).

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

func TestTDPStateFor(t *testing.T) {
	tests := []struct {
		name                 string
		watts, pl1, pl2, pl3 int
		want                 api.TDPState
	}{
		{
			name:  "unified watts fills all limits",
			watts: 45,
			want:  api.TDPState{PL1SPL: 45, PL2SPPT: 45, FPPT: 45, APUSPPT: 45, PlatformSPPT: 45},
		},
		{
			name:  "non-zero overrides replace watts",
			watts: 45, pl1: 40, pl2: 60, pl3: 70,
			want: api.TDPState{PL1SPL: 40, PL2SPPT: 60, FPPT: 70, APUSPPT: 60, PlatformSPPT: 60},
		},
		{
			name:  "zero override falls back to watts",
			watts: 50, pl2: 65,
			want: api.TDPState{PL1SPL: 50, PL2SPPT: 65, FPPT: 50, APUSPPT: 65, PlatformSPPT: 65},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TDPStateFor(tt.watts, tt.pl1, tt.pl2, tt.pl3); got != tt.want {
				t.Errorf("TDPStateFor() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
