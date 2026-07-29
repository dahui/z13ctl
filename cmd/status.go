package cmd

// status.go — "status" subcommand: display a summary of all system metrics.
// Read-only command, no flags. Aggregates APU temperature, fan RPM, profile,
// TDP, and battery information into a single dashboard view.

import (
	"fmt"
	"os"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status (temperature, fans, profile, TDP, battery)",
	Long: `Display a summary of all system metrics in a single view.

Shows APU temperature, fan speed and mode, performance profile, TDP power
limits, and battery charge level and limit. All values are read directly
from sysfs.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runStatus()
	},
}

func runStatus() error {
	// APU temperature.
	if temp, err := cli.ReadAPUTemperature(); err == nil {
		fmt.Printf("APU:     %d°C\n", temp)
	} else {
		fmt.Println("APU:     N/A")
	}

	// Fan RPM and mode.
	rpms, rpmErr := cli.ReadBothFanRPM()
	modes, modeErr := cli.ReadBothFanModes()
	rpmStr := "N/A"
	if rpmErr == nil {
		rpmStr = fmt.Sprintf("%d RPM", rpms[0])
	}
	modeStr := ""
	if modeErr == nil {
		modeStr = ", mode: " + cli.FanModeName(modes[0])
	}
	fmt.Printf("Fans:    %s%s\n", rpmStr, modeStr)

	// Performance profile.
	profile := readCurrentProfile()
	fmt.Printf("Profile: %s\n", profile)

	// TDP power limits.
	tdp, tdpErr := cli.ReadEffectivePPT(effectiveProfileForTDP())
	if tdpErr == nil {
		fmt.Printf("TDP:     %dW (PL1) / %dW (PL2) / %dW (PL3)\n",
			tdp.PL1SPL, tdp.PL2SPPT, tdp.FPPT)
	} else {
		fmt.Println("TDP:     N/A")
	}

	// Undervolt (Curve Optimizer). Ask the daemon, which probed once at startup
	// and cached the answer.
	//
	// status must NOT call SMUProbeUndervolt itself: the probe writes a CO
	// offset of 0, which is exactly a reset, and the cache that makes that
	// harmless in the daemon does not survive a CLI process — so probing here
	// would silently wipe an active undervolt every time anyone ran `status`.
	// Without a daemon, report only what stat'ing sysfs can prove.
	// CO values have no sysfs readback, so current values need daemon state.
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil {
		if st.UndervoltAvailable {
			fmt.Println("UV:      available (use 'undervolt --get' for current values)")
		}
	} else if cli.SMUAvailable() {
		fmt.Println("UV:      ryzen_smu loaded (start the daemon to confirm Curve Optimizer support)")
	}

	// Battery: current charge level and charge limit.
	capStr := "N/A"
	if data, err := os.ReadFile(cli.FindBatteryCapacityPath()); err == nil {
		capStr = strings.TrimSpace(string(data)) + "%"
	}
	limitStr := ""
	if data, err := os.ReadFile(cli.FindBatteryThresholdPath()); err == nil {
		limitStr = " (limit: " + strings.TrimSpace(string(data)) + "%)"
	}
	fmt.Printf("Battery: %s%s\n", capStr, limitStr)

	return nil
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
