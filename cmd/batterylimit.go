package cmd

// batterylimit.go — "batterylimit" subcommand: read or set the battery charge
// limit via the Linux ACPI power_supply sysfs interface. No HID access required.

import (
	"fmt"
	"os"
	"strconv"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"

	"github.com/spf13/cobra"
)

var (
	batteryGetFlag      bool
	batterySetFlag      string
	batteryFullOnceFlag bool
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
top of the range removes any limit (charges to full).

With --full-once, the limit is lifted until the battery is full, and then the
daemon puts it back (G-Helper's "charge to 100% once"). Unplugging part-way
does not end it; --full-once=false cancels it and restores the limit now, and
so does setting a limit. It needs the daemon, which watches for the battery to
fill.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("full-once") {
			return runChargeOnce(batteryFullOnceFlag)
		}
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
				return fmt.Errorf("%s", hw.Missing("charge-limit", "no battery charge control on this device"))
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
				return withSetupHint("setting battery limit", err)
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
		// The number is what the hardware holds (scripts parse it); the
		// note goes to stderr.
		if _, st, err := api.SendGetState(); err == nil && st != nil && st.ChargeOnce {
			fmt.Fprintf(os.Stderr, "note: charging to full once; the limit returns to %d%% when the battery is full\n", st.Battery)
		}
		return nil
	},
}

// runChargeOnce starts or cancels a one-time charge to full. Daemon only: it
// is the daemon that notices the pack is full and restores the limit, so a
// direct write would lift the limit for good.
func runChargeOnce(on bool) error {
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Battery == nil || !hw.Battery.Caps().ChargeLimit {
		return fmt.Errorf("%s", hw.Missing("charge-limit", "no battery charge control on this device"))
	}
	if dryRunFlag {
		if on {
			fmt.Printf("Would lift the battery charge limit to %d%% until the battery is full, then put the current limit back\n",
				hw.Battery.Caps().ChargeLimitMax)
		} else {
			fmt.Println("Would cancel a one-time charge to full and put the limit back")
		}
		return nil
	}
	handled, err := api.SendChargeOnce(on)
	if !handled {
		return fmt.Errorf("charging to full once needs the voltaire daemon running: it watches for the battery to fill and puts the limit back")
	}
	if err != nil {
		return err
	}
	if on {
		fmt.Println("Charging to full once; the limit comes back when the battery is full")
	} else {
		fmt.Println("One-time charge cancelled; the limit is back")
	}
	return nil
}

func init() {
	batterylimitCmd.Flags().BoolVar(&batteryGetFlag, "get", false, "Print the current battery charge limit")
	batterylimitCmd.Flags().StringVar(&batterySetFlag, "set", "", "Set the battery charge limit, in percent")
	batterylimitCmd.Flags().BoolVar(&batteryFullOnceFlag, "full-once", false,
		"Charge to full once, then put the limit back (=false cancels)")
	rootCmd.AddCommand(batterylimitCmd)
}
