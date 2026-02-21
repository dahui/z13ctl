package cmd

// off.go — "off" subcommand: turn all lighting off.

import (
	"fmt"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/cli"
	"github.com/dahui/z13ctl/internal/hid"

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
