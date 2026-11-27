package cmd

// bootsound.go — "bootsound" subcommand: read or set the POST boot sound via
// the ASUS asus-armoury firmware-attributes sysfs interface. No HID access required.

import (
	"github.com/spf13/cobra"
)

var (
	bootSoundGetFlag bool
	bootSoundSetFlag string
)

var bootsoundCmd = &cobra.Command{
	Use:   "bootsound",
	Short: "Get or set the boot POST sound (ASUS firmware toggle)",
	Long: `Get or set the boot POST sound, an ASUS firmware toggle exposed by the kernel's
asus-armoury firmware-attributes interface. It is the named form of
'voltaire feature --get/--set boot_sound', kept for scripts; a device without
the toggle refuses it.

Values are the ones the firmware lists for the attribute (--list on the
feature command shows them); for this switch:
  0 — disabled (silent boot)
  1 — enabled (audible POST beep)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case bootSoundSetFlag != "":
			return setToggle("boot_sound", bootSoundSetFlag, "Boot sound set to %d\n")
		case bootSoundGetFlag:
			return runFeatureGet("boot_sound")
		}
		return cmd.Help()
	},
}

func init() {
	bootsoundCmd.Flags().BoolVar(&bootSoundGetFlag, "get", false, "Print the current boot sound setting")
	bootsoundCmd.Flags().StringVar(&bootSoundSetFlag, "set", "", "Set boot sound (0=off, 1=on)")
	rootCmd.AddCommand(bootsoundCmd)
}
