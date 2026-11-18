package cmd

// setup_test.go — drift guards between the permission artifacts that
// "voltaire setup" generates and the ones shipped by package installs under
// contrib/. Issue #14 was caused by exactly this drift: the ppt_* grants
// existed only in the generated artifacts, so .rpm/.deb users lost TDP access
// on every reboot.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/drivers/evdevkey"
)

const (
	packagedRulesPath   = "../contrib/udev/99-voltaire.rules"
	packagedServicePath = "../contrib/systemd/system/voltaire-perms.service"
)

// sysfsGrants are the targets voltaire must be able to write as a non-root user.
// Each must appear in the generated artifact and in the packaged one shipped by
// nfpm, otherwise package installs silently lose the capability after a reboot.
var sysfsGrants = []struct {
	name  string
	match string
	// rules and service record which artifact is responsible for the grant.
	rules   bool
	service bool
}{
	{name: "lightbar hidraw", match: `ATTRS{idProduct}=="18c6"`, rules: true},
	{name: "keyboard hidraw", match: `ATTRS{idProduct}=="1a30"`, rules: true},
	{name: "platform profile", match: `SUBSYSTEM=="platform-profile"`, rules: true},
	{name: "armoury crate button", match: `ATTRS{name}=="Asus WMI hotkeys"`, rules: true},
	{name: "custom fan curve hwmon", match: `ATTR{name}=="asus_custom_fan_curve"`, rules: true},
	{name: "fan mode hwmon", match: `ATTR{name}=="asus"`, rules: true},
	{name: "battery charge threshold", match: "charge_control_end_threshold", rules: true, service: true},
	{name: "boot sound", match: "attributes/boot_sound/current_value", rules: true, service: true},
	{name: "panel overdrive", match: "attributes/panel_overdrive/current_value", rules: true, service: true},
	{name: "armoury PPT power limits", match: "/sys/class/firmware-attributes/asus-armoury/attributes/ppt_*/current_value", rules: true, service: true},
	{name: "PPT power limits (asus-nb-wmi fallback)", match: "/sys/devices/platform/asus-nb-wmi/ppt_*", rules: true, service: true},
	{name: "ryzen_smu", match: "/sys/kernel/ryzen_smu_drv/", service: true},
	// Service only, and unusually so: cpufreq's boost switch is a plain kobject
	// rather than a device, so udevadm cannot even name it and no rule can
	// grant it. Everything else in this table that the unit covers has a rule
	// beside it as a best-effort first pass; this one has the unit or nothing.
	{name: "cpufreq boost", match: "/sys/devices/system/cpu/cpufreq/boost", service: true},
	// The one read-only grant. energy_uj is 0400 root:root under the Platypus
	// mitigation, so package power is unreadable without it; the rule matches
	// the package-0 domain and the unit globs, because the counter can exist
	// before any udev event this rule could hang off.
	{name: "powercap energy counter", match: "energy_uj", rules: true, service: true},
}

// TestEveryDeviceFileTargetIsGranted: the HID ids and button devices come from
// the device files, so a device file that adds a zone or a button must reach
// both the generated rules and the packaged copy. sysfsGrants above pins the
// Z13's by name; this covers whatever the device files say next.
func TestEveryDeviceFileTargetIsGranted(t *testing.T) {
	t.Parallel()

	targets, err := deviceSetupTargets()
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.hid) == 0 || len(targets.buttons) == 0 {
		t.Fatalf("no targets from the device files: %+v", targets)
	}
	var want []string
	for _, g := range targets.hid {
		v, p, _ := strings.Cut(g.usb, ":")
		want = append(want, `ATTRS{idVendor}=="`+v+`", ATTRS{idProduct}=="`+p+`", ENV{ID_USB_INTERFACE_NUM}=="`+fmt.Sprintf("%02x", g.iface)+`"`)
	}
	for _, g := range targets.buttons {
		want = append(want, `ATTRS{name}=="`+g.name+`"`)
	}
	for name, content := range map[string]string{
		"buildRulesContent": buildRulesContent("users"),
		packagedRulesPath:   readPackaged(t, packagedRulesPath),
	} {
		for _, w := range want {
			if !strings.Contains(content, w) {
				t.Errorf("%s does not grant %s", name, w)
			}
		}
	}
}

