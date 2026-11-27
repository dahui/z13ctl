package cmd

// setup_test.go — setup's orchestration (files written, commands run, order)
// and the packaging that runs it. What is granted is internal/perms', and its
// tests cover the grants themselves. Every seam that reaches the machine
// (commands, root, DMI, the permission passes, the /etc paths) is replaced
// here: no test in this file can change a real permission.

import (
	"bytes"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/drivers/inputplumber"
	"github.com/dahui/voltaire/v2/internal/perms"
)

type setupRig struct {
	dir      string
	upstream string // InputPlumber's device configs, empty until a test adds one
	commands []string
	applied  []perms.Grants
	reverted int
}

// newSetupRig points every seam at a recorder and the paths at a temporary
// directory, and restores them afterwards. Flags are package globals, so the
// tests here do not run in parallel.
func newSetupRig(t *testing.T) *setupRig {
	t.Helper()
	r := &setupRig{dir: t.TempDir()}
	g, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		t.Skipf("cannot name this process's group: %v", err)
	}

	saved := struct {
		rules, service, oldRules, oldService, group, machine string
		permsOnly, remove, dry                               bool
		run                                                  func(string, ...string) ([]byte, error)
		root                                                 func() bool
		match                                                func() (device.Config, error)
		apply                                                func(perms.Grants, []perms.HIDRaw, perms.Options) perms.Result
		revert                                               func([]perms.Grants, perms.Options) perms.Result
		groupSet, machineSet, ipFlag, ipSet                  bool
		ipOverride                                           string
		ipUpstream                                           func() string
		ipDMI                                                func() inputplumber.DMI
	}{rulesPath, servicePath, oldRulesPath, oldServicePath, setupGroup, setupMachineFlag,
		permsOnlyFlag, setupRemoveFlag, dryRunFlag, runCommand, isRoot, matchConfig, applyGrants, revertGrants,
		setupGroupSet, setupMachineSet, ipPassthroughFlag, ipPassthroughSet,
		inputplumber.OverrideDir, ipUpstreamDir, ipReadDMI}
	t.Cleanup(func() {
		rulesPath, servicePath, oldRulesPath, oldServicePath = saved.rules, saved.service, saved.oldRules, saved.oldService
		setupGroup, setupMachineFlag = saved.group, saved.machine
		permsOnlyFlag, setupRemoveFlag, dryRunFlag = saved.permsOnly, saved.remove, saved.dry
		runCommand, isRoot, matchConfig, applyGrants, revertGrants = saved.run, saved.root, saved.match, saved.apply, saved.revert
		setupGroupSet, setupMachineSet, ipPassthroughFlag, ipPassthroughSet = saved.groupSet, saved.machineSet, saved.ipFlag, saved.ipSet
		inputplumber.OverrideDir, ipUpstreamDir, ipReadDMI = saved.ipOverride, saved.ipUpstream, saved.ipDMI
	})
	setupGroupSet, setupMachineSet, ipPassthroughFlag, ipPassthroughSet = false, false, false, false
	inputplumber.OverrideDir = filepath.Join(r.dir, "etc-inputplumber", "devices.d")
	ipUpstreamDir = func() string { return r.upstream }
	ipReadDMI = func() inputplumber.DMI {
		return inputplumber.DMI{"board_name": "RC73XA", "sys_vendor": "ASUSTeK COMPUTER INC."}
	}

	rulesPath = filepath.Join(r.dir, "99-voltaire.rules")
	servicePath = filepath.Join(r.dir, serviceName)
	oldRulesPath = filepath.Join(r.dir, "99-z13ctl.rules")
	oldServicePath = filepath.Join(r.dir, "z13ctl-perms.service")
	setupGroup, setupMachineFlag = g.Name, ""
	permsOnlyFlag, setupRemoveFlag, dryRunFlag = false, false, false
	runCommand = func(name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		r.commands = append(r.commands, cmd)
		if strings.Contains(cmd, "is-enabled") {
			return nil, errors.New("not enabled") // no pre-2.0 unit here
		}
		return nil, nil
	}
	isRoot = func() bool { return true }
	matchConfig = func() (device.Config, error) { return device.ConfigByID("asus-rog-flow-z13-2025") }
	applyGrants = func(g perms.Grants, _ []perms.HIDRaw, _ perms.Options) perms.Result {
		r.applied = append(r.applied, g)
		return perms.Result{}
	}
	revertGrants = func(gs []perms.Grants, _ perms.Options) perms.Result {
		r.reverted = len(gs)
		return perms.Result{}
	}
	return r
}

