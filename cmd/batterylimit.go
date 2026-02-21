package cmd

// batterylimit.go — "batterylimit" subcommand: read or set the battery charge
// limit via the Linux ACPI power_supply sysfs interface. No HID access required.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"z13ctl/internal/cli"
	"z13ctl/internal/daemon"

	"github.com/spf13/cobra"
)

var (
	batteryGetFlag bool
	batterySetFlag string
)

var batterylimitCmd = &cobra.Command{
	Use:   "batterylimit",
	Short: "Get or set the battery charge limit via ACPI power_supply",
	Long: `Get or set the battery charge end threshold via the Linux ACPI power_supply
sysfs interface.

With --get, prints the current charge limit percentage.
With --set, writes the threshold to the kernel (root or group access required).

Range: 40–100. Writing 100 removes any limit (charges to full).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !batteryGetFlag && batterySetFlag == "" {
			return cmd.Help()
		}

		if batterySetFlag != "" {
			limit, err := strconv.Atoi(batterySetFlag)
			if err != nil || limit < 40 || limit > 100 {
				return fmt.Errorf("invalid limit %q: must be an integer 40–100", batterySetFlag)
			}

			if dryRunFlag {
				cli.DryRunBatteryLimit(limit)
				return nil
			}

			if handled, err := daemon.SendBatteryLimitSet(limit); handled {
				if err != nil {
					return err
				}
				fmt.Printf("Battery charge limit set to %d%%\n", limit)
				return nil
			}

			path := cli.FindBatteryThresholdPath()
			if err := os.WriteFile(path, []byte(strconv.Itoa(limit)+"\n"), 0o644); err != nil {
				return fmt.Errorf("setting battery limit: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
			}
			fmt.Printf("Battery charge limit set to %d%%\n", limit)
			return nil
		}

		// --get
		data, err := os.ReadFile(cli.FindBatteryThresholdPath())
		if err != nil {
			return fmt.Errorf("reading battery limit: %w", err)
		}
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	},
}

func init() {
	batterylimitCmd.Flags().BoolVar(&batteryGetFlag, "get", false, "Print the current battery charge limit")
	batterylimitCmd.Flags().StringVar(&batterySetFlag, "set", "", "Set the battery charge limit (40–100)")
	rootCmd.AddCommand(batterylimitCmd)
}
