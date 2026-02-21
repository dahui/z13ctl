// Package cmd implements the z13ctl CLI subcommands via Cobra.
// Each file in this package defines exactly one subcommand.
// CLI support utilities (color parsing, dry-run display) live in internal/cli.
//
// root.go — Cobra root command for z13ctl.
package cmd

//go:generate go run ../tools/gendocs ../docs

import (
	"github.com/spf13/cobra"
)

// Version is the current release. Override at build time:
//
//	go build -ldflags "-X z13ctl/cmd.Version=1.2.3" .
var Version = "1.0.0-beta"

var (
	deviceFlag string
	dryRunFlag bool
)

var rootCmd = &cobra.Command{
	Use:     "z13ctl",
	Version: Version,
	Short:   "ROG Flow Z13 RGB lighting control",
	Long: `z13ctl — ROG Flow Z13 RGB lighting control

Controls keyboard and lightbar RGB on the 2025 ASUS ROG Flow Z13 (USB 0b05:18c6)
via Linux hidraw, using the Aura HID protocol reverse-engineered from g-helper.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// GetRootCmd returns the root Cobra command, for use by documentation generators.
func GetRootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().StringVar(&deviceFlag, "device", "", "Target device: keyboard, lightbar, or a hidraw path (default: all)")
	rootCmd.PersistentFlags().BoolVar(&dryRunFlag, "dry-run", false, "Print packets without sending to device")
}
