// Package cmd implements the z13ctl CLI subcommands via Cobra.
// Each file in this package defines exactly one subcommand.
// CLI support utilities (color parsing, dry-run display) live in internal/cli.
//
// root.go — Cobra root command for z13ctl.
package cmd

import (
	"github.com/spf13/cobra"
)

// Version is the current release. Override at build time:
//
//	go build -ldflags "-X z13ctl/cmd.Version=1.2.3" .
var Version = "1.0.0-beta"

var (
	deviceFlag   string
	dryRunFlag   bool
	noButtonFlag bool
)

var rootCmd = &cobra.Command{
	Use:     "z13ctl",
	Version: Version,
	Short:   "System control for the ASUS ROG Flow Z13",
	Long: `z13ctl — system control for the 2025 ASUS ROG Flow Z13

Controls keyboard and lightbar RGB via Linux hidraw, performance profile and
battery charge limit via asus-wmi sysfs, and boot sound and panel overdrive
via asus-armoury firmware-attributes.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&deviceFlag, "device", "", "Target device: keyboard, lightbar, or a hidraw path (default: all)")
	rootCmd.PersistentFlags().BoolVar(&dryRunFlag, "dry-run", false, "Preview changes without applying them")
	rootCmd.PersistentFlags().BoolVar(&noButtonFlag, "no-button", false, "Disable the Armoury Crate button watcher (daemon only)")
}