func readPackaged(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// TestPowercapIsGrantedReadOnly pins the one asymmetry in the grant table.
//
// Every other target here is chmod g+w because voltaire writes it. The powercap
// directory holds the package power *caps* alongside the energy counter, so a
// g+w grant there would hand every member of the group control of the CPU's
// power limits — through a rule whose only purpose is to draw a graph. The
// counter is read, so the grant is read.
func TestPowercapIsGrantedReadOnly(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"buildRulesContent":   buildRulesContent("users"),
		"buildServiceContent": buildServiceContent("users"),
		packagedRulesPath:     readPackaged(t, packagedRulesPath),
		packagedServicePath:   readPackaged(t, packagedServicePath),
	} {
		for _, line := range strings.Split(content, "\n") {
			// Comments name the file to explain the grant; only the grant
			// itself is under test.
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
				continue
			}
			if !strings.Contains(line, "energy_uj") {
				continue
			}
			if strings.Contains(line, "g+w") {
				t.Errorf("%s grants write on the powercap energy counter:\n  %s\n"+
					"That directory also holds the power caps; the counter only needs g+r.",
					name, strings.TrimSpace(line))
			}
			if !strings.Contains(line, "g+r") {
				t.Errorf("%s touches energy_uj without granting read:\n  %s", name, strings.TrimSpace(line))
			}
		}
	}
}

func TestGeneratedAndPackagedArtifactsGrantSameTargets(t *testing.T) {
	t.Parallel()

	genRules := buildRulesContent("users")
	genService := buildServiceContent("users")
	pkgRules := readPackaged(t, packagedRulesPath)
	pkgService := readPackaged(t, packagedServicePath)

	for _, g := range sysfsGrants {
		if g.rules {
			if !strings.Contains(genRules, g.match) {
				t.Errorf("buildRulesContent() is missing the %s grant (%q)", g.name, g.match)
			}
			if !strings.Contains(pkgRules, g.match) {
				t.Errorf("%s is missing the %s grant (%q) — package installs will lose it on reboot",
					packagedRulesPath, g.name, g.match)
			}
		}
		if g.service {
			if !strings.Contains(genService, g.match) {
				t.Errorf("buildServiceContent() is missing the %s grant (%q)", g.name, g.match)
			}
			if !strings.Contains(pkgService, g.match) {
				t.Errorf("%s is missing the %s grant (%q) — package installs will lose it on reboot",
					packagedServicePath, g.name, g.match)
			}
		}
	}
}

// TestGeneratedArtifactsHaveNoFormatErrors catches a mismatch between the number
// of %s verbs and the arguments passed to fmt.Sprintf, which would otherwise
// write a literal "%!s(MISSING)" into a live udev rule or unit file.
func TestGeneratedArtifactsHaveNoFormatErrors(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"buildRulesContent":   buildRulesContent("users"),
		"buildServiceContent": buildServiceContent("users"),
	} {
		if strings.Contains(content, "%!") {
			t.Errorf("%s() output contains a fmt error verb:\n%s", name, content)
		}
		if strings.Contains(content, "%s") {
			t.Errorf("%s() output contains an unsubstituted %%s verb:\n%s", name, content)
		}
	}
}

// TestShellLoopsEscapeDollar guards the escaping rule that is easy to get wrong
// when hand-editing these files: "$$" is systemd's and udev's escape for a
// literal dollar sign. A bare "$f" expands to the empty string, so the loop
// silently chmods nothing and the permission grant is a no-op.
func TestShellLoopsEscapeDollar(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"buildRulesContent":   buildRulesContent("users"),
		"buildServiceContent": buildServiceContent("users"),
		packagedRulesPath:     readPackaged(t, packagedRulesPath),
		packagedServicePath:   readPackaged(t, packagedServicePath),
	}
	for name, content := range sources {
		for _, line := range strings.Split(content, "\n") {
			if !strings.Contains(line, "for f in ") {
				continue
			}
			// Strip the correct "$$" form; anything left is a bare "$".
			if strings.Contains(strings.ReplaceAll(line, "$$", ""), "$") {
				t.Errorf("%s has a shell loop with an unescaped $ (use $$):\n  %s", name, line)
			}
		}
	}
}

