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

With --reset, switches to the balanced profile, resets fan curves to auto mode,
and writes balanced's stock PPT values back to hardware. The firmware manages
fan curves for stock profiles but does not restore PPT on its own.

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

Setting a custom TDP switches to the "custom" profile. Switching back to a
stock profile restores that profile's stock PPT values to hardware while
keeping the custom values saved, so "custom" stays re-selectable.`,
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
	tdp, err := cli.ReadEffectivePPT(effectiveProfileForTDP())
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

// effectiveProfileForTDP returns the profile name to use when interpreting PPT
// values. It prefers the daemon's own profile because "custom" is a virtual
// profile that is deliberately never written to platform_profile — so sysfs
// alone cannot tell a legitimate 5W custom TDP from the kernel's stale 5W cache,
// and cli.ReadEffectivePPT would substitute the stock table for real values.
// Falls back to platform_profile when the daemon is not running.
func effectiveProfileForTDP() string {
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil && st.Profile != "" {
		return st.Profile
	}
	return readCurrentProfile()
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

	// The daemon applies the fan floor itself (cli.ApplyTDPSafely), so hand the
	// whole operation over before touching hardware here.
	if handled, err := api.SendTdpSet(tdpSetFlag, tdpPL1Flag, tdpPL2Flag, tdpPL3Flag, tdpForceFlag); handled {
		if err != nil {
			return err
		}
		fmt.Printf("TDP set to %dW\n", watts)
		return nil
	}

	// Direct path: same helper, so the no-daemon path enforces the 80% fan floor
	// on the same terms — fans first, and no TDP at all if that write fails.
	if err := cli.ApplyTDPSafely(cli.TDPStateFor(watts, pl1, pl2, pl3)); err != nil {
		return fmt.Errorf("setting TDP: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	if pl1 > cli.TDPMaxSafe {
		fmt.Println("Fans set to 80%+ curve for thermal safety")
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
		fmt.Println("TDP reset: switched to balanced profile (stock PPT restored)")
		return nil
	}

	// Direct path (no daemon): switch to balanced, write its stock PPT values
	// back to hardware, and only then release the fans to firmware auto — so
	// they are never dropped to auto while a high custom TDP is still in force.
	// The firmware manages fan curves on a profile change but does not restore
	// PPT, so that part has to be explicit.
	if err := cli.SetProfile("balanced"); err != nil {
		return fmt.Errorf("switching to balanced profile: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	restoreStockPPT("balanced")
	if err := cli.ResetAllFanCurves(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to reset fan curves: %v\n", err)
	}
	fmt.Println("TDP reset: switched to balanced profile")
	return nil
}

// restoreStockPPT writes the measured stock PPT values for a stock profile back
// to hardware on the direct (no-daemon) path. The asus-nb-wmi PPT attributes
// have no "reset to firmware default" operation and the firmware does not
// re-apply per-profile limits on a platform_profile change, so without this a
// custom TDP leaks into every stock profile. Failures warn and continue: a
// profile switch must not hard-fail because the PPT restore did not take.
func restoreStockPPT(profile string) {
	stock, ok := cli.StockProfilePPT[profile]
	if !ok {
		return
	}
	if err := cli.SetTDPState(stock); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to restore stock TDP for %s: %v\n", profile, err)
	}
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
	tdpCmd.Flags().BoolVar(&tdpResetFlag, "reset", false, "Reset to balanced profile and restore its stock PPT values")
	tdpCmd.Flags().StringVar(&tdpPL1Flag, "pl1", "", "Override PL1/SPL (watts)")
	tdpCmd.Flags().StringVar(&tdpPL2Flag, "pl2", "", "Override PL2/sPPT (watts)")
	tdpCmd.Flags().StringVar(&tdpPL3Flag, "pl3", "", "Override PL3/fPPT (watts)")
	tdpCmd.Flags().BoolVar(&tdpForceFlag, "force", false, "Allow sustained TDP (PL1) above 75W (up to 93W)")
	rootCmd.AddCommand(tdpCmd)
}
