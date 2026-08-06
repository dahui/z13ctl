package cmd

// profile.go — "profile" subcommand: read or set the performance profile, and
// manage named custom profiles.
//
// The three firmware profiles are written to platform_profile via asus-wmi. A
// custom profile is z13ctl's own: a named set of fan curve, TDP and undervolt
// settings that it applies itself and never writes to platform_profile. The
// firmware names are reserved, so selecting one always reaches the firmware
// profile and can never be shadowed by a custom profile.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var (
	profileGetFlag    bool
	profileSetFlag    string
	profileListFlag   bool
	profileCreateFlag string
	profileSaveAsFlag string
	profileDeleteFlag string
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Get or set the performance profile, or manage custom profiles",
	Long: `Get or set the performance profile, or manage named custom profiles.

Firmware profiles (written to platform_profile via asus-wmi):
  quiet        — Silent/Eco mode (low power, low noise)
  balanced     — Balanced mode   (default)
  performance  — Turbo mode      (maximum performance)

Custom profiles are z13ctl's own: a named set of fan curve, TDP and undervolt
settings that z13ctl applies itself and never writes to platform_profile, so
profile ownership stays with your desktop. "custom" is the profile created
automatically by the first 'fancurve --set', 'tdp --set' or 'undervolt --set'
made while a firmware profile is active; --create makes more.

Setting a fan curve, TDP or undervolt edits the profile you are running, and
the change takes effect and persists immediately — there is no save step.
'--profile <name>' on those commands edits a profile you are NOT running,
which is how you build the profile that 'z13ctl autoswitch' selects on battery
without applying it first.

The firmware profile names are reserved and cannot name a custom profile.
Custom profiles require the daemon, which is what recalls and applies them.

Examples:
  z13ctl profile --set performance
  z13ctl profile --create battery-uv
  z13ctl tdp --set 35 --profile battery-uv
  z13ctl profile --set battery-uv
  z13ctl profile --list`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case profileSetFlag != "":
			return runProfileSet()
		case profileListFlag:
			return runProfileList()
		case profileCreateFlag != "":
			return runProfileCreate()
		case profileSaveAsFlag != "":
			return runProfileSaveAs()
		case profileDeleteFlag != "":
			return runProfileDelete()
		case profileGetFlag:
			return runProfileGet()
		}
		return cmd.Help()
	},
}