// TestPackagedServiceIsOneshotWithRemainAfterExit documents why
// contrib/nfpm/postinstall.sh must restart the unit rather than rely on
// "systemctl enable --now", which is a no-op when the unit is already active.
func TestPackagedServiceIsOneshotWithRemainAfterExit(t *testing.T) {
	t.Parallel()

	service := readPackaged(t, packagedServicePath)
	for _, want := range []string{"Type=oneshot", "RemainAfterExit=yes"} {
		if !strings.Contains(service, want) {
			t.Errorf("%s is missing %q", packagedServicePath, want)
		}
	}

	postinstall := readPackaged(t, "../contrib/nfpm/postinstall.sh")
	if !strings.Contains(postinstall, "systemctl restart voltaire-perms.service") {
		t.Error("contrib/nfpm/postinstall.sh must restart voltaire-perms.service; " +
			"'enable --now' does not re-run ExecStart for an already-active oneshot, " +
			"so upgrades would not apply new permission grants until a reboot")
	}
}

// TestPostinstallCleansUpPreRenameUnits pins the 2.0 migration half of the
// packaging scripts. The old perms unit is Type=oneshot with
// RemainAfterExit=yes, so a package upgrade that merely stops shipping it
// leaves it active and enabled, holding stale grants; only an explicit
// "disable --now" clears it. The old user units likewise stay enabled until
// disabled, racing the new ones for the shared legacy socket path.
func TestPostinstallCleansUpPreRenameUnits(t *testing.T) {
	t.Parallel()

	postinstall := readPackaged(t, "../contrib/nfpm/postinstall.sh")
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

// TestSetupCleanupVerifiesOldRulesHeader: cleanupOldArtifacts may delete the
// pre-rename rules file only when it carries our generated-by marker — a file
// at that path we did not write is someone's local configuration. This pins
// the marker string the check looks for to the one pre-2.0 setup actually
// wrote, so neither side can drift without failing here.
func TestSetupCleanupVerifiesOldRulesHeader(t *testing.T) {
	t.Parallel()

	// The exact header every pre-2.0 "z13ctl setup" wrote.
	const oldGeneratedBy = "# Generated by: z13ctl setup"
	if !strings.Contains(oldGeneratedBy, "Generated by: z13ctl setup") {
		t.Fatal("test constant drifted from the marker cleanupOldArtifacts greps for")
	}
	// And the new generated content must NOT match the old marker, or setup
	// run twice would delete its own freshly-written file if the paths ever
	// collapsed back together.
	if strings.Contains(buildRulesContent("users"), "Generated by: z13ctl setup") {
		t.Error("new rules content carries the old generated-by marker")
	}
	if !strings.Contains(buildRulesContent("users"), "Generated by: voltaire setup") {
		t.Error("new rules content is missing its generated-by marker")
	}
}

// inputGrantLines is every input-node grant line in a rules file.
func inputGrantLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || !strings.Contains(line, `SUBSYSTEM=="input"`) {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// checkButtonGrant is the rule every button grant must keep: read-only, and
// either refusing typing keyboards or scoped to one machine's DMI identity.
// It returns what is wrong with line, "" when nothing is.
func checkButtonGrant(line string) string {
	if !strings.Contains(line, `MODE="0640"`) {
		return "is not read-only (a writable input node accepts injected events)"
	}
	guarded := strings.Contains(line, `ENV{ID_INPUT_KEYBOARD}!="1"`)
	scoped := strings.Contains(line, `ATTR{[dmi/id]sys_vendor}==`) && strings.Contains(line, `ATTR{[dmi/id]product_name}==`)
	if !guarded && !scoped {
		return "could match a typing keyboard on any machine"
	}
	return ""
}

// TestButtonGrantsNeverReachATypingKeyboard: read access to a typing keyboard
// is read access to every keystroke. Every button grant, generated and
// packaged, refuses one or is pinned to the one machine whose device file
// opted its keyboard node in.
func TestButtonGrantsNeverReachATypingKeyboard(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"buildRulesContent": buildRulesContent("users"),
		packagedRulesPath:   readPackaged(t, packagedRulesPath),
	} {
		lines := inputGrantLines(content)
		if len(lines) == 0 {
			t.Fatalf("%s: no input grants at all", name)
		}
		for _, line := range lines {
			if why := checkButtonGrant(line); why != "" {
				t.Errorf("%s: %s\n  %s", name, why, line)
			}
		}
	}
}

// keyboardNodeConfigs is a device file that does not ship: one ordinary
// button and one whose chords arrive on the AT keyboard.
func keyboardNodeConfigs() []device.Config {
	return []device.Config{{
		Device: device.Meta{ID: "oxp-test", Match: []device.Match{
			{Vendor: "ONE-NETBOOK", ProductPrefix: "ONEXPLAYER X2"},
			{Vendor: "ONE-NETBOOK", Product: "ONEXPLAYER X2Mini PRO"},
		}},
		Buttons: []device.ButtonConfig{
			{ID: "home", Label: "Home button", Method: "evdev-key", Device: "OXP hotkeys", Keycode: 102},
			{ID: "oxp", Label: "OneXPlayer button", Method: "evdev-key", Device: "AT Translated Set 2 keyboard",
				Keycode: 56, KeyboardNode: "carries only the button chords on this model"},
		},
	}}
}

func TestKeyboardNodeGrantIsScopedToItsDevice(t *testing.T) {
	t.Parallel()

	rules := buttonRules(setupTargetsFrom(keyboardNodeConfigs()).buttons, "users")
	lines := inputGrantLines(rules)
	if len(lines) != 3 { // the ordinary button, and one per DMI pattern for the keyboard node
		t.Fatalf("got %d grant lines, want 3:\n%s", len(lines), rules)
	}
	for _, line := range lines {
		if why := checkButtonGrant(line); why != "" {
			t.Errorf("%s\n  %s", why, line)
		}
		if strings.Contains(line, "AT Translated") {
			if strings.Contains(line, "ENV{ID_INPUT_KEYBOARD}") {
				t.Errorf("the opted-in keyboard node is refused by its own grant:\n  %s", line)
			}
			if !strings.Contains(line, `ATTR{[dmi/id]sys_vendor}=="ONE-NETBOOK"`) {
				t.Errorf("keyboard node grant not scoped to the vendor:\n  %s", line)
			}
		}
	}
	for _, want := range []string{
		`ATTR{[dmi/id]product_name}=="ONEXPLAYER X2*"`,            // a prefix becomes a glob
		`ATTR{[dmi/id]product_name}=="ONEXPLAYER X2Mini PRO"`,     // an exact product stays exact
		`ATTRS{name}=="OXP hotkeys", ENV{ID_INPUT_KEYBOARD}!="1"`, // the ordinary button keeps the guard
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules missing %s:\n%s", want, rules)
		}
	}
}

