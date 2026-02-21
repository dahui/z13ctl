// brightness.go — "brightness" subcommand: adjust brightness without changing the
// current lighting effect.
package cmd

import (
	"fmt"
	"strings"

	"z13ctl/internal/aura"
	"z13ctl/internal/cli"
	"z13ctl/internal/hid"

	"github.com/spf13/cobra"
)

var brightnessCmd = &cobra.Command{
	Use:   "brightness <level>",
	Short: "Set brightness without changing the current lighting effect",
	Long: `Set the brightness level without altering the current lighting mode or color.

Levels:
  off     — all lighting disabled (power off)
  low     — minimum brightness
  medium  — mid brightness
  high    — maximum brightness (default for apply)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		level, err := cli.ParseBrightness(args[0])
		if err != nil {
			return err
		}

		if dryRunFlag {
			cli.DryRunBrightness(level)
			return nil
		}

		dev, err := hid.FindDevice(deviceFlag)
		if err != nil {
			return err
		}
		defer dev.Close()

		if err := aura.Init(dev); err != nil {
			return err
		}
		if err := aura.SetPower(dev, level > 0); err != nil {
			return err
		}
		if err := aura.SetBrightness(dev, level); err != nil {
			return err
		}
		fmt.Printf("Brightness set to %s (%s)\n", args[0], strings.Join(dev.Descriptions(), ", "))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(brightnessCmd)
}
