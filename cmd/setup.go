package cmd

// setup.go — "setup" subcommand: grant voltaire's group what this machine's
// device file needs. What is granted is internal/perms' decision; this file
// finds the machine, writes the files and runs udevadm and systemctl.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/perms"
	"github.com/dahui/voltaire/v2/internal/version"
)

const serviceName = "voltaire-perms.service"

// Where setup writes. Vars so tests can point them at a temporary directory.
var (
	rulesPath   = "/etc/udev/rules.d/99-voltaire.rules"
	servicePath = "/etc/systemd/system/" + serviceName
)

// The pre-rename artifact locations. setup removes them — the rules file only
// after verifying it is ours (see cleanupOldArtifacts), the unit with an
// explicit disable first: z13ctl-perms.service is Type=oneshot with
// RemainAfterExit=yes, so without the disable it stays active holding stale
// grants until the next reboot.
var (
	oldRulesPath   = "/etc/udev/rules.d/99-z13ctl.rules"
	oldServicePath = "/etc/systemd/system/z13ctl-perms.service"
)

const oldServiceName = "z13ctl-perms.service"

var (
	setupGroup       string
	permsOnlyFlag    bool
	setupMachineFlag string
	setupRemoveFlag  bool

	// Whether --group and --machine were given: when not, an earlier run's
	// choice (read back from the boot unit) stands.
	setupGroupSet, setupMachineSet bool

	ipPassthroughFlag bool // --inputplumber-passthrough
	ipPassthroughSet  bool // given at all, either way
)

// The seams that keep setup's tests off the machine: commands, the root
// check, DMI, and the two passes that change permissions. Tests replace them.
var (
	runCommand = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
	isRoot       = func() bool { return os.Getuid() == 0 }
	matchConfig  = device.MatchConfig
	applyGrants  = perms.Apply
	revertGrants = perms.Revert
)

// note runs a command whose failure must not stop setup — it has already
// written its files, and a chroot or image build has no udevd or systemd to
// answer — and says so when it fails.
func note(name string, args ...string) bool {
	if out, err := runCommand(name, args...); err != nil {
		fmt.Fprintf(os.Stderr, "note: %s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
		return false
	}
	return true
}

// setupMachine is the device file setup grants for, and whether it was
// matched to this machine by DMI (a keyboard_node grant applies only then).
type setupMachine struct {
	cfg      device.Config
	dmiMatch bool
}

// resolveSetupMachine finds the device file: the one named by --machine, or
// the one matching DMI. An unmatched machine is device.ErrUnknownDevice; setup
// grants nothing then rather than borrowing another machine's file, which
// would hand out power and fan attributes for hardware that is not there.
func resolveSetupMachine(id string) (setupMachine, error) {
	dmi, dmiErr := matchConfig()
	if id == "" {
		if dmiErr != nil {
			return setupMachine{}, dmiErr
		}
		return setupMachine{cfg: dmi, dmiMatch: true}, nil
	}
	c, err := device.ConfigByID(id)
	if err != nil {
		return setupMachine{}, err
	}
	return setupMachine{cfg: c, dmiMatch: dmiErr == nil && dmi.Device.ID == c.Device.ID}, nil
}

// lookupGID resolves the group before anything is written: a group that does
// not exist would otherwise produce rules udev rejects.
func lookupGID(group string) (int, error) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, fmt.Errorf("group %q: %w", group, err)
	}
	return strconv.Atoi(g.Gid)
}

// knownHIDRaw is every device file's hidraw grants: the boot pass resets a
// stale grant on another interface of any of those devices.
func knownHIDRaw() []perms.HIDRaw {
	configs, _ := device.Configs() // validated by every caller's resolve step first
	return perms.Known(configs)
}

// setupExecutable is the binary the boot unit runs: the one setup was run as.
// Package scripts call /usr/bin/voltaire explicitly so a stale source-install
// copy in /usr/local/bin never ends up here.
func setupExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "/usr/bin/voltaire"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.HasPrefix(exe, "/home/") || strings.HasPrefix(exe, "/tmp/") {
		fmt.Fprintf(os.Stderr, "note: the boot unit will run %s; install voltaire system-wide so it survives a cleanup of that directory\n", exe)
	}
	return exe
}

