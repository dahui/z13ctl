package cmd

// paneloverdrive.go — "paneloverdrive" subcommand: read or set the panel refresh
// overdrive via the ASUS asus-armoury firmware-attributes sysfs interface.
// No HID access required.

import (
	"github.com/spf13/cobra"
)

var (
	panelOverdriveGetFlag bool
	panelOverdriveSetFlag string
)

var paneloverdriveCmd = &cobra.Command{
	Use:   "paneloverdrive",
	Short: "Get or set panel refresh overdrive (ASUS firmware toggle)",
	Long: `Get or set the display panel's refresh overdrive, an ASUS firmware toggle
exposed by the kernel's asus-armoury firmware-attributes interface. It is the
named form of 'voltaire feature --get/--set panel_overdrive', kept for scripts;
a device without the toggle refuses it.

Values are the ones the firmware lists for the attribute (--list on the
feature command shows them); for this switch:
  0 — disabled
  1 — enabled`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case panelOverdriveSetFlag != "":
			return setToggle("panel_overdrive", panelOverdriveSetFlag, "Panel overdrive set to %d\n")
		case panelOverdriveGetFlag:
			return runFeatureGet("panel_overdrive")
		}
		return cmd.Help()
	},
}

func init() {
	paneloverdriveCmd.Flags().BoolVar(&panelOverdriveGetFlag, "get", false, "Print the current panel overdrive setting")
	paneloverdriveCmd.Flags().StringVar(&panelOverdriveSetFlag, "set", "", "Set panel overdrive (0=off, 1=on)")
	rootCmd.AddCommand(paneloverdriveCmd)
}
