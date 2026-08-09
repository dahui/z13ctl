package cmd

// off.go — "off" subcommand: turn all lighting off.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/aura"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/hid"

	"github.com/spf13/cobra"
)

var offCmd = &cobra.Command{
	Use:   "off",
	Short: "Turn all lighting off",
	RunE: func(_ *cobra.Command, _ []string) error {
		if dryRunFlag {
			cli.DryRunOff()
			return nil
		}

		if handled, err := api.SendOff(deviceFlag); handled {
			if err != nil {
				return err
			}
			fmt.Println("Lighting off.")
			return nil
		}

		dev, err := hid.FindDevice(deviceFlag)
		if err != nil {
			return err
		}
		defer dev.Close()

		if err := aura.TurnOff(dev); err != nil {
			return err
		}
		fmt.Println("Lighting off.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(offCmd)
}