// keepEarlierChoices carries --group and --machine over from the boot unit
// an earlier run wrote, unless given again. Every package upgrade runs a
// plain "voltaire setup", which would otherwise reset a custom group to
// "users" and drop a --machine choice (failing outright on a machine DMI
// does not match).
func keepEarlierChoices() {
	data, err := os.ReadFile(servicePath)
	if err != nil {
		return
	}
	group, machine := perms.ParseUnit(string(data))
	if !setupGroupSet && group != "" && group != setupGroup {
		setupGroup = group
		fmt.Printf("Keeping group %s from the existing setup (--group changes it).\n", group)
	}
	if !setupMachineSet && machine != "" && setupMachineFlag == "" {
		setupMachineFlag = machine
		fmt.Printf("Keeping --machine %s from the existing setup.\n", machine)
	}
}

func runSetup() error {
	if setupRemoveFlag {
		return runSetupRemove()
	}
	if !permsOnlyFlag {
		keepEarlierChoices() // the boot pass is told both by its unit
	}
	m, err := resolveSetupMachine(setupMachineFlag)
	if errors.Is(err, device.ErrUnknownDevice) {
		msg := fmt.Sprintf("%v: nothing granted.\n"+
			"voltaire supports the machines its device files describe; please report the line above.\n"+
			"Only if this machine is the same hardware under another name, run setup with --machine <id>.", err)
		if permsOnlyFlag {
			fmt.Println(msg) // the boot unit: nothing to do is not a failure
			return nil
		}
		return errors.New(msg)
	}
	if err != nil {
		return err
	}
	grants, err := perms.For(m.cfg)
	if err != nil {
		return err
	}
	gid, err := lookupGID(setupGroup)
	if err != nil {
		return err
	}
	header := perms.Header{Device: m.cfg.Device.ID, Group: setupGroup, Version: version.Version}
	opts := perms.Options{Group: setupGroup, GID: gid, DMIMatch: m.dmiMatch, DryRun: dryRunFlag, Out: os.Stdout}
	opts.OnlyOurs = onlyOursGrant(setupGroup)
	rules := grants.Rules(header)
	passthrough := wantPassthrough()
	unit := perms.Unit(setupExecutable(), header, setupMachineFlag, passthrough && len(lightingSources(m.cfg)) > 0)

	if dryRunFlag {
		fmt.Printf("=== DRY RUN for %s (no changes will be made) ===\n", m.cfg.Device.Name)
		if !permsOnlyFlag {
			fmt.Printf("\nWould write %s:\n\n%s\n", rulesPath, rules)
			fmt.Printf("Would write %s:\n\n%s\n", servicePath, unit)
			cleanupOldArtifacts(true)
		}
		syncPassthrough(m.cfg, passthrough, !permsOnlyFlag)
		fmt.Println("Would apply now (files present on this machine):")
		res := applyGrants(grants, knownHIDRaw(), opts)
		fmt.Printf("  %d to grant, %d already granted, %d to revoke, %d left alone (anything else is absent)\n",
			res.Granted, res.Already, res.Revoked, res.Skipped)
		return nil
	}
	if !isRoot() {
		return fmt.Errorf("this command requires root — run with sudo:\n  sudo voltaire setup\n\nTo preview changes without root: voltaire --dry-run setup")
	}

	if permsOnlyFlag {
		// Before InputPlumber starts (the unit's ordering), so it reads an
		// override made from its current config.
		syncPassthrough(m.cfg, passthrough, false)
		res := applyGrants(grants, knownHIDRaw(), opts)
		fmt.Printf("voltaire permissions for %s: %s\n", m.cfg.Device.ID, res)
		if data, err := os.ReadFile(rulesPath); err == nil {
			if dev, _ := perms.ParseHeader(string(data)); dev != "" && dev != m.cfg.Device.ID {
				fmt.Printf("note: %s was written for %s, not this machine (%s); run: sudo voltaire setup\n",
					rulesPath, dev, m.cfg.Device.ID)
			}
		}
		return nil
	}

	if err := os.WriteFile(rulesPath, []byte(rules), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", rulesPath, err)
	}
	fmt.Printf("Wrote %s for %s (group: %s)\n", rulesPath, m.cfg.Device.Name, setupGroup)
	// The pre-rename artifacts go after the new rules exist and before udev
	// reloads, so there is no window with neither in place.
	cleanupOldArtifacts(false)
	// Before the grants: a node InputPlumber stops hiding is granted below.
	syncPassthrough(m.cfg, passthrough, true)
	if note("udevadm", "control", "--reload-rules") {
		note("udevadm", "trigger", "--subsystem-match=hidraw")
	}

	// systemd 259+ does not execute RUN for a synthetic `udevadm trigger`, so
	// the grants are applied directly too; the rules cover real events from
	// the next boot on.
	res := applyGrants(grants, knownHIDRaw(), opts)
	fmt.Printf("Applied: %s.\n", res)

	if err := os.WriteFile(servicePath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", servicePath, err)
	}
	fmt.Printf("Wrote %s\n", servicePath)
	// reenable replaces an enable link that points at a unit a package used
	// to ship under /usr/lib; restart, because enabling an already-active
	// oneshot runs nothing.
	if note("systemctl", "daemon-reload") && note("systemctl", "reenable", serviceName) && note("systemctl", "restart", serviceName) {
		fmt.Printf("Enabled %s.\n", serviceName)
	} else {
		fmt.Fprintf(os.Stderr, "Enable it by hand: sudo systemctl enable --now %s\n", serviceName)
	}

	fmt.Printf("\nDone. Members of the '%s' group can now use voltaire without sudo.\n", setupGroup)
	fmt.Printf("If your user is not in '%s', add it with:\n  sudo usermod -aG %s $USER\n", setupGroup, setupGroup)
	fmt.Println("Then log out and back in for the group change to take effect.")
	return nil
}