// The direct grant follows the same two rules, through the same keyboard test
// the watcher uses.
func TestDirectButtonGrantFollowsTheGuard(t *testing.T) {
	orig := isTypingKeyboard
	t.Cleanup(func() { isTypingKeyboard = orig })
	kbd := map[string]bool{"event2": true, "event7": false}
	isTypingKeyboard = func(event string) (bool, error) { return kbd[event], nil }

	grants := setupTargetsFrom(keyboardNodeConfigs()).buttons
	for _, c := range []struct {
		desc, name, event, matched string
		want                       bool
	}{
		{"ordinary node", "OXP hotkeys", "event7", "", true},
		{"ordinary name on a keyboard node", "OXP hotkeys", "event2", "", false},
		{"opted-in keyboard, this machine", "AT Translated Set 2 keyboard", "event2", "oxp-test", true},
		{"opted-in keyboard, another machine", "AT Translated Set 2 keyboard", "event2", "asus-rog-flow-z13-2025", false},
		{"a name no device file gives", "Some keyboard", "event7", "oxp-test", false},
	} {
		if got := buttonNodeGranted(grants, c.name, c.event, c.matched); got != c.want {
			t.Errorf("%s: granted = %v, want %v", c.desc, got, c.want)
		}
	}
}

// An evdev button naming a USB interface is granted that interface's node
// only, in the rule and in the direct grant, as the watcher reads it. The OXP
// X2 Mini Pro's controller has two nodes named "HID 1a86:fe00"; only
// interface 0 carries the chords, and it is a typing keyboard by udev's test,
// so its grant is DMI-scoped too. The terms must be ENV: the event node
// carries usb_id's properties, while ATTRS{idVendor} sits on a different
// parent from ATTRS{name} and would make the rule match nothing.
func TestEvdevGrantNamesItsUSBInterface(t *testing.T) {
	zero := 0
	configs := []device.Config{{
		Device: device.Meta{ID: "oxp-test", Match: []device.Match{{Vendor: "ONE-NETBOOK", Product: "ONEXPLAYER X2Mini PRO"}}},
		Buttons: []device.ButtonConfig{{ID: "oxp", Label: "OneXPlayer button", Method: "evdev-chord",
			Device: "HID 1a86:fe00", USB: "1a86:fe00", Interface: &zero, Keys: []int{29, 56, 125},
			KeyboardNode: "the controller's interface-0 keyboard carries only the button chords"}},
	}}
	grants := setupTargetsFrom(configs).buttons
	rules := buttonRules(grants, "users")
	lines := inputGrantLines(rules)
	if len(lines) != 1 {
		t.Fatalf("got %d grant lines, want 1:\n%s", len(lines), rules)
	}
	for _, want := range []string{
		`ENV{ID_VENDOR_ID}=="1a86", ENV{ID_MODEL_ID}=="fe00", ENV{ID_USB_INTERFACE_NUM}=="00"`,
		`ATTR{[dmi/id]product_name}=="ONEXPLAYER X2Mini PRO"`,
	} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("grant missing %s:\n  %s", want, lines[0])
		}
	}
	if strings.Contains(lines[0], "ATTRS{id") {
		t.Errorf("USB identity written as ATTRS, which cannot match beside ATTRS{name}:\n  %s", lines[0])
	}
	if why := checkButtonGrant(lines[0]); why != "" {
		t.Errorf("%s\n  %s", why, lines[0])
	}

	origUSB := nodeUSB
	t.Cleanup(func() { nodeUSB = origUSB })
	nodes := map[string]evdevkey.USBNode{
		"event10": {Vendor: 0x1a86, Product: 0xfe00, Interface: 1},
		"event13": {Vendor: 0x1a86, Product: 0xfe00, Interface: 0},
	}
	nodeUSB = func(event string) (evdevkey.USBNode, bool) { n, ok := nodes[event]; return n, ok }
	if !buttonNodeGranted(grants, "HID 1a86:fe00", "event13", "oxp-test") {
		t.Error("interface 0's node, on this machine, must be granted")
	}
	if buttonNodeGranted(grants, "HID 1a86:fe00", "event10", "oxp-test") {
		t.Error("interface 1's node shares the name but must not be granted")
	}
	if buttonNodeGranted(grants, "HID 1a86:fe00", "event99", "oxp-test") {
		t.Error("a node whose USB identity cannot be read must not be granted")
	}
}

