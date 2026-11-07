package cmd

// vram.go — "vram" subcommand: read or set the iGPU memory reservation (the
// BIOS UMA frame buffer, Armoury Crate's "Free up memory") through the ATK WMI
// method via acpi_call. As root it calls the firmware itself; otherwise it asks
// the z13ctl-vram helper (see internal/cli/vramhelper.go).

import (
	"fmt"
	"os"

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

Needs the acpi_call kernel module (package acpi-call-dkms; with Secure Boot
on, its signing key must be enrolled once). Without sudo it goes through the
z13ctl-vram helper service, which 'z13ctl setup' and the packages install.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !vramGetFlag && !vramListFlag && vramSetFlag == "" {
			return cmd.Help()
		}

		info, err := cli.VRAMGet()
		if err != nil {
			return fmt.Errorf("reading iGPU memory setting: %w", err)
		}

		if vramSetFlag != "" {
			label, err := cli.MatchVRAMOption(vramSetFlag, info)
			if err != nil {
				return err
			}
			if dryRunFlag {
				return dryRunVRAM(label)
			}
			after, err := cli.VRAMSet(label)
			if err != nil {
				return fmt.Errorf("setting iGPU memory: %w", err)
			}
			if after.Next == "" {
				fmt.Printf("iGPU memory set to %s, the size already in use; any other size set since boot is cancelled.\n", after.Current)
				return nil
			}
			fmt.Printf("iGPU memory set to %s for the next boot (%s until then). Reboot for it to take effect.\n", after.Next, after.Current)
			return nil
		}

		if vramListFlag {
			for _, o := range info.Options {
				line := o
				switch o {
				case info.Current:
					line += "  (current)"
				case info.Next:
					line += "  (next boot)"
				}
				fmt.Println(line)
			}
			return nil
		}

		if info.Next != "" {
			fmt.Printf("%s (%s after reboot)\n", info.Current, info.Next)
			return nil
		}
		fmt.Println(info.Current)
		return nil
	},
}

// dryRunVRAM shows the exact firmware call when root, where z13ctl would make
// it itself; otherwise the request the helper would receive.
func dryRunVRAM(label string) error {
	if os.Geteuid() != 0 {
		cli.DryRunVRAMHelper(label)
		return nil
	}
	st, err := cli.ReadVRAM()
	if err != nil {
		return fmt.Errorf("reading iGPU memory setting: %w", err)
	}
	value, err := cli.ParseVRAMSize(label, st)
	if err != nil {
		return err
	}
	cli.DryRunVRAM(value, label)
	return nil
}

func init() {
	vramCmd.Flags().BoolVar(&vramGetFlag, "get", false, "Print the iGPU memory in use this boot, and the size stored for the next")
	vramCmd.Flags().BoolVar(&vramListFlag, "list", false, "List the sizes the firmware offers")
	vramCmd.Flags().StringVar(&vramSetFlag, "set", "", "Set the iGPU memory for the next boot (auto, 512M, 0.5G, 1G, ... 48G)")
	vramCmd.MarkFlagsMutuallyExclusive("get", "list", "set")
	rootCmd.AddCommand(vramCmd)
}
