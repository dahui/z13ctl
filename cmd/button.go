package cmd

// button.go — "button" subcommand: which hardware button opens voltaire.
//
// The choices are the device file's buttons and nothing else — a user picks
// among them, the way G-Helper offers its fixed list of vendor keys, and
// nothing reads a keyboard to learn a key. The daemon stores the choice and
// consults it on every press, so --set needs the daemon; --list and --get fall
// back to the device file without one.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"

	"github.com/spf13/cobra"
)

var (
	buttonListFlag bool
	buttonGetFlag  bool
	buttonSetFlag  string
)

var buttonCmd = &cobra.Command{
	Use:   "button",
	Short: "Choose which hardware button opens voltaire",
	Long: `Choose which hardware button opens voltaire: one press opens the quickbar,
a quick second press the full window (swappable in the window's Settings).

The choices are the buttons this machine's device file declares (--list), plus
"none" for no button and "default" for the device's own default. They are
fixed by the device file on purpose: voltaire never reads a keyboard to learn
a key, so it cannot see what you type.

The daemon keeps the choice, so --set needs it running.`,
	Example: `  voltaire button --list
  voltaire button --set none
  voltaire button --set default`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case buttonSetFlag != "":
			return runButtonSet(buttonSetFlag)
		case buttonListFlag:
			return runButtonList()
		case buttonGetFlag:
			return runButtonGet()
		}
		return cmd.Help()
	},
}

// buttonChoices is the device's buttons and the button that opens voltaire:
// the daemon's resolved answer when it is up (daemonUp), the device file's
// default otherwise.
func buttonChoices() (buttons []api.ButtonInfo, current string, daemonUp bool, err error) {
	handled, info, infoErr := api.SendDeviceGet()
	if handled && infoErr == nil && info != nil {
		buttons = info.HardwareButtons
		if h, st, stErr := api.SendGetState(); h && stErr == nil && st != nil {
			return buttons, st.DrawerButton, true, nil
		}
	}
	hw, err := hardware()
	if err != nil {
		return nil, "", false, err
	}
	buttons = buttons[:0]
	for _, b := range hw.ButtonList {
		buttons = append(buttons, api.ButtonInfo{ID: b.ID, Label: b.Label, Default: b.ID == hw.DefaultButton})
	}
	return buttons, defaultOrNone(hw), false, nil
}

// defaultOrNone is the device's default button, or "none" when it has none.
func defaultOrNone(hw *device.Device) string {
	if hw.DefaultButton == "" {
		return api.DrawerButtonNone
	}
	return hw.DefaultButton
}

func runButtonList() error {
	buttons, current, daemonUp, err := buttonChoices()
	if err != nil {
		return err
	}
	if len(buttons) == 0 {
		fmt.Println("This device declares no hardware buttons.")
		return nil
	}
	fmt.Println("Hardware buttons (* opens voltaire):")
	for _, b := range buttons {
		mark := " "
		if b.ID == current {
			mark = "*"
		}
		note := ""
		if b.Default {
			note = " (default)"
		}
		fmt.Printf("  %s %-20s %s%s\n", mark, b.ID, b.Label, note)
	}
	if current == api.DrawerButtonNone {
		fmt.Println("  No button opens voltaire.")
	}
	if !daemonUp {
		fmt.Println("(daemon not running: showing the device default, not your choice)")
	}
	return nil
}

func runButtonGet() error {
	buttons, current, daemonUp, err := buttonChoices()
	if err != nil {
		return err
	}
	if len(buttons) == 0 {
		fmt.Println("This device declares no hardware buttons.")
		return nil
	}
	label := "none"
	for _, b := range buttons {
		if b.ID == current {
			label = b.ID + " (" + b.Label + ")"
		}
	}
	suffix := ""
	if !daemonUp {
		suffix = " (daemon not running: the device default)"
	}
	fmt.Printf("Opens voltaire: %s%s\n", label, suffix)
	return nil
}

func runButtonSet(choice string) error {
	if dryRunFlag {
		fmt.Println("=== DRY RUN (no daemon request) ===")
		fmt.Printf("Would ask the daemon to make %q the button that opens voltaire\n", choice)
		return nil
	}
	handled, err := api.SendButtonSet(choice)
	if !handled {
		return fmt.Errorf("the daemon keeps this choice, and it is not running (systemctl --user start voltaire)")
	}
	if err != nil {
		return err
	}
	switch choice {
	case api.DrawerButtonNone:
		fmt.Println("No button opens voltaire now.")
	case api.DrawerButtonDefault:
		fmt.Println("The device's default button opens voltaire again.")
	default:
		fmt.Printf("%s now opens voltaire.\n", choice)
	}
	return nil
}

func init() {
	buttonCmd.Flags().BoolVar(&buttonListFlag, "list", false, "List the buttons this device offers, marking the one that opens voltaire")
	buttonCmd.Flags().BoolVar(&buttonGetFlag, "get", false, "Print the button that opens voltaire")
	buttonCmd.Flags().StringVar(&buttonSetFlag, "set", "", `Button id from --list, "none", or "default"`)
	buttonCmd.MarkFlagsMutuallyExclusive("list", "get", "set")
	rootCmd.AddCommand(buttonCmd)
}
