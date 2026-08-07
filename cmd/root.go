// Package cmd implements the z13ctl CLI subcommands via Cobra.
// Each file in this package defines exactly one subcommand.
// CLI support utilities (color parsing, dry-run display) live in internal/cli.
//
// root.go — Cobra root command for z13ctl.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dahui/z13ctl/api"
)

// profileFlagUsage is shared by the fancurve, tdp, and undervolt commands so
// the three describe --profile identically.
const profileFlagUsage = "Store the setting in this custom profile instead of applying it to the active one"

// ensureProfileTargetSupported must be called BEFORE sending any --profile
// edit, and is the guard against the one dangerous version skew this feature
// has.
//
// The "profile" request field is additive, so a daemon from before named
// profiles existed unmarshals the request, ignores the field it does not know,
// and applies the setting to the running machine — then answers ok. The CLI
// would report "stored in profile X (not applied)" over a live TDP change.
// Probing profile-list is what distinguishes the two: an older daemon answers
// "unknown command".
//
// Cost is one extra round trip, and only on the --profile path.
func ensureProfileTargetSupported(profile string) error {
	if profile == "" {
		return nil
	}
	handled, _, err := api.SendProfileList()
	if !handled {
		return requireDaemonForProfile(profile)
	}
	if err != nil {
		return fmt.Errorf("the running daemon does not support --profile, and would apply this setting to the\n"+
			"  machine instead of storing it in %s; restart it after upgrading\n"+
			"  (systemctl --user restart z13ctl)\n"+
			"  daemon said: %w", profile, err)
	}
	return nil
}

// requireDaemonForProfile turns "--profile with no daemon" into an explanation
// rather than a silent write to the running machine. Editing a profile that is
// not active means editing daemon state, which only the daemon holds; without
// this the direct fallback would apply the setting to hardware, which is the
// opposite of what --profile asks for.
func requireDaemonForProfile(profile string) error {
	if profile == "" {
		return nil
	}
	return fmt.Errorf("--profile %s requires the daemon, which stores custom profiles; start it first", profile)
}

// profileEditMessage adds a line saying the setting was stored rather than
// applied, so a --profile edit never looks like it changed the machine.
func profileEditMessage(profile, applied string) string {
	if profile == "" {
		return applied + "\n"
	}
	return fmt.Sprintf("Stored in profile %s (not applied — activate it with 'z13ctl profile --set %s')\n", profile, profile)
}

// Version is the current release. Override at build time:
//
//	go build -ldflags "-X z13ctl/cmd.Version=1.2.3" .
var Version = "1.0.0-beta"

var (
	deviceFlag         string
	dryRunFlag         bool
	noButtonFlag       bool
	noSleepReleaseFlag bool
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
	rootCmd.PersistentFlags().BoolVar(&noSleepReleaseFlag, "no-sleep-release", false,
		"Keep the custom fan curve through sleep instead of handing the fans back to the firmware (daemon only)")
}
