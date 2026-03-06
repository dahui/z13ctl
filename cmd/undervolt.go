package cmd

// undervolt.go — "undervolt" subcommand: read or set CPU/iGPU Curve Optimizer
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
	uvIGPUFlag  string
)

var undervoltCmd = &cobra.Command{
	Use:   "undervolt",
	Short: "Get or set CPU/iGPU Curve Optimizer offsets via ryzen_smu",
	Long: `Get or set AMD Curve Optimizer (CO) offsets for the CPU and integrated GPU.

Curve Optimizer adjusts the voltage-frequency curve — negative values reduce
voltage (undervolt), improving efficiency and thermals without reducing performance.

With --get, prints the current CO offsets from daemon state. CO values have no
sysfs readback, so this shows the last-applied values.

With --set, applies an all-core CPU CO offset. Use --igpu to also set the iGPU
offset. Values must be 0 (stock) or negative (undervolt).

With --reset, resets both CPU and iGPU CO to 0 (stock voltage).

Safety limits (matching G-Helper defaults):
  CPU:  0 to -40
  iGPU: 0 to -30

Requires the ryzen_smu kernel module (ryzen_smu-dkms-git on Arch/AUR).
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
	if uv.CPUCO == 0 && uv.IGPUCO == 0 {
		fmt.Println("Curve Optimizer: stock (0)")
		return
	}
	active := profile == "" || profile == "custom"
	if active {
		fmt.Println("Curve Optimizer offsets:")
	} else {
		fmt.Printf("Curve Optimizer offsets (not active — %s profile):\n", profile)
	}
	suffix := ""
	if !active {
		suffix = "  (saved)"
	}
	fmt.Printf("  CPU:  %d%s\n", uv.CPUCO, suffix)
	fmt.Printf("  iGPU: %d%s\n", uv.IGPUCO, suffix)
}

func runUndervoltSet() error {
	var cpuOffset int
	if _, err := fmt.Sscan(uvSetFlag, &cpuOffset); err != nil {
		return fmt.Errorf("invalid CPU undervolt value %q: must be an integer", uvSetFlag)
	}

	var igpuOffset int
	if uvIGPUFlag != "" {
		if _, err := fmt.Sscan(uvIGPUFlag, &igpuOffset); err != nil {
			return fmt.Errorf("invalid iGPU undervolt value %q: must be an integer", uvIGPUFlag)
		}
	}

	if err := cli.ValidateCOValues(cpuOffset, igpuOffset); err != nil {
		return err
	}

	if dryRunFlag {
		cli.DryRunUndervolt(cpuOffset, igpuOffset)
		return nil
	}

	if handled, err := api.SendUndervoltSet(uvSetFlag, uvIGPUFlag); handled {
		if err != nil {
			return err
		}
		printUndervoltResult(cpuOffset, igpuOffset)
		return nil
	}

	if err := cli.SetCurveOptimizer(cpuOffset, igpuOffset); err != nil {
		return fmt.Errorf("setting curve optimizer: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	printUndervoltResult(cpuOffset, igpuOffset)
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

func printUndervoltResult(cpu, igpu int) {
	if igpu != 0 {
		fmt.Printf("Curve Optimizer set: CPU %d, iGPU %d\n", cpu, igpu)
	} else {
		fmt.Printf("Curve Optimizer set: CPU %d\n", cpu)
	}
}

func init() {
	undervoltCmd.Flags().BoolVar(&uvGetFlag, "get", false, "Print current Curve Optimizer offsets")
	undervoltCmd.Flags().StringVar(&uvSetFlag, "set", "", "Set all-core CPU CO offset (0 to -40)")
	undervoltCmd.Flags().BoolVar(&uvResetFlag, "reset", false, "Reset CO to stock (0)")
	undervoltCmd.Flags().StringVar(&uvIGPUFlag, "igpu", "", "Set iGPU CO offset (0 to -30)")
	rootCmd.AddCommand(undervoltCmd)
}