// TestHidrawGrantsNameAnInterface: a keyboard's interfaces include the one it
// types through, so a hidraw grant by vendor:product alone exposes raw
// keystrokes. The Z13's cover did exactly that (interface 2, Generic Desktop /
// Keyboard) from z13ctl's first commit until 2026-11-15. Every hidraw grant,
// generated and packaged, names the one interface it means.
func TestHidrawGrantsNameAnInterface(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"buildRulesContent": buildRulesContent("users"),
		packagedRulesPath:   readPackaged(t, packagedRulesPath),
	} {
		n := 0
		for _, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") || !strings.Contains(line, `SUBSYSTEM=="hidraw"`) {
				continue
			}
			n++
			if !strings.Contains(line, `ENV{ID_USB_INTERFACE_NUM}==`) {
				t.Errorf("%s grants every interface of a device:\n  %s", name, line)
			}
			// Every ATTRS key in a rule must match on one parent device, and
			// bInterfaceNumber is on the USB interface while idVendor and
			// idProduct are on the USB device above it: this form matches
			// nothing, and shipped once, leaving the keyboard dark at boot.
			if strings.Contains(line, `ATTRS{bInterfaceNumber}`) {
				t.Errorf("%s matches the interface as ATTRS, which can never match beside idVendor:\n  %s", name, line)
			}
		}
		if n == 0 {
			t.Errorf("%s: no hidraw grants at all", name)
		}
	}
}

