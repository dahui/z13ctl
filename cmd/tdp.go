package cmd

// tdp.go — "tdp" subcommand: read or set TDP power limits via the Linux
// asus-nb-wmi PPT sysfs attributes. No HID access required.

import (
	"fmt"
	"strconv"

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

Safety: Maximum safe TDP is 75W. Use --force to allow up to 93W (the absolute
hardware maximum for the ROG Flow Z13 GZ302E). When --force is used with values
above 75W, fans are automatically set to full speed for thermal safety.

With --reset, restores firmware default PPT values.

PPT attributes:
  PL1/SPL          — Sustained Power Limit (base TDP)
  PL2/sPPT         — Short Power Package Tracking (short boost)
  PL3/fPPT         — Fast Boost (instantaneous boost)
  APU sPPT         — APU Short PPT (follows PL2)
  Platform sPPT    — Platform Short PPT (follows PL2)`,
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
	tdp, err := cli.ReadAllPPT()
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

func runTdpSet() error {
	watts, err := strconv.Atoi(tdpSetFlag)
	if err != nil {
		return fmt.Errorf("invalid TDP value %q: must be an integer", tdpSetFlag)
	}

	pl1, pl2, pl3, err := parsePLOverrides(watts)
	if err != nil {
		return err
	}

	tdpMax := cli.TDPMaxSafe
	if tdpForceFlag {
		tdpMax = cli.TDPMaxForced
	}
	for _, v := range []struct {
		name  string
		value int
	}{
		{"PL1", pl1}, {"PL2", pl2}, {"PL3", pl3},
	} {
		if v.value < cli.TDPMin || v.value > tdpMax {
			if v.value > cli.TDPMaxSafe && !tdpForceFlag {
				return fmt.Errorf("%s value %dW exceeds safe maximum (%dW); use --force to allow up to %dW",
					v.name, v.value, cli.TDPMaxSafe, cli.TDPMaxForced)
			}
			return fmt.Errorf("%s value %dW out of range %d–%d", v.name, v.value, cli.TDPMin, tdpMax)
		}
	}

	if dryRunFlag {
		cli.DryRunTdp(watts, pl1, pl2, pl3, tdpForceFlag)
		return nil
	}

	// Safety: force fans to full speed when any value exceeds safe max.
	if tdpForceFlag && (pl1 > cli.TDPMaxSafe || pl2 > cli.TDPMaxSafe || pl3 > cli.TDPMaxSafe) {
		if err := cli.SetAllFansFullSpeed(); err != nil {
			return fmt.Errorf("failed to set fans to full speed: %w (refusing to apply unsafe TDP)", err)
		}
		fmt.Println("Fans set to full speed for thermal safety")
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
		fmt.Println("TDP reset to firmware defaults")
		return nil
	}

	if err := cli.ResetTDP(); err != nil {
		return fmt.Errorf("resetting TDP: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Println("TDP reset to firmware defaults")
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
	tdpCmd.Flags().BoolVar(&tdpResetFlag, "reset", false, "Restore firmware default TDP")
	tdpCmd.Flags().StringVar(&tdpPL1Flag, "pl1", "", "Override PL1/SPL (watts)")
	tdpCmd.Flags().StringVar(&tdpPL2Flag, "pl2", "", "Override PL2/sPPT (watts)")
	tdpCmd.Flags().StringVar(&tdpPL3Flag, "pl3", "", "Override PL3/fPPT (watts)")
	tdpCmd.Flags().BoolVar(&tdpForceFlag, "force", false, "Allow TDP above 75W (up to 93W)")
	rootCmd.AddCommand(tdpCmd)
}