// onlyOursGrant reports perms.OnlyOursGrant for group, noting when another
// rules file hands the group out too: then a hidraw grant udev left on a
// renumbered node of an unknown device stays (docs/decisions/0006).
func onlyOursGrant(group string) bool {
	only, other := perms.OnlyOursGrant(group, rulesPath)
	if !only {
		fmt.Printf("note: %s also gives group %s to devices, so only voltaire's own devices' stray hidraw grants are reset\n", other, group)
	}
	return only
}

// runSetupRemove undoes setup: the unit and the rules file (each only if it
// carries setup's marker), and every grant still carrying the group.
func runSetupRemove() error {
	group := setupGroup
	if data, err := os.ReadFile(rulesPath); err == nil {
		if _, g := perms.ParseHeader(string(data)); g != "" {
			group = g // the group that run granted, not the flag's default
		}
	}
	if !dryRunFlag && !isRoot() {
		return fmt.Errorf("this command requires root — run with sudo:\n  sudo voltaire setup --remove")
	}
	if dryRunFlag {
		fmt.Println("=== DRY RUN (no changes will be made) ===")
		fmt.Printf("Would run: systemctl disable --now %s\n", serviceName)
	} else {
		note("systemctl", "disable", "--now", serviceName)
	}
	for _, path := range []string{servicePath, rulesPath} {
		data, err := os.ReadFile(path)
		switch {
		case err != nil:
			continue
		case !strings.Contains(string(data), perms.Marker):
			fmt.Fprintf(os.Stderr, "note: %s was not written by voltaire setup; leaving it in place\n", path)
		case dryRunFlag:
			fmt.Printf("Would remove %s\n", path)
		case os.Remove(path) != nil:
			fmt.Fprintf(os.Stderr, "note: cannot remove %s\n", path)
		default:
			fmt.Printf("Removed %s\n", path)
		}
	}
	if !dryRunFlag {
		note("systemctl", "daemon-reload")
		note("udevadm", "control", "--reload-rules")
	}
	syncPassthrough(device.Config{}, false, true)

	gid, err := lookupGID(group)
	if err != nil {
		return err
	}
	configs, err := device.Configs()
	if err != nil {
		return err
	}
	var all []perms.Grants
	for _, c := range configs {
		if g, err := perms.For(c); err == nil {
			all = append(all, g)
		}
	}
	res := revertGrants(all, perms.Options{Group: group, GID: gid, DryRun: dryRunFlag, Out: os.Stdout, OnlyOurs: onlyOursGrant(group)})
	fmt.Printf("Took group %s back from %d files.\n", group, res.Revoked)
	if !dryRunFlag {
		// Input nodes go back to udev's defaults on a fresh event.
		note("udevadm", "trigger", "--action=add", "--subsystem-match=input")
	}
	return nil
}

