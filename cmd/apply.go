// apply.go — "apply" subcommand: set color, mode, speed, and brightness.
package cmd

import (
	"fmt"
	"strings"

	"z13ctl/internal/aura"
	"z13ctl/internal/cli"
	"z13ctl/internal/hid"

	"github.com/spf13/cobra"
)

var (
	colorFlag      string
	color2Flag     string
	modeFlag       string
	speedFlag      string
	brightnessFlag string
	listColorsFlag bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a lighting effect",
	Example: `  z13ctl apply --color cyan --brightness high
  z13ctl apply --color 00FF88 --mode rainbow --speed slow
  z13ctl apply --mode breathe --color hotpink --color2 blue
  z13ctl apply --list-colors`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if listColorsFlag {
			cli.PrintColorList()
			return nil
		}
		if cmd.Flags().NFlag() == 0 {
			return cmd.Help()
		}

		brightness, err := cli.ParseBrightness(brightnessFlag)
		if err != nil {
			return fmt.Errorf("--brightness: %w", err)
		}
		r, g, b, err := cli.ParseColor(colorFlag)
		if err != nil {
			cli.PrintColorList()
			return fmt.Errorf("--color: %w", err)
		}
		r2, g2, b2, err := cli.ParseColor(color2Flag)
		if err != nil {
			cli.PrintColorList()
			return fmt.Errorf("--color2: %w", err)
		}
		mode, err := aura.ModeFromString(modeFlag)
		if err != nil {
			return fmt.Errorf("--mode: %w", err)
		}
		speed, err := aura.SpeedFromString(speedFlag)
		if err != nil {
			return fmt.Errorf("--speed: %w", err)
		}

		if dryRunFlag {
			cli.DryRunApply(r, g, b, r2, g2, b2, mode, speed, brightness)
			return nil
		}

		dev, err := hid.FindDevice(deviceFlag)
		if err != nil {
			return err
		}
		defer dev.Close()

		if err := aura.Apply(dev, mode, r, g, b, r2, g2, b2, speed, brightness); err != nil {
			return err
		}

		parts := []string{
			"Applied: " + strings.Join(dev.Descriptions(), ", "),
			"mode=" + modeFlag,
		}
		// cycle and rainbow pick colors automatically; color is not meaningful.
		if mode != aura.ModeCycle && mode != aura.ModeRainbow {
			parts = append(parts, "color="+cli.ColorDisplay(colorFlag))
		}
		// color2 is only used by breathe (dual-color).
		if mode == aura.ModeBreathe {
			parts = append(parts, "color2="+cli.ColorDisplay(color2Flag))
		}
		// speed is not meaningful for static.
		if mode != aura.ModeStatic {
			parts = append(parts, "speed="+speedFlag)
		}
		parts = append(parts, "brightness="+brightnessFlag)
		fmt.Println(strings.Join(parts, " "))
		return nil
	},
}

func init() {
	applyCmd.Flags().StringVar(&colorFlag, "color", "FF0000",
		"Primary color: hex (RRGGBB) or name (e.g. red, cyan, hotpink). Use --list-colors for all names.")
	applyCmd.Flags().StringVar(&color2Flag, "color2", "000000",
		"Secondary color for breathe mode: hex (RRGGBB) or name. Use --list-colors for all names.")
	applyCmd.Flags().StringVar(&modeFlag, "mode", "static",
		"Lighting mode: static|breathe|cycle|rainbow|star|rain|strobe|comet|flash")
	applyCmd.Flags().StringVar(&speedFlag, "speed", "normal",
		"Animation speed: slow|normal|fast")
	applyCmd.Flags().StringVar(&brightnessFlag, "brightness", "high",
		"Brightness level: off|low|medium|high")
	applyCmd.Flags().BoolVar(&listColorsFlag, "list-colors", false,
		"List all supported color names with swatches")
	rootCmd.AddCommand(applyCmd)
}
