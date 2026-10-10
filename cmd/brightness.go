package cmd

// brightness.go — "brightness" subcommand: adjust brightness without changing the
// current lighting effect.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/driver"

	"github.com/spf13/cobra"
)

var brightnessCmd = &cobra.Command{
	Use:   "brightness <level>",
	Short: "Set brightness without changing the current lighting effect",
	Long: `Set the brightness level without altering the current lighting mode or color.

Levels are positions on the device's scale, or a number from 0 to its maximum:
  off     — all lighting disabled (power off)
  low     — minimum brightness
  medium  — mid brightness
  high    — maximum brightness (default for apply)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		caps := lightingCaps()
		if err := checkLightingZone(caps, deviceFlag); err != nil {
			return err
		}
		level, err := parseLightingBrightness(args[0], caps.BrightnessMax)
		if err != nil {
			return err
		}

		if dryRunFlag {
			cli.DryRunBrightness(uint8(level))
			return nil
		}

		handled, err := api.SendBrightness(deviceFlag, level)
		if !handled {
			err = withLighting(func(l driver.Lighting) error { return l.SetBrightness(deviceFlag, level) })
		}
		if err != nil {
			return err
		}
		fmt.Printf("Brightness set to %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(brightnessCmd)
}
