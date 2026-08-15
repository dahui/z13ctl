package cmd

// cpuboost.go — "cpuboost" subcommand: read or set the CPU's opportunistic
// boost clocks through cpufreq's global boost switch.
//
// Unlike the firmware toggles this resembles, boost is not a setting the
// machine keeps: cpufreq comes up boosting on every boot. The daemon records
// the choice and replays it at startup, which is why --set prefers the daemon
// even though the sysfs write would succeed without it — a value written
// directly here is one nothing will restore.

import (
	"fmt"
	"strconv"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"

	"github.com/spf13/cobra"
)

var (
	cpuBoostGetFlag bool
	cpuBoostSetFlag string
)

var cpuboostCmd = &cobra.Command{
	Use:   "cpuboost",
	Short: "Get or set CPU boost clocks",
	Long: `Get or set the CPU's opportunistic boost clocks (cpufreq's global boost switch).

Values:
  0 — disabled (cores are capped at their base clock)
  1 — enabled (default)

Disabling boost lowers peak power and heat at the cost of peak single-thread
performance. It is a kernel runtime setting rather than a firmware one: the
kernel re-enables it on every boot, so the voltaire daemon records the choice
and restores it at startup. Setting it with the daemon stopped changes the
hardware but will not survive a reboot.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !cpuBoostGetFlag && cpuBoostSetFlag == "" {
			return cmd.Help()
		}

		if cpuBoostSetFlag != "" {
			value, err := strconv.Atoi(cpuBoostSetFlag)
			if err != nil || (value != 0 && value != 1) {
				return fmt.Errorf("invalid value %q: must be 0 or 1", cpuBoostSetFlag)
			}
			on := value == 1

			if dryRunFlag {
				cli.DryRunCPUBoost(on)
				return nil
			}

			if handled, sendErr := api.SendCPUBoostSet(on); handled {
				if sendErr != nil {
					return sendErr
				}
				fmt.Printf("CPU boost %s\n", boostWord(on))
				return nil
			}

			hw, err := hardware()
			if err != nil {
				return err
			}
			if hw.CPUBoost == nil {
				return fmt.Errorf("no CPU boost control on this device")
			}
			if err := hw.CPUBoost.Set(on); err != nil {
				return fmt.Errorf("setting cpu boost: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
			}
			// Said plainly rather than warned about: with no daemon there is
			// nothing to replay the choice, and a user who reboots and finds
			// boost back on deserves to have been told why.
			fmt.Printf("CPU boost %s (daemon not running — this will not survive a reboot)\n", boostWord(on))
			return nil
		}

		// --get reads hardware directly, as every other --get does: sysfs is
		// ground truth, and here it is also the only thing that can be wrong in
		// an interesting way, since anything with the grant can write it.
		hw, err := hardware()
		if err != nil {
			return err
		}
		if hw.CPUBoost == nil {
			return fmt.Errorf("no CPU boost control on this device")
		}
		on, err := hw.CPUBoost.Get()
		if err != nil {
			return fmt.Errorf("reading cpu boost: %w", err)
		}
		fmt.Printf("CPU boost: %s\n", boostWord(on))
		return nil
	},
}

func boostWord(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

func init() {
	cpuboostCmd.Flags().BoolVar(&cpuBoostGetFlag, "get", false, "Show whether CPU boost is enabled")
	cpuboostCmd.Flags().StringVar(&cpuBoostSetFlag, "set", "", "Enable (1) or disable (0) CPU boost")
	rootCmd.AddCommand(cpuboostCmd)
}
