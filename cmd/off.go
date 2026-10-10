package cmd

// off.go — "off" subcommand: turn all lighting off.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/driver"

	"github.com/spf13/cobra"
)

var offCmd = &cobra.Command{
	Use:   "off",
	Short: "Turn all lighting off",
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := checkLightingZone(lightingCaps(), deviceFlag); err != nil {
			return err
		}
		if dryRunFlag {
			cli.DryRunOff()
			return nil
		}

		handled, err := api.SendOff(deviceFlag)
		if !handled {
			err = withLighting(func(l driver.Lighting) error { return l.Off(deviceFlag) })
		}
		if err != nil {
			return err
		}
		fmt.Println("Lighting off.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(offCmd)
}
