package cmd

// undervolt.go — "undervolt" subcommand: read or set CPU Curve Optimizer
// offsets via the ryzen_smu kernel module. Requires the ryzen_smu DKMS module.

import (
	"encoding/json"
	"fmt"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var (
	uvGetFlag   bool
	uvSetFlag   string
	uvResetFlag bool
)

var undervoltCmd = &cobra.Command{
	Use:   "undervolt",
	Short: "Get or set CPU Curve Optimizer offsets via ryzen_smu",
	Long: `Get or set AMD Curve Optimizer (CO) offsets for all CPU cores.

Curve Optimizer adjusts the voltage-frequency curve — negative values reduce
voltage (undervolt), improving efficiency and thermals without reducing performance.

With --get, prints the current CO offset from daemon state. CO values have no
sysfs readback, so this shows the last-applied value and whether it is active.

With --set, applies an all-core CPU CO offset. Values must be 0 (stock) or
negative (undervolt).

With --reset, resets CPU CO to 0 (stock voltage).

Safety limits (matching G-Helper defaults):
  CPU: 0 to -40

Requires the ryzen_smu kernel module (ryzen_smu-dkms-git on Arch/AUR).
The amkillam fork is required for Strix Halo (Ryzen AI MAX+) support.
CO values are volatile — they reset on reboot and sleep. The daemon reapplies
them automatically on startup and resume when the custom profile is active.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !uvGetFlag && uvSetFlag == "" && !uvResetFlag {
			return cmd.Help()
		}

		if uvSetFlag != "" {
			return runUndervoltSet()
		}
		if uvResetFlag {
			return runUndervoltReset()
		}
		return runUndervoltGet()
	},
}

func runUndervoltGet() error {
	if handled, value, err := api.SendUndervoltGet(); handled {
		if err != nil {
			return err
		}
		var resp struct {
			api.UndervoltState
			Profile string `json:"profile"`
		}
		if jErr := json.Unmarshal([]byte(value), &resp); jErr != nil {
			return fmt.Errorf("parsing undervolt state: %w", jErr)
		}
		printUndervoltState(resp.UndervoltState, resp.Profile)
		return nil
	}

	// No daemon — check if SMU is available at all.
	if !cli.SMUAvailable() {
		return fmt.Errorf("ryzen_smu kernel module not detected\n  Install: ryzen_smu-dkms-git (AUR) or equivalent for your distro")
	}
	fmt.Println("Curve Optimizer: not set (daemon not running)")
	return nil
}

func printUndervoltState(uv api.UndervoltState, profile string) {
	if uv.CPUCO == 0 {
		fmt.Println("Curve Optimizer: stock (0)")
		return
	}
	if uv.Active {
		fmt.Println("Curve Optimizer offsets:")
	} else {
		fmt.Printf("Curve Optimizer offsets (not active — %s profile):\n", profile)
	}
	suffix := ""
	if !uv.Active {
		suffix = "  (saved)"
	}
	fmt.Printf("  CPU: %d%s\n", uv.CPUCO, suffix)
}

func runUndervoltSet() error {
	var cpuOffset int
	if _, err := fmt.Sscan(uvSetFlag, &cpuOffset); err != nil {
		return fmt.Errorf("invalid CPU undervolt value %q: must be an integer", uvSetFlag)
	}

	if err := cli.ValidateCOValues(cpuOffset); err != nil {
		return err
	}

	if dryRunFlag {
		cli.DryRunUndervolt(cpuOffset)
		return nil
	}

	if handled, err := api.SendUndervoltSet(uvSetFlag); handled {
		if err != nil {
			return err
		}
		fmt.Printf("Curve Optimizer set: CPU %d\n", cpuOffset)
		return nil
	}

	if err := cli.SetCurveOptimizer(cpuOffset); err != nil {
		return fmt.Errorf("setting curve optimizer: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Printf("Curve Optimizer set: CPU %d\n", cpuOffset)
	return nil
}

func runUndervoltReset() error {
	if dryRunFlag {
		cli.DryRunUndervoltReset()
		return nil
	}

	if handled, err := api.SendUndervoltReset(); handled {
		if err != nil {
			return err
		}
		fmt.Println("Curve Optimizer reset to stock (0)")
		return nil
	}

	if err := cli.ResetCurveOptimizer(); err != nil {
		return fmt.Errorf("resetting curve optimizer: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Println("Curve Optimizer reset to stock (0)")
	return nil
}

func init() {
	undervoltCmd.Flags().BoolVar(&uvGetFlag, "get", false, "Print current Curve Optimizer offset")
	undervoltCmd.Flags().StringVar(&uvSetFlag, "set", "", "Set all-core CPU CO offset (0 to -40)")
	undervoltCmd.Flags().BoolVar(&uvResetFlag, "reset", false, "Reset CO to stock (0)")
	rootCmd.AddCommand(undervoltCmd)
}
