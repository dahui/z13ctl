package cmd

// tdp.go — "tdp" subcommand: read or set TDP power limits via the Linux
// asus-nb-wmi PPT sysfs attributes. No HID access required.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var (
	tdpGetFlag   bool
	tdpSetFlag   string
	tdpResetFlag bool
	tdpPL1Flag   string
	tdpPL2Flag   string
	tdpPL3Flag   string
	tdpForceFlag bool
)

var tdpCmd = &cobra.Command{
	Use:   "tdp",
	Short: "Get or set TDP power limits via asus-nb-wmi PPT",
	Long: `Get or set TDP power limits via the Linux asus-nb-wmi PPT sysfs attributes.

With --get, prints all current PPT (Package Power Tracking) values.

With --set, writes power limits in watts. By default, all PPT values are set to
the same value. Use --pl1, --pl2, --pl3 to override individual limits.

Safety: The sustained power limit (PL1) is capped at 75W by default. Use --force
to allow PL1 up to 93W (the absolute hardware maximum for the ROG Flow Z13
GZ302E). When PL1 exceeds 75W, fans are automatically set to full speed for
thermal safety. Burst limits (PL2/PL3) are allowed up to 93W without --force
since short bursts are thermally safe.

With --reset, switches to the balanced profile and resets fan curves to auto mode.
The firmware then manages PPT and fan curves automatically.

PPT attributes:
  PL1/SPL          — Sustained Power Limit: the continuous power budget the APU
                     can draw indefinitely. This is your effective base TDP.
  PL2/sPPT         — Short-term boost: the APU can draw this much power for
                     several seconds before throttling back to PL1.
  PL3/fPPT         — Fast boost: the maximum instantaneous power the APU can
                     draw for millisecond-scale spikes (e.g. launching an app).
  APU sPPT         — APU-specific short-term limit (automatically set to PL2).
  Platform sPPT    — Platform-level short-term limit (automatically set to PL2).

When using --set, all three limits are set to the same value by default. Use
--pl1, --pl2, and --pl3 to set them independently — for example, --set 45
--pl2 55 --pl3 65 allows short bursts up to 65W while sustaining 45W.

Stock profiles (quiet/balanced/performance) let the firmware manage TDP
dynamically. Setting a custom TDP switches to the "custom" profile.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !tdpGetFlag && tdpSetFlag == "" && !tdpResetFlag {
			return cmd.Help()
		}

		if tdpSetFlag != "" {
			return runTdpSet()
		}
		if tdpResetFlag {
			return runTdpReset()
		}
		return runTdpGet()
	},
}

func runTdpGet() error {
	profile := readCurrentProfile()
	tdp, err := cli.ReadEffectivePPT(profile)
	if err != nil {
		return fmt.Errorf("reading TDP: %w", err)
	}

	fmt.Println("TDP Power Limits (watts):")
	fmt.Printf("  PL1 (SPL):          %d\n", tdp.PL1SPL)
	fmt.Printf("  PL2 (sPPT):         %d\n", tdp.PL2SPPT)
	fmt.Printf("  PL3 (fPPT):         %d\n", tdp.FPPT)
	fmt.Printf("  APU sPPT:           %d\n", tdp.APUSPPT)
	fmt.Printf("  Platform sPPT:      %d\n", tdp.PlatformSPPT)
	return nil
}

func readCurrentProfile() string {
	data, err := os.ReadFile(cli.FindProfilePath())
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

func runTdpSet() error {
	watts, err := strconv.Atoi(tdpSetFlag)
	if err != nil {
		return fmt.Errorf("invalid TDP value %q: must be an integer", tdpSetFlag)
	}

	pl1, pl2, pl3, err := parsePLOverrides(watts)
	if err != nil {
		return err
	}

	// PL1 (sustained) requires --force above 75W. PL2/PL3 (burst) are allowed
	// up to the hardware max without --force since short bursts are thermally safe.
	pl1Max := cli.TDPMaxSafe
	if tdpForceFlag {
		pl1Max = cli.TDPMaxForced
	}
	if pl1 < cli.TDPMin || pl1 > pl1Max {
		if pl1 > cli.TDPMaxSafe && !tdpForceFlag {
			return fmt.Errorf("PL1 value %dW exceeds safe sustained maximum (%dW); use --force to allow up to %dW",
				pl1, cli.TDPMaxSafe, cli.TDPMaxForced)
		}
		return fmt.Errorf("PL1 value %dW out of range %d–%d", pl1, cli.TDPMin, pl1Max)
	}
	for _, v := range []struct {
		name  string
		value int
	}{
		{"PL2", pl2}, {"PL3", pl3},
	} {
		if v.value < cli.TDPMin || v.value > cli.TDPMaxForced {
			return fmt.Errorf("%s value %dW out of range %d–%d", v.name, v.value, cli.TDPMin, cli.TDPMaxForced)
		}
	}

	if dryRunFlag {
		cli.DryRunTdp(watts, pl1, pl2, pl3, tdpForceFlag)
		return nil
	}

	// Safety: set fans to 80% minimum when sustained TDP exceeds safe max.
	if pl1 > cli.TDPMaxSafe {
		if err := cli.SetBothFanCurves(cli.HighTDPFanCurve()); err != nil {
			return fmt.Errorf("failed to set high-TDP fan curve: %w (refusing to apply unsafe TDP)", err)
		}
		fmt.Println("Fans set to 80%+ curve for thermal safety")
	}

	if handled, err := api.SendTdpSet(tdpSetFlag, tdpPL1Flag, tdpPL2Flag, tdpPL3Flag, tdpForceFlag); handled {
		if err != nil {
			return err
		}
		fmt.Printf("TDP set to %dW\n", watts)
		return nil
	}

	if err := cli.SetTDP(watts, pl1, pl2, pl3); err != nil {
		return fmt.Errorf("setting TDP: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Printf("TDP set to %dW\n", watts)
	return nil
}

func runTdpReset() error {
	if dryRunFlag {
		cli.DryRunTdpReset()
		return nil
	}

	if handled, err := api.SendTdpReset(); handled {
		if err != nil {
			return err
		}
		fmt.Println("TDP reset: switched to balanced profile (firmware manages PPT)")
		return nil
	}

	// Direct path (no daemon): reset fans to auto, then switch to balanced
	// profile. The firmware sets per-profile PPT and fan curves automatically.
	if err := cli.ResetAllFanCurves(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to reset fan curves: %v\n", err)
	}
	if err := cli.SetProfile("balanced"); err != nil {
		return fmt.Errorf("switching to balanced profile: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Println("TDP reset: switched to balanced profile")
	return nil
}

// parsePLOverrides returns the effective PL1/PL2/PL3 values, applying
// per-PL flag overrides when set. Non-zero overrides replace the unified watts value.
func parsePLOverrides(watts int) (pl1, pl2, pl3 int, err error) {
	pl1, pl2, pl3 = watts, watts, watts
	if tdpPL1Flag != "" {
		pl1, err = strconv.Atoi(tdpPL1Flag)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid --pl1 value %q: must be an integer", tdpPL1Flag)
		}
	}
	if tdpPL2Flag != "" {
		pl2, err = strconv.Atoi(tdpPL2Flag)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid --pl2 value %q: must be an integer", tdpPL2Flag)
		}
	}
	if tdpPL3Flag != "" {
		pl3, err = strconv.Atoi(tdpPL3Flag)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid --pl3 value %q: must be an integer", tdpPL3Flag)
		}
	}
	return pl1, pl2, pl3, nil
}

func init() {
	tdpCmd.Flags().BoolVar(&tdpGetFlag, "get", false, "Print current TDP power limits")
	tdpCmd.Flags().StringVar(&tdpSetFlag, "set", "", "Set TDP power limit in watts")
	tdpCmd.Flags().BoolVar(&tdpResetFlag, "reset", false, "Reset to balanced profile (firmware manages PPT)")
	tdpCmd.Flags().StringVar(&tdpPL1Flag, "pl1", "", "Override PL1/SPL (watts)")
	tdpCmd.Flags().StringVar(&tdpPL2Flag, "pl2", "", "Override PL2/sPPT (watts)")
	tdpCmd.Flags().StringVar(&tdpPL3Flag, "pl3", "", "Override PL3/fPPT (watts)")
	tdpCmd.Flags().BoolVar(&tdpForceFlag, "force", false, "Allow sustained TDP (PL1) above 75W (up to 93W)")
	rootCmd.AddCommand(tdpCmd)
}