// The revocation decision: a lighting device's other interfaces are stale, its
// lighting interface and any device setup never granted are not.
func TestStaleHidrawInterface(t *testing.T) {
	t.Parallel()

	grants := []hidGrant{{usb: "0b05:1a30", iface: 0}, {usb: "0b05:18c6", iface: 0}}
	uevent := func(id string) string { return "DRIVER=hid-generic\nHID_ID=" + id + "\nHID_NAME=x\n" }
	for _, c := range []struct {
		desc, uevent, iface string
		granted, stale      bool
	}{
		{"cover lighting interface", uevent("0003:00000B05:00001A30"), "00", true, false},
		{"cover typing interface", uevent("0003:00000B05:00001A30"), "02", false, true},
		{"cover touchpad", uevent("0003:00000B05:00001A30"), "03", false, true},
		{"lightbar's second interface", uevent("0003:00000B05:000018C6"), "01", false, true},
		// The OXP's built-in keyboard: setup once reported it as "a device
		// file grants … which is a keyboard", because the caller read a
		// device id with no stale flag as a grant.
		{"a device setup never granted", uevent("0003:0000258A:0000001E"), "00", false, false},
		{"no HID_ID", "DRIVER=x\n", "02", false, false},
		{"unparseable interface", uevent("0003:00000B05:00001A30"), "zz", false, false},
	} {
		_, granted, stale := staleHidrawInterface(c.uevent, c.iface, grants)
		if granted != c.granted || stale != c.stale {
			t.Errorf("%s: granted, stale = %v, %v; want %v, %v", c.desc, granted, stale, c.granted, c.stale)
		}
	}
}

// A hidraw button is granted read-only on its own interface, and not at all
// on an interface lighting already writes, where a read-only rule after the
// read-write one would take the write away.
func TestHidrawButtonGrants(t *testing.T) {
	t.Parallel()

	zero, two := 0, 2
	targets := setupTargetsFrom([]device.Config{{
		Device:   device.Meta{ID: "ally-test", Match: []device.Match{{Vendor: "V", Product: "P"}}},
		Lighting: &device.LightingConfig{Zones: []device.LightingZoneConfig{{Name: "sticks", Label: "Sticks", USB: "0b05:1abe", Interface: &zero}}},
		Buttons: []device.ButtonConfig{
			{ID: "ac", Label: "A", Method: "hidraw-report", USB: "0b05:1abe", Interface: &two, ReportID: 0x5a, Match: [][]int{{1, 0xa6}}},
			{ID: "cc", Label: "C", Method: "hidraw-report", USB: "0b05:1abe", Interface: &two, ReportID: 0x5a, Match: [][]int{{1, 0x38}}},
			{ID: "lit", Label: "L", Method: "hidraw-report", USB: "0b05:1abe", Interface: &zero, Match: [][]int{{1, 1}}},
		},
	}})
	if len(targets.hidButtons) != 1 || targets.hidButtons[0] != (hidGrant{usb: "0b05:1abe", iface: 2}) {
		t.Fatalf("hidButtons = %+v, want only interface 2 once", targets.hidButtons)
	}
	if got := targets.allHID(); len(got) != 2 {
		t.Errorf("allHID = %+v, want the lighting interface and the button's", got)
	}
}

// A node another tool hid (mode 0000, as InputPlumber leaves the devices it
// manages) is not loosened by the direct grant; any other mode is fair game.
func TestHiddenNode(t *testing.T) {
	dir := t.TempDir()
	hidden, open := dir+"/hidden", dir+"/open"
	for p, mode := range map[string]os.FileMode{hidden: 0, open: 0o600} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	if !hiddenNode(hidden) {
		t.Error("a 0000 node must count as hidden")
	}
	if hiddenNode(open) || hiddenNode(dir+"/missing") {
		t.Error("a 0600 node, or a missing one, is not hidden")
	}
}

// Every loop in the perms unit skips a file that does not exist without
// failing. "[ -e f ] && chgrp …" leaves the loop's status at 1 when the last
// file is missing, and a non-zero ExecStart fails the unit at every boot: on
// any machine without asus-armoury's boot_sound or asus-nb-wmi's deprecated
// ppt_* (the OneXPlayer, or a Z13 kernel built without
// CONFIG_ASUS_WMI_DEPRECATED_ATTRS). Found on the OXP 2026-11-17; the
// ryzen_smu line had been patched alone (25ef8a9), the rest never were.
func TestPermsUnitToleratesMissingFiles(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"buildServiceContent": buildServiceContent("users"),
		packagedServicePath:   readPackaged(t, packagedServicePath),
	} {
		loops := 0
		for _, line := range strings.Split(content, "\n") {
			if !strings.HasPrefix(line, "ExecStart=") || !strings.Contains(line, "for f in") {
				continue
			}
			loops++
			if !strings.Contains(line, `[ -e "$$f" ] || continue;`) {
				t.Errorf("%s: a loop that fails on a missing file:\n  %s", name, line)
			}
		}
		if loops == 0 {
			t.Errorf("%s: no ExecStart loops found", name)
		}
	}
}