// cleanupOldArtifacts removes what a pre-2.0 "z13ctl setup" installed.
//
// The rules file is deleted only when its header shows it is ours — a file at
// that path we did not generate is someone's local configuration, and deleting
// it would be worse than leaving a stale grant. The old perms unit gets an
// explicit "disable --now" before removal because RemainAfterExit=yes keeps it
// active (holding its enable symlink and stale grants) through anything short
// of that; failures are reported and skipped, since old-name cleanup must
// never stop the new setup from completing.
func cleanupOldArtifacts(dryRun bool) {
	if data, err := os.ReadFile(oldRulesPath); err == nil {
		if strings.Contains(string(data), "Generated by: z13ctl setup") {
			if dryRun {
				fmt.Printf("Would remove old rules file %s\n", oldRulesPath)
			} else if err := os.Remove(oldRulesPath); err != nil {
				fmt.Fprintf(os.Stderr, "note: cannot remove old rules file %s: %v\n", oldRulesPath, err)
			} else {
				fmt.Printf("Removed old rules file %s\n", oldRulesPath)
			}
		} else {
			fmt.Fprintf(os.Stderr, "note: %s was not generated by z13ctl setup; leaving it in place\n", oldRulesPath)
		}
	}

	_, enabledErr := runCommand("systemctl", "is-enabled", "--quiet", oldServiceName)
	_, oldUnitStat := os.Stat(oldServicePath)
	if enabledErr != nil && oldUnitStat != nil {
		return
	}
	if dryRun {
		fmt.Printf("Would run: systemctl disable --now %s\n", oldServiceName)
		if oldUnitStat == nil {
			fmt.Printf("Would remove old unit file %s\n", oldServicePath)
		}
		return
	}
	if note("systemctl", "disable", "--now", oldServiceName) {
		fmt.Printf("Disabled old %s\n", oldServiceName)
	}
	if oldUnitStat == nil {
		if err := os.Remove(oldServicePath); err != nil {
			fmt.Fprintf(os.Stderr, "note: cannot remove old unit file %s: %v\n", oldServicePath, err)
		} else {
			fmt.Printf("Removed old unit file %s\n", oldServicePath)
		}
	}
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Grant non-root access to this machine's devices",
	Long: `Grants a group (default: users) what this machine's device file needs for
voltaire to run without root: the lighting and hardware-button device nodes,
and the sysfs attributes for profiles, fans, power limits, firmware switches,
the charge limit, CPU boost and the package-power counter — each only if the
device file declares it. Other machines' hardware is never granted.

It writes ` + "/etc/udev/rules.d/99-voltaire.rules" + ` (applied by udev on real
device events) and ` + "/etc/systemd/system/" + serviceName + `, a boot-time
oneshot that runs 'voltaire setup --perms-only' for attributes created after
their udev event or outside udev entirely. Both say which device and group they
were written for. Distribution packages run setup when they are installed.

The machine is identified by its firmware (DMI). On a machine no device file
describes, setup grants nothing and says so.

  --machine <id>  use that device file instead of matching DMI (for the same
                  hardware under another name, or with --dry-run to preview
                  another machine's grants)
  --perms-only    apply the grants now, without writing either file (what the
                  boot unit runs)
  --remove        undo setup: disable and remove the unit and rules file, and
                  take the group back from everything it was granted
  --inputplumber-passthrough
                  where InputPlumber hides the interface this machine's
                  lighting uses (the ROG Xbox Ally X's joystick rings), write
                  an override of InputPlumber's own config for this machine
                  that leaves it visible, and restart InputPlumber. The
                  override is remade from InputPlumber's installed config on
                  every setup run and at boot, so it follows InputPlumber's
                  updates. =false removes it, as does --remove.
  --dry-run       show what would be written and changed, without root

A later run keeps the group, --machine and the passthrough choice of the
previous one unless they are given again, so a package upgrade's plain
"voltaire setup" changes none of them.

This command must be run with sudo (unless --dry-run is used).`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		setupGroupSet = cmd.Flags().Changed("group")
		setupMachineSet = cmd.Flags().Changed("machine")
		ipPassthroughSet = cmd.Flags().Changed("inputplumber-passthrough")
		return runSetup()
	},
}

func init() {
	setupCmd.Flags().StringVar(&setupGroup, "group", "users", "Group to grant device access to")
	setupCmd.Flags().BoolVar(&permsOnlyFlag, "perms-only", false, "Apply the grants now without writing the rules file or boot unit")
	setupCmd.Flags().StringVar(&setupMachineFlag, "machine", "", "Device file id to grant for, instead of matching this machine's DMI")
	setupCmd.Flags().BoolVar(&setupRemoveFlag, "remove", false, "Remove the rules file and boot unit and take back every grant")
	setupCmd.Flags().BoolVar(&ipPassthroughFlag, "inputplumber-passthrough", false,
		"Have InputPlumber leave this machine's lighting interface visible (=false undoes it)")
	rootCmd.AddCommand(setupCmd)
}