func runProfileSet() error {
	profile := strings.ToLower(profileSetFlag)

	if dryRunFlag {
		cli.DryRunProfile(profile)
		return nil
	}

	// Every profile goes through the daemon when it is running. Name validation
	// lives there: only the daemon knows which custom profiles are saved, and
	// it returns a message naming the profile.
	if handled, err := api.SendProfileSet(profile); handled {
		if err != nil {
			return err
		}
		fmt.Printf("Performance profile set to %s\n", profile)
		return nil
	}

	// No daemon: only firmware profiles can be applied, since recalling a custom
	// profile means reading daemon state.
	if !cli.IsStockProfile(profile) {
		return fmt.Errorf("custom profiles require the daemon to recall their saved settings; start the daemon first\n"+
			"  (%q is not one of quiet, balanced, performance)", profile)
	}

	// Direct path (no daemon): write platform_profile, restore that profile's
	// stock PPT, and only then release the fans to firmware auto. The firmware
	// manages fan curves for stock profiles but does not re-apply PPT, so a
	// previously set custom TDP would otherwise persist across the switch. Fans
	// are released last so they are never dropped to auto while a high custom
	// TDP is still in force — the same order as the daemon and 'tdp --reset'.
	if err := cli.SetProfile(profile); err != nil {
		return fmt.Errorf("setting platform profile: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	restoreStockPPT(profile)
	if err := cli.ResetAllFanCurves(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to reset fan curves: %v\n", err)
	}
	fmt.Printf("Performance profile set to %s\n", profile)
	return nil
}

func runProfileGet() error {
	// Prefer the daemon: platform_profile is never "custom", so sysfs alone
	// cannot report which custom profile is running.
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil && st.Profile != "" {
		fmt.Println(st.Profile)
		return nil
	}
	data, err := os.ReadFile(cli.FindProfilePath())
	if err != nil {
		return fmt.Errorf("reading platform profile: %w", err)
	}
	fmt.Println(strings.TrimSpace(string(data)))
	return nil
}

// profileRow mirrors the daemon's profile-list entry.
type profileRow struct {
	api.CustomProfile
	Active bool `json:"active"`
}

func runProfileList() error {
	handled, value, err := api.SendProfileList()
	if !handled {
		return fmt.Errorf("listing custom profiles requires the daemon; start it first")
	}
	if err != nil {
		return err
	}
	var rows []profileRow
	if err := json.Unmarshal([]byte(value), &rows); err != nil {
		return fmt.Errorf("parsing profile list: %w", err)
	}

	fmt.Println("Firmware profiles: quiet, balanced, performance")
	fmt.Println("Custom profiles:")
	for _, r := range rows {
		marker := " "
		if r.Active {
			marker = "*"
		}
		fmt.Printf(" %s %-20s %s\n", marker, r.Name, describeProfile(r.CustomProfile))
	}
	return nil
}

// describeProfile summarises which subsystems a profile controls. An empty
// profile is worth saying out loud: it cannot be activated.
func describeProfile(p api.CustomProfile) string {
	var parts []string
	if p.FanCurve != nil {
		parts = append(parts, "fan curve")
	}
	if p.TDP != nil {
		parts = append(parts, fmt.Sprintf("%dW", p.TDP.PL1SPL))
	}
	if p.Undervolt != nil {
		parts = append(parts, fmt.Sprintf("CO %d", p.Undervolt.CPUCO))
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, ", ")
}

func runProfileCreate() error {
	// Validated as typed: see handleProfileCreate on why the case is not folded.
	name := strings.TrimSpace(profileCreateFlag)
	if err := cli.ValidateProfileName(name); err != nil {
		return err
	}
	if dryRunFlag {
		cli.DryRunProfileCreate(name)
		return nil
	}
	handled, err := api.SendProfileCreate(name)
	if !handled {
		return fmt.Errorf("custom profiles require the daemon; start it first")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Created empty profile %s\n", name)
	fmt.Printf("  Add settings with: z13ctl tdp --set 35 --profile %s\n", name)
	return nil
}

func runProfileSaveAs() error {
	name := strings.TrimSpace(profileSaveAsFlag)
	if err := cli.ValidateProfileName(name); err != nil {
		return err
	}
	if dryRunFlag {
		cli.DryRunProfileSave(name)
		return nil
	}
	handled, err := api.SendProfileSave(name)
	if !handled {
		return fmt.Errorf("custom profiles require the daemon; start it first")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Copied the active profile to %s\n", name)
	return nil
}

func runProfileDelete() error {
	name := strings.ToLower(strings.TrimSpace(profileDeleteFlag))
	if dryRunFlag {
		cli.DryRunProfileDelete(name)
		return nil
	}
	handled, err := api.SendProfileDelete(name)
	if !handled {
		return fmt.Errorf("custom profiles require the daemon; start it first")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Deleted profile %s\n", name)
	return nil
}

func init() {
	profileCmd.Flags().BoolVar(&profileGetFlag, "get", false, "Print the active profile")
	profileCmd.Flags().StringVar(&profileSetFlag, "set", "", "Set the profile (quiet, balanced, performance, or a custom profile name)")
	profileCmd.Flags().BoolVar(&profileListFlag, "list", false, "List saved custom profiles")
	profileCmd.Flags().StringVar(&profileCreateFlag, "create", "", "Create an empty custom profile (does not activate it)")
	profileCmd.Flags().StringVar(&profileSaveAsFlag, "save-as", "", "Copy the active custom profile under a new name")
	profileCmd.Flags().StringVar(&profileDeleteFlag, "delete", "", "Delete a saved custom profile")
	rootCmd.AddCommand(profileCmd)
}
