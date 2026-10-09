package cmd

// batterylimit.go — "batterylimit" subcommand: read or set the battery charge
// limit via the Linux ACPI power_supply sysfs interface. No HID access required.

import (
	"fmt"
	"strconv"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"

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

The accepted range is device data, since the kernel does not publish one; an
out-of-range value is refused with the range this machine accepts. Writing the
top of the range removes any limit (charges to full).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !batteryGetFlag && batterySetFlag == "" {
			return cmd.Help()
		}

		if batterySetFlag != "" {
			// The range comes from the device data, so the device is assembled
			// before anything is sent. Assembly reads DMI and constructs drivers;
			// it writes nothing.
			hw, err := hardware()
			if err != nil {
				return err
			}
			if hw.Battery == nil || !hw.Battery.Caps().ChargeLimit {
				return fmt.Errorf("no battery charge control on this device")
			}
			caps := hw.Battery.Caps()
			limit, err := strconv.Atoi(batterySetFlag)
			if err != nil || limit < caps.ChargeLimitMin || limit > caps.ChargeLimitMax {
				return fmt.Errorf("invalid limit %q: must be an integer %d–%d",
					batterySetFlag, caps.ChargeLimitMin, caps.ChargeLimitMax)
			}

			if dryRunFlag {
				cli.DryRunBatteryLimit(limit)
				return nil
			}

			if handled, sendErr := api.SendBatteryLimitSet(limit); handled {
				if sendErr != nil {
					return sendErr
				}
				fmt.Printf("Battery charge limit set to %d%%\n", limit)
				return nil
			}

			if err := hw.Battery.SetChargeLimit(limit); err != nil {
				return fmt.Errorf("setting battery limit: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
			}
			fmt.Printf("Battery charge limit set to %d%%\n", limit)
			return nil
		}

		// --get
		hw, err := hardware()
		if err != nil {
			return err
		}
		if hw.Battery == nil {
			return fmt.Errorf("no battery charge control on this device")
		}
		limit, err := hw.Battery.ChargeLimit()
		if err != nil {
			return fmt.Errorf("reading battery limit: %w", err)
		}
		fmt.Println(limit)
		return nil
	},
}

func init() {
	batterylimitCmd.Flags().BoolVar(&batteryGetFlag, "get", false, "Print the current battery charge limit")
	batterylimitCmd.Flags().StringVar(&batterySetFlag, "set", "", "Set the battery charge limit, in percent")
	rootCmd.AddCommand(batterylimitCmd)
}