func (r *setupRig) read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// TestSetupWritesThisMachinesGrants: the rules and unit name the machine they
// were written for and carry only its hardware, and the commands run in the
// order that makes an upgrade take effect without a reboot.
func TestSetupWritesThisMachinesGrants(t *testing.T) {
	r := newSetupRig(t)
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	rules := r.read(t, rulesPath)
	for _, want := range []string{perms.Marker, "# Device: asus-rog-flow-z13-2025, group: " + setupGroup, `idProduct}=="1a30"`} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules lack %q", want)
		}
	}
	if strings.Contains(rules, "1a86") {
		t.Error("the Z13's rules grant the OneXPlayer's controller")
	}
	unit := r.read(t, servicePath)
	if !strings.Contains(unit, "setup --perms-only --group "+setupGroup+"\n") || strings.Contains(unit, "--machine") {
		t.Errorf("unit's ExecStart is wrong for a DMI-matched machine:\n%s", unit)
	}
	if len(r.applied) != 1 || r.applied[0].Device != "asus-rog-flow-z13-2025" {
		t.Errorf("applied %+v, want the Z13's grants once", r.applied)
	}
	want := []string{
		"udevadm control --reload-rules",
		"udevadm trigger --subsystem-match=hidraw",
		"systemctl daemon-reload",
		"systemctl reenable " + serviceName,
		"systemctl restart " + serviceName,
	}
	var got []string
	for _, c := range r.commands {
		if !strings.Contains(c, "is-enabled") {
			got = append(got, c)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestSetupMachineFlag: --machine picks another device file, and the boot unit
// is told the same, since DMI would not find it at boot either.
func TestSetupMachineFlag(t *testing.T) {
	r := newSetupRig(t)
	setupMachineFlag = "asus-rog-xbox-ally-x"
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.read(t, rulesPath), "mcu_powersave") {
		t.Error("rules are not the Ally's")
	}
	if !strings.Contains(r.read(t, servicePath), "--machine asus-rog-xbox-ally-x") {
		t.Error("the boot unit was not told the machine")
	}
	setupMachineFlag = "no-such-device"
	if err := runSetup(); err == nil {
		t.Error("an unknown --machine id was accepted")
	}
}

// TestUnknownMachineGrantsNothing: setup never borrows another machine's
// file; the boot unit treats it as nothing to do.
func TestUnknownMachineGrantsNothing(t *testing.T) {
	r := newSetupRig(t)
	matchConfig = func() (device.Config, error) {
		return device.Config{}, errors.Join(device.ErrUnknownDevice, errors.New(`vendor "Acme", product "Laptop"`))
	}
	if err := runSetup(); err == nil || !strings.Contains(err.Error(), "nothing granted") {
		t.Fatalf("full setup on an unknown machine: %v", err)
	}
	if _, err := os.Stat(rulesPath); err == nil {
		t.Error("rules written for an unknown machine")
	}
	permsOnlyFlag = true
	if err := runSetup(); err != nil {
		t.Errorf("the boot pass on an unknown machine must exit 0: %v", err)
	}
	if len(r.applied) != 0 || len(r.commands) != 0 {
		t.Errorf("unknown machine: applied %d, ran %v", len(r.applied), r.commands)
	}
}

func TestSetupRefusesWithoutRoot(t *testing.T) {
	r := newSetupRig(t)
	isRoot = func() bool { return false }
	if err := runSetup(); err == nil {
		t.Fatal("setup ran without root")
	}
	if len(r.applied) != 0 {
		t.Error("grants applied without root")
	}
}

// TestSetupRemove deletes only files carrying setup's marker, and takes the
// group back from what any device file grants.
func TestSetupRemove(t *testing.T) {
	r := newSetupRig(t)
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(r.dir, "foreign.rules")
	if err := os.WriteFile(foreign, []byte("# a local rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rulesPath = foreign
	setupRemoveFlag = true
	r.commands = nil
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(servicePath); err == nil {
		t.Error("remove left the unit it wrote")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("remove deleted a file setup did not write")
	}
	configs, _ := device.Configs()
	if r.reverted != len(configs) {
		t.Errorf("reverted %d device files' grants, want all %d", r.reverted, len(configs))
	}
	if len(r.commands) == 0 || r.commands[0] != "systemctl disable --now "+serviceName {
		t.Errorf("remove must disable the unit first: %v", r.commands)
	}
}

// TestSetupCleanupVerifiesOldRulesHeader: cleanupOldArtifacts deletes the
// pre-rename rules file only when it carries the marker pre-2.0 setup wrote,
// and the new marker is different, so setup can never delete its own file.
func TestSetupCleanupVerifiesOldRulesHeader(t *testing.T) {
	r := newSetupRig(t)
	if err := os.WriteFile(oldRulesPath, []byte("# Generated by: z13ctl setup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupOldArtifacts(false)
	if _, err := os.Stat(oldRulesPath); err == nil {
		t.Error("the pre-rename rules file survived cleanup")
	}
	if strings.Contains(perms.Marker, "z13ctl") {
		t.Error("the new marker matches the old one")
	}
	if len(r.commands) != 1 { // only the is-enabled probe: no old unit here
		t.Errorf("cleanup ran %v", r.commands)
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// TestNoPermissionArtifactIsPackaged: setup writes the rules and the boot unit
// for the machine it runs on. A packaged copy would grant every device's paths
// on every machine, and a packaged copy beside setup's is the half-state
// issue #12 was.
func TestNoPermissionArtifactIsPackaged(t *testing.T) {
	for _, gone := range []string{"../contrib/udev/99-voltaire.rules", "../contrib/systemd/system/voltaire-perms.service"} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s exists; setup generates it per machine", gone)
		}
	}
	for _, f := range []string{"../.goreleaser.yml", "../contrib/aur/PKGBUILD"} {
		content := readRepoFile(t, f)
		for _, name := range []string{"contrib/udev/99-voltaire.rules", "voltaire-perms.service"} {
			if strings.Contains(content, name) {
				t.Errorf("%s still ships %s", f, name)
			}
		}
	}
}

// TestPackagesRunSetup: every package runs setup at install and upgrade by its
// full path, and undoes it only on a real removal.
func TestPackagesRunSetup(t *testing.T) {
	post := readRepoFile(t, "../contrib/nfpm/postinstall.sh")
	if !strings.Contains(post, "/usr/bin/voltaire setup") {
		t.Error("postinstall.sh does not run /usr/bin/voltaire setup")
	}
	pre := readRepoFile(t, "../contrib/nfpm/preremove.sh")
	guard := strings.Index(pre, "1 | 2 | upgrade | failed-upgrade | deconfigure) exit 0")
	remove := strings.Index(pre, "/usr/bin/voltaire setup --remove")
	if guard < 0 || remove < 0 || guard > remove {
		t.Error("preremove.sh must return on an upgrade before it runs setup --remove: " +
			"rpm runs the old package's preun after the new post")
	}
	gr := readRepoFile(t, "../.goreleaser.yml")
	for _, want := range []string{"posttrans: contrib/nfpm/postinstall.sh", "postupgrade: contrib/nfpm/postinstall.sh"} {
		if !strings.Contains(gr, want) {
			t.Errorf(".goreleaser.yml lacks %q", want)
		}
	}
	aur := readRepoFile(t, "../contrib/aur/voltaire-bin.install")
	for _, want := range []string{"/usr/bin/voltaire setup ||", "pre_remove()", "/usr/bin/voltaire setup --remove"} {
		if !strings.Contains(aur, want) {
			t.Errorf("voltaire-bin.install lacks %q", want)
		}
	}
}

// TestPostinstallCleansUpPreRenameUnits pins the 2.0 migration half of the
// packaging scripts. The old perms unit is Type=oneshot with
// RemainAfterExit=yes, so a package upgrade that merely stops shipping it
// leaves it active and enabled, holding stale grants; only an explicit
// "disable --now" clears it. The old user units likewise stay enabled until
// disabled, racing the new ones for the shared legacy socket path.
func TestPostinstallCleansUpPreRenameUnits(t *testing.T) {
	postinstall := readRepoFile(t, "../contrib/nfpm/postinstall.sh")
	for _, want := range []string{
		"systemctl disable --now z13ctl-perms.service",
		"systemctl --global disable z13ctl.socket z13ctl.service",
		"systemctl --global enable voltaire.socket voltaire.service",
	} {
		if !strings.Contains(postinstall, want) {
			t.Errorf("contrib/nfpm/postinstall.sh is missing %q", want)
		}
	}
}

// withAllyInputPlumber gives the rig InputPlumber 0.85.0's Ally config and
// makes the machine the Ally.
func (r *setupRig) withAllyInputPlumber(t *testing.T) {
	t.Helper()
	r.upstream = filepath.Join(r.dir, "usr-share-inputplumber", "devices")
	if err := os.MkdirAll(r.upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../internal/drivers/inputplumber/testdata/50-rog_xbox_ally.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.upstream, "50-rog_xbox_ally.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	matchConfig = func() (device.Config, error) { return device.ConfigByID("asus-rog-xbox-ally-x") }
}

func (r *setupRig) ran(cmd string) int {
	for i, c := range r.commands {
		if c == cmd {
			return i
		}
	}
	return -1
}

const ipRestart = "systemctl try-restart inputplumber.service"

// TestSetupPassthroughOptIn: nothing without the flag; with it, the override,
// an InputPlumber restart before the grants and a boot unit ordered before
// InputPlumber; a plain rerun and the boot pass keep it without restarting;
// =false removes it.
func TestSetupPassthroughOptIn(t *testing.T) {
	r := newSetupRig(t)
	r.withAllyInputPlumber(t)
	override := filepath.Join(inputplumber.OverrideDir, "50-rog_xbox_ally.yaml")

	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(override); err == nil || r.ran(ipRestart) >= 0 {
		t.Fatal("an override was written, or InputPlumber restarted, without the opt-in")
	}

	ipPassthroughFlag, ipPassthroughSet = true, true
	r.commands = nil
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(override)
	if err != nil || !inputplumber.IsOverride(data) {
		t.Fatalf("no override written: %v", err)
	}
	restart, reload := r.ran(ipRestart), r.ran("udevadm control --reload-rules")
	if restart < 0 || restart > reload {
		t.Errorf("InputPlumber must restart before the grants: %v", r.commands)
	}
	if !strings.Contains(r.read(t, servicePath), "Before=inputplumber.service") {
		t.Error("the boot unit does not run before InputPlumber")
	}

	// A package upgrade's plain run keeps it and restarts nothing.
	ipPassthroughFlag, ipPassthroughSet = false, false
	r.commands = nil
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(override); err != nil || r.ran(ipRestart) >= 0 {
		t.Errorf("a plain rerun dropped the override or restarted InputPlumber: %v", r.commands)
	}
	if !strings.Contains(r.read(t, servicePath), "Before=inputplumber.service") {
		t.Error("a plain rerun lost the boot ordering")
	}

	// The boot pass remakes it and never restarts InputPlumber.
	if err := os.WriteFile(override, append([]byte(inputplumber.OverrideMarker+"\n"), "stale\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	permsOnlyFlag = true
	r.commands = nil
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(override); !bytes.Equal(got, data) || r.ran(ipRestart) >= 0 {
		t.Errorf("boot pass: remade %v, commands %v", bytes.Equal(got, data), r.commands)
	}
	permsOnlyFlag = false

	ipPassthroughFlag, ipPassthroughSet = false, true
	r.commands = nil
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(override); err == nil || r.ran(ipRestart) < 0 {
		t.Errorf("=false left the override or did not restart InputPlumber: %v", r.commands)
	}
	if strings.Contains(r.read(t, servicePath), "inputplumber") {
		t.Error("the boot unit still orders against InputPlumber after =false")
	}
}

func TestSetupRemoveTakesTheOverride(t *testing.T) {
	r := newSetupRig(t)
	r.withAllyInputPlumber(t)
	ipPassthroughFlag, ipPassthroughSet = true, true
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	ipPassthroughFlag, ipPassthroughSet = false, false
	setupRemoveFlag = true
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	if len(inputplumber.Overrides()) != 0 || r.ran(ipRestart) < 0 {
		t.Errorf("remove left the override or did not restart InputPlumber: %v", r.commands)
	}
}

// TestSetupKeepsEarlierChoices: a plain run keeps the group and --machine
// the boot unit was written with.
func TestSetupKeepsEarlierChoices(t *testing.T) {
	r := newSetupRig(t)
	earlier := setupGroup
	setupMachineFlag = "asus-rog-xbox-ally-x"
	if err := runSetup(); err != nil {
		t.Fatal(err)
	}
	// The next run as a package upgrade makes it: no flags, defaults.
	setupMachineFlag, setupGroup = "", "no-such-group-voltaire-test"
	matchConfig = func() (device.Config, error) { return device.Config{}, device.ErrUnknownDevice }
	if err := runSetup(); err != nil {
		t.Fatalf("a plain rerun did not keep the earlier choices: %v", err)
	}
	unit := r.read(t, servicePath)
	if !strings.Contains(unit, "--group "+earlier+" --machine asus-rog-xbox-ally-x\n") {
		t.Errorf("unit after a plain rerun:\n%s", unit)
	}
	// Given again, a flag wins.
	setupMachineFlag, setupMachineSet = "no-such-device", true
	if err := runSetup(); err == nil {
		t.Error("an explicit --machine was overridden by the earlier one")
	}
}
