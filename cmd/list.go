package cmd

// list.go — "list" subcommand: enumerate matching hidraw devices.

import (
	"fmt"

	"github.com/dahui/z13ctl/internal/hid"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List matching hidraw devices",
	Run: func(_ *cobra.Command, _ []string) {
		devices := hid.ListDevices()
		if len(devices) == 0 {
			fmt.Println("No matching ASUS devices found.")
			return
		}
		for _, d := range devices {
			status := "[Aura/0x5d confirmed]"
			if d.OpenErr != "" {
				status = fmt.Sprintf("(can't open: %s)", d.OpenErr)
			} else if !d.HasAura {
				status = "[no Aura report]"
			}
			fmt.Printf("%-16s  %-10s  %s\n", d.Path, d.Name, status)
		}
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
