package cmd

// profile.go — "profile" subcommand: read or set the performance profile via
// the asus-wmi platform_profile sysfs interface. No HID access required.

import (
	"fmt"
	"os"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var (
	profileGetFlag bool
	profileSetFlag string
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Get or set the performance profile via asus-wmi",
	Long: `Get or set the performance profile via the Linux asus-wmi sysfs interface.

Profiles:
  quiet        — Silent/Eco mode (low power, low noise)
  balanced     — Balanced mode   (default)
  performance  — Turbo mode      (maximum performance)
  custom       — Re-apply saved custom fan curves and TDP from state`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !profileGetFlag && profileSetFlag == "" {
			return cmd.Help()
		}

		if profileSetFlag != "" {
			profile := strings.ToLower(profileSetFlag)
			switch profile {
			case "quiet", "balanced", "performance", "custom":
			default:
				return fmt.Errorf("unknown profile %q: must be quiet, balanced, performance, or custom", profileSetFlag)
			}

			if dryRunFlag {
				cli.DryRunProfile(profile)
				return nil
			}

			// All profiles (including custom) go through the daemon if running.
			if handled, err := api.SendProfileSet(profile); handled {
				if err != nil {
					return err
				}
				fmt.Printf("Performance profile set to %s\n", profile)
				return nil
			}

			// Direct path (no daemon): stock profiles write to sysfs and reset fan/TDP.
			if profile != "custom" {
				if err := cli.SetProfile(profile); err != nil {
					return fmt.Errorf("setting platform profile: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
				}
				// Reset fan curves and TDP to firmware defaults for stock profiles.
				_ = cli.ResetAllFanCurves()
				_ = cli.ResetTDP()
			}
			// "custom" without daemon: can't recall state, error out.
			if profile == "custom" {
				return fmt.Errorf("custom profile requires the daemon to recall saved settings; start the daemon first")
			}
			fmt.Printf("Performance profile set to %s\n", profile)
			return nil
		}

		// --get
		data, err := os.ReadFile(cli.FindProfilePath())
		if err != nil {
			return fmt.Errorf("reading platform profile: %w", err)
		}
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	},
}

func init() {
	profileCmd.Flags().BoolVar(&profileGetFlag, "get", false, "Print the active performance profile")
	profileCmd.Flags().StringVar(&profileSetFlag, "set", "", "Set the performance profile (quiet, balanced, performance)")
	rootCmd.AddCommand(profileCmd)
}
