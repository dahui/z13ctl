package cmd

// bootsound.go — "bootsound" subcommand: read or set the POST boot sound via
// the ASUS asus-armoury firmware-attributes sysfs interface. No HID access required.

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
	bootSoundGetFlag bool
	bootSoundSetFlag string
)

var bootsoundCmd = &cobra.Command{
	Use:   "bootsound",
	Short: "Get or set the boot POST sound via asus-armoury firmware attributes",
	Long: `Get or set the boot POST sound via the Linux asus-armoury firmware-attributes
sysfs interface.

Values:
  0 — disabled (silent boot)
  1 — enabled (audible POST beep)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !bootSoundGetFlag && bootSoundSetFlag == "" {
			return cmd.Help()
		}

		if bootSoundSetFlag != "" {
			value, err := strconv.Atoi(bootSoundSetFlag)
			if err != nil || (value != 0 && value != 1) {
				return fmt.Errorf("invalid value %q: must be 0 or 1", bootSoundSetFlag)
			}

			if dryRunFlag {
				cli.DryRunBootSound(value)
				return nil
			}

			if handled, err := api.SendBootSoundSet(value); handled {
				if err != nil {
					return err
				}
				fmt.Printf("Boot sound set to %d\n", value)
				return nil
			}

			if err := cli.SetBootSound(value); err != nil {
				return fmt.Errorf("setting boot sound: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
			}
			fmt.Printf("Boot sound set to %d\n", value)
			return nil
		}

		// --get
		data, err := os.ReadFile(cli.FindBootSoundPath())
		if err != nil {
			return fmt.Errorf("reading boot sound: %w", err)
		}
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	},
}

func init() {
	bootsoundCmd.Flags().BoolVar(&bootSoundGetFlag, "get", false, "Print the current boot sound setting")
	bootsoundCmd.Flags().StringVar(&bootSoundSetFlag, "set", "", "Set boot sound (0=off, 1=on)")
	rootCmd.AddCommand(bootsoundCmd)
}
