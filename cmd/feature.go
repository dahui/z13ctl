package cmd

// feature.go — "feature" subcommand: generic access to the device's firmware
// toggles by wire id. bootsound and paneloverdrive remain as named forms of
// the same controls; this command is how a toggle that exists only in device
// data — a future device's, or one added to the Z13 file later — is reachable
// without teaching the CLI a new subcommand.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
	"github.com/dahui/z13ctl/internal/driver"

	"github.com/spf13/cobra"
)

var (
	featureListFlag bool
	featureGetFlag  string
	featureSetFlag  string
)

var featureCmd = &cobra.Command{
	Use:   "feature",
	Short: "Get or set firmware toggles by id",
	Long: `Get or set the device's firmware toggles (BIOS switches) by their id.

The set of toggles comes from the device data this binary was assembled with;
--list shows what this machine offers. On the 2025 ROG Flow Z13 that is
boot_sound and panel_overdrive, which also have their own named commands
(bootsound, paneloverdrive) — feature is the generic form GUIs and scripts can
drive from the daemon's device-get document without knowing the device.

Boolean toggles take 0 (off) or 1 (on).`,
	Example: `  z13ctl feature --list
  z13ctl feature --get boot_sound
  z13ctl feature --set boot_sound=0`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case featureListFlag:
			return runFeatureList()
		case featureGetFlag != "":
			return runFeatureGet(featureGetFlag)
		case featureSetFlag != "":
			return runFeatureSet(featureSetFlag)
		}
		return cmd.Help()
	},
}

// deviceToggles returns the device's declared toggle set — from the daemon's
// device-get document when it is running (the authoritative runtime answer),
// else from the locally assembled device.
func deviceToggles() ([]api.ToggleInfo, error) {
	if handled, info, err := api.SendDeviceGet(); handled && err == nil {
		return info.Toggles, nil
	}
	hw, err := hardware()
	if err != nil {
		return nil, err
	}
	if hw.Toggles == nil {
		return nil, nil
	}
	var out []api.ToggleInfo
	for _, t := range hw.Toggles.List() {
		out = append(out, api.ToggleInfo{ID: t.ID, Label: t.Label, Kind: string(t.Kind)})
	}
	return out, nil
}

func runFeatureList() error {
	toggles, err := deviceToggles()
	if err != nil {
		return err
	}
	if len(toggles) == 0 {
		fmt.Println("No firmware toggles on this device")
		return nil
	}
	for _, t := range toggles {
		fmt.Printf("%-18s %-8s %s\n", t.ID, t.Kind, t.Label)
	}
	return nil
}

func runFeatureGet(id string) error {
	// Reads go to hardware for ground truth, as every --get does: another
	// process may have changed the setting.
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Toggles == nil {
		return fmt.Errorf("no firmware toggles on this device")
	}
	v, err := hw.Toggles.Get(id)
	if err != nil {
		// The driver's per-method "capability absent" sentinel means the id is
		// not one this device declares.
		if errors.Is(err, driver.ErrUnsupported) {
			return fmt.Errorf("unknown feature %q on this device (see 'z13ctl feature --list')", id)
		}
		return fmt.Errorf("reading %s: %w", id, err)
	}
	fmt.Println(v)
	return nil
}

func runFeatureSet(arg string) error {
	id, valStr, ok := strings.Cut(arg, "=")
	if !ok || id == "" {
		return fmt.Errorf("invalid --set %q: expected id=value, e.g. boot_sound=1", arg)
	}
	value, err := strconv.Atoi(strings.TrimSpace(valStr))
	if err != nil {
		return fmt.Errorf("invalid value %q for %s: must be an integer", valStr, id)
	}

	// Validate against the declared toggle set before writing anywhere, so an
	// unknown id or an out-of-range bool is refused with the same message the
	// daemon would produce.
	toggles, err := deviceToggles()
	if err != nil {
		return err
	}
	var spec *api.ToggleInfo
	for i := range toggles {
		if toggles[i].ID == id {
			spec = &toggles[i]
			break
		}
	}
	if spec == nil {
		return fmt.Errorf("unknown feature %q on this device (see 'z13ctl feature --list')", id)
	}
	if spec.Kind == string(driver.ToggleBool) && value != 0 && value != 1 {
		return fmt.Errorf("feature %s: value must be 0 or 1", id)
	}

	if dryRunFlag {
		cli.DryRunFeature(id, value)
		return nil
	}

	if handled, sendErr := api.SendFeatureSet(id, value); handled {
		if sendErr != nil {
			return sendErr
		}
		fmt.Printf("Feature %s set to %d\n", id, value)
		return nil
	}

	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Toggles == nil {
		return fmt.Errorf("no firmware toggles on this device")
	}
	if err := hw.Toggles.Set(id, value); err != nil {
		return fmt.Errorf("setting %s: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", id, err)
	}
	fmt.Printf("Feature %s set to %d\n", id, value)
	return nil
}

func init() {
	featureCmd.Flags().BoolVar(&featureListFlag, "list", false, "List the device's firmware toggles")
	featureCmd.Flags().StringVar(&featureGetFlag, "get", "", "Print a toggle's current value by id")
	featureCmd.Flags().StringVar(&featureSetFlag, "set", "", "Set a toggle: id=value, e.g. boot_sound=1")
	rootCmd.AddCommand(featureCmd)
}
