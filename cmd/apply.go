package cmd

// apply.go — "apply" subcommand: set color, mode, speed, and brightness.

import (
	"fmt"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/aura"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/driver"

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
	Example: `  voltaire apply --color cyan --brightness high
  voltaire apply --mode rainbow --speed slow
  voltaire apply --mode breathe --color hotpink --color2 blue
  voltaire apply --list-colors`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if listColorsFlag {
			cli.PrintColorList()
			return nil
		}
		if cmd.Flags().NFlag() == 0 {
			return cmd.Help()
		}

		caps := lightingCaps()
		if err := checkLightingZone(caps, deviceFlag); err != nil {
			return err
		}
		brightness, err := parseLightingBrightness(brightnessFlag, caps.BrightnessMax)
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
		modeInfo, ok := caps.Mode(modeFlag)
		if !ok {
			return fmt.Errorf("--mode: unknown mode %q (valid: %s)", modeFlag, modeNamesText(caps))
		}
		if !caps.HasSpeed(speedFlag) {
			return fmt.Errorf("--speed: unknown speed %q (valid: %s)", speedFlag, strings.Join(caps.Speeds, "|"))
		}

		if dryRunFlag {
			mode, merr := aura.ModeFromString(modeFlag)
			if merr != nil {
				return fmt.Errorf("--mode: %w", merr)
			}
			speed, serr := aura.SpeedFromString(speedFlag)
			if serr != nil {
				return fmt.Errorf("--speed: %w", serr)
			}
			cli.DryRunApply(lightingZoneBytes(), r, g, b, r2, g2, b2, mode, speed, uint8(brightness))
			return nil
		}

		color := fmt.Sprintf("%02X%02X%02X", r, g, b)
		color2 := fmt.Sprintf("%02X%02X%02X", r2, g2, b2)
		handled, err := api.SendApply(deviceFlag, color, color2, modeFlag, speedFlag, brightness)
		if !handled {
			ls := api.LightingState{Enabled: true, Mode: modeFlag, Color: color, Color2: color2,
				Speed: speedFlag, Brightness: brightness}
			err = withLighting(func(l driver.Lighting) error { return l.Apply(deviceFlag, ls) })
		}
		if err != nil {
			return err
		}
		fmt.Println(appliedText(deviceFlag, modeInfo))
		return nil
	},
}

// appliedText is apply's confirmation, naming only the inputs the mode uses —
// the mode's own description, not a list of modes that ignore colour.
func appliedText(zone string, m driver.LightingMode) string {
	parts := []string{"Applied:"}
	if zone != "" {
		parts[0] = "Applied: " + zone
	}
	parts = append(parts, "mode="+m.Name)
	if m.Color {
		parts = append(parts, "color="+cli.ColorDisplay(colorFlag))
	}
	if m.Color2 {
		parts = append(parts, "color2="+cli.ColorDisplay(color2Flag))
	}
	if m.Speed {
		parts = append(parts, "speed="+speedFlag)
	}
	parts = append(parts, "brightness="+brightnessFlag)
	return strings.Join(parts, " ")
}

func init() {
	applyCmd.Flags().StringVar(&colorFlag, "color", "FF0000",
		"Primary color: hex (RRGGBB) or name (e.g. red, cyan, hotpink). Ignored by cycle and rainbow. 000000 makes the firmware pick a color — use 'voltaire off' for no light. Use --list-colors for all names.")
	applyCmd.Flags().StringVar(&color2Flag, "color2", "000000",
		"Secondary color for breathe mode: hex (RRGGBB) or name. Use --list-colors for all names.")
	applyCmd.Flags().StringVar(&modeFlag, "mode", "static",
		"Lighting mode, one the device offers (static, breathe, cycle, rainbow, strobe on the Z13)")
	applyCmd.Flags().StringVar(&speedFlag, "speed", "normal",
		"Animation speed: slow|normal|fast. Ignored by modes that do not animate.")
	applyCmd.Flags().StringVar(&brightnessFlag, "brightness", "high",
		"Brightness: off|low|medium|high, or a number up to the device's maximum")
	applyCmd.Flags().BoolVar(&listColorsFlag, "list-colors", false,
		"List all supported color names with swatches")
	rootCmd.AddCommand(applyCmd)
}
