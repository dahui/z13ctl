package cmd

// paneloverdrive.go — "paneloverdrive" subcommand: read or set the panel refresh
// overdrive via the ASUS asus-armoury firmware-attributes sysfs interface.
// No HID access required.

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
	panelOverdriveGetFlag bool
	panelOverdriveSetFlag string
)

var paneloverdriveCmd = &cobra.Command{
	Use:   "paneloverdrive",
	Short: "Get or set panel refresh overdrive via asus-armoury firmware attributes",
	Long: `Get or set the display panel refresh overdrive via the Linux asus-armoury
firmware-attributes sysfs interface.

Values:
  0 — disabled
  1 — enabled`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !panelOverdriveGetFlag && panelOverdriveSetFlag == "" {
			return cmd.Help()
		}

		if panelOverdriveSetFlag != "" {
			value, err := strconv.Atoi(panelOverdriveSetFlag)
			if err != nil || (value != 0 && value != 1) {
				return fmt.Errorf("invalid value %q: must be 0 or 1", panelOverdriveSetFlag)
			}

			if dryRunFlag {
				cli.DryRunPanelOverdrive(value)
				return nil
			}

			if handled, err := api.SendPanelOverdriveSet(value); handled {
				if err != nil {
					return err
				}
				fmt.Printf("Panel overdrive set to %d\n", value)
				return nil
			}

			if err := cli.SetPanelOverdrive(value); err != nil {
				return fmt.Errorf("setting panel overdrive: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
			}
			fmt.Printf("Panel overdrive set to %d\n", value)
			return nil
		}

		// --get
		data, err := os.ReadFile(cli.FindPanelOverdrivePath())
		if err != nil {
			return fmt.Errorf("reading panel overdrive: %w", err)
		}
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	},
}

func init() {
	paneloverdriveCmd.Flags().BoolVar(&panelOverdriveGetFlag, "get", false, "Print the current panel overdrive setting")
	paneloverdriveCmd.Flags().StringVar(&panelOverdriveSetFlag, "set", "", "Set panel overdrive (0=off, 1=on)")
	rootCmd.AddCommand(paneloverdriveCmd)
}
