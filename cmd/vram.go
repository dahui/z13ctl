package cmd

// vram.go — "vram" subcommand: read or set the iGPU memory reservation (the
// BIOS UMA frame buffer, Armoury Crate's "Free up memory") through the ATK WMI
// method via acpi_call. Root only, and not routed through the daemon: the
// daemon runs as the user and /proc/acpi/call stays root-only (see
// internal/cli/vram.go).

import (
	"fmt"

	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var (
	vramGetFlag  bool
	vramListFlag bool
	vramSetFlag  string
)

var vramCmd = &cobra.Command{
	Use:   "vram",
	Short: "Get or set the iGPU memory reservation (Armoury Crate \"Free up memory\")",
	Long: `Get or set how much system memory the firmware reserves for the iGPU at boot
(the BIOS "UMA Frame Buffer Size", shown in Armoury Crate as "Free up memory").

The sizes come from the firmware; on the 64 GB GZ302EA they are auto, 0.5G,
1G, 2G, 4G, 8G, 16G, 32G and 48G. A new size takes effect after a reboot.

Requires root and the acpi_call kernel module (package acpi-call-dkms; with
Secure Boot on, its signing key must be enrolled once).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !vramGetFlag && !vramListFlag && vramSetFlag == "" {
			return cmd.Help()
		}

		st, err := cli.ReadVRAM()
		if err != nil {
			return fmt.Errorf("reading iGPU memory setting: %w", err)
		}

		if vramSetFlag != "" {
			value, err := cli.ParseVRAMSize(vramSetFlag, st)
			if err != nil {
				return err
			}
			label := st.Label(value)

			if dryRunFlag {
				cli.DryRunVRAM(value, label)
				return nil
			}

			// Written even when it matches Current: Current is the size in use this
			// boot, so a match says nothing about a size stored by an earlier --set.
			if err = cli.SetVRAM(value); err != nil {
				return fmt.Errorf("setting iGPU memory: %w", err)
			}
			fmt.Printf("iGPU memory set to %s for the next boot (%s until then). Reboot for it to take effect.\n", label, st.Label(st.Current))
			return nil
		}

		if vramListFlag {
			for _, o := range st.Options {
				line := st.Label(o)
				if o == st.Current {
					line += "  (current)"
				}
				fmt.Println(line)
			}
			return nil
		}

		fmt.Println(st.Label(st.Current))
		return nil
	},
}

func init() {
	vramCmd.Flags().BoolVar(&vramGetFlag, "get", false, "Print the iGPU memory in use this boot")
	vramCmd.Flags().BoolVar(&vramListFlag, "list", false, "List the sizes the firmware offers")
	vramCmd.Flags().StringVar(&vramSetFlag, "set", "", "Set the iGPU memory for the next boot (auto, 512M, 0.5G, 1G, ... 48G)")
	vramCmd.MarkFlagsMutuallyExclusive("get", "list", "set")
	rootCmd.AddCommand(vramCmd)
}
