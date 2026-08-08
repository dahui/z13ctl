package cmd

// status.go — "status" subcommand: display a summary of all system metrics.
// Read-only command, no flags. Aggregates APU temperature, fan RPM, profile,
// TDP, and battery information into a single dashboard view.

import (
	"encoding/json"
	"fmt"

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
	hw, err := hardware()
	if err != nil {
		return err
	}

	// APU temperature.
	tempShown := false
	if hw.Telemetry != nil {
		if s, sErr := hw.Telemetry.Sample(); sErr == nil {
			fmt.Printf("APU:     %d°C\n", s.TempC)
			tempShown = true
		}
	}
	if !tempShown {
		fmt.Println("APU:     N/A")
	}

	// Fan RPM and mode. The mode is folded across every readable fan, as the
	// daemon reports it: "custom" only when all of them honour the curve.
	rpmStr := "N/A"
	modeStr := ""
	if hw.Fans != nil {
		if rpms, rErr := hw.Fans.ReadRPM(); rErr == nil && len(rpms) > 0 {
			rpmStr = fmt.Sprintf("%d RPM", rpms[0])
		}
		if mode, mErr := hw.Fans.ReadMode(); mErr == nil {
			modeStr = ", mode: " + cli.FanModeName(mode)
		}
	}
	fmt.Printf("Fans:    %s%s\n", rpmStr, modeStr)

	// Performance profile. platform_profile is never a custom profile name, so
	// the effective profile comes from the daemon when it is running; show the
	// firmware profile underneath it when the two differ.
	profile := effectiveProfileForTDP(hw)
	if hwProf := readCurrentProfile(hw); hwProf != profile && hwProf != "unknown" {
		fmt.Printf("Profile: %s (platform: %s)\n", profile, hwProf)
	} else {
		fmt.Printf("Profile: %s\n", profile)
	}

	// One battery reading serves the power-source line here and the charge
	// level at the bottom. ACKnown carries the "unknown is not on-battery"
	// distinction: no Mains supply means the line is simply omitted.
	var onAC bool
	acKnown := false
	capStr := "N/A"
	if hw.Battery != nil {
		if st, bErr := hw.Battery.Status(); bErr == nil {
			onAC, acKnown = st.OnAC, st.ACKnown
			capStr = fmt.Sprintf("%d%%", st.Capacity)
		}
	}

	// Power source, and what autoswitch would select for it.
	if acKnown {
		source := "battery"
		if onAC {
			source = "AC"
		}
		fmt.Printf("Power:   %s%s\n", source, autoswitchNote(onAC))
	}

	// TDP power limits.
	tdpShown := false
	if hw.Power != nil {
		if tdp, tErr := hw.Power.ReadEffective(profile); tErr == nil {
			fmt.Printf("TDP:     %dW (PL1) / %dW (PL2) / %dW (PL3)\n",
				tdp.PL1SPL, tdp.PL2SPPT, tdp.FPPT)
			tdpShown = true
		}
	}
	if !tdpShown {
		fmt.Println("TDP:     N/A")
	}

	// Undervolt (Curve Optimizer). Ask the daemon, which probed once at startup
	// and cached the answer.
	//
	// status must NOT probe itself: ProbeAvailable writes a CO offset of 0,
	// which is exactly a reset, and the cache that makes that harmless in the
	// daemon does not survive a CLI process — so probing here would silently
	// wipe an active undervolt every time anyone ran `status`. Without a
	// daemon, Present reports only what stat'ing sysfs can prove.
	// CO values have no sysfs readback, so current values need daemon state.
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil {
		if st.UndervoltAvailable {
			fmt.Println("UV:      available (use 'undervolt --get' for current values)")
		}
	} else if hw.Undervolt != nil && hw.Undervolt.Present() {
		fmt.Println("UV:      ryzen_smu loaded (start the daemon to confirm Curve Optimizer support)")
	}

	// Battery: current charge level and charge limit.
	limitStr := ""
	if hw.Battery != nil {
		if limit, lErr := hw.Battery.ChargeLimit(); lErr == nil {
			limitStr = fmt.Sprintf(" (limit: %d%%)", limit)
		}
	}
	fmt.Printf("Battery: %s%s\n", capStr, limitStr)

	return nil
}

// autoswitchNote returns a parenthetical naming the profile autoswitch selects
// for the current source, or "" when it is off, unconfigured, or the daemon is
// not running. Best-effort: status must not fail because autoswitch is unset.
func autoswitchNote(onAC bool) string {
	handled, value, err := api.SendAutoswitchGet()
	if !handled || err != nil {
		return ""
	}
	var st autoswitchStatus
	if err := json.Unmarshal([]byte(value), &st); err != nil || !st.Enabled {
		return ""
	}
	target := st.Battery
	if onAC {
		target = st.AC
	}
	if target == "" {
		return ""
	}
	return " (autoswitch: " + target + ")"
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
