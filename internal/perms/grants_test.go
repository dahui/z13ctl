package perms

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/drivers/evdevkey"
)

var update = flag.Bool("update", false, "rewrite testdata/*.rules from the device files")

// TestMain points the boot pass's root at a directory that does not exist,
// so a test that forgets its temporary tree finds nothing rather than
// changing the machine's permissions.
func TestMain(m *testing.M) {
	root = "/nonexistent-voltaire-perms-test-root"
	os.Exit(m.Run())
}

var testHeader = func(id string) Header { return Header{Device: id, Group: "users", Version: "test"} }

func configs(t *testing.T) []device.Config {
	t.Helper()
	cs, err := device.Configs()
	if err != nil {
		t.Fatalf("device.Configs: %v", err)
	}
	return cs
}

func configByID(t *testing.T, id string) device.Config {
	t.Helper()
	for _, c := range configs(t) {
		if c.Device.ID == id {
			return c
		}
	}
	t.Fatalf("no device file %q", id)
	return device.Config{}
}

func mustFor(t *testing.T, c device.Config) Grants {
	t.Helper()
	g, err := For(c)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestGoldens pins every device file's rules. A change to a device file's
// grants shows up as a reviewed diff; regenerate with
// `go test ./internal/perms -run TestGoldens -update`. A device file with no
// golden, or a golden with no device file, fails.
func TestGoldens(t *testing.T) {
	want := map[string]bool{}
	for _, c := range configs(t) {
		name := c.Device.ID + ".rules"
		want[name] = true
		got := mustFor(t, c).Rules(testHeader(c.Device.ID))
		path := filepath.Join("testdata", name)
		if *update {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		golden, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: no golden (run with -update and review it): %v", c.Device.ID, err)
			continue
		}
		if string(golden) != got {
			t.Errorf("%s: rules differ from %s (run with -update and review the diff)\n--- got ---\n%s", c.Device.ID, path, got)
		}
	}
	files, _ := filepath.Glob("testdata/*.rules")
	for _, f := range files {
		if !want[filepath.Base(f)] {
			t.Errorf("%s has no device file", f)
		}
	}
}

// ruleLines is every non-comment, non-empty line of a rules file.
func ruleLines(rules string) []string {
	var out []string
	for _, l := range strings.Split(rules, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// allRules is every device file's rules plus the synthetic configs below,
// for the guards that must hold on any output.
func allRules(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, c := range append(configs(t), keyboardNodeConfig(), usbInterfaceConfig()) {
		out[c.Device.ID] = mustFor(t, c).Rules(testHeader(c.Device.ID))
	}
	return out
}

// TestRulesGuards: the rules every grant line keeps, on every device.
func TestRulesGuards(t *testing.T) {
	for id, rules := range allRules(t) {
		if !strings.Contains(rules, Marker+"\n") {
			t.Errorf("%s: no marker line", id)
		}
		if strings.Contains(rules, "%!") {
			t.Errorf("%s: format error in the rules:\n%s", id, rules)
		}
		for _, l := range ruleLines(rules) {
			switch {
			case strings.Contains(l, `SUBSYSTEM=="input"`):
				// Read access to a typing keyboard is read access to every
				// keystroke: refuse one, or be pinned to one machine's DMI.
				if !strings.Contains(l, `MODE="0640"`) {
					t.Errorf("%s: input grant is not read-only:\n  %s", id, l)
				}
				guarded := strings.Contains(l, `ENV{ID_INPUT_KEYBOARD}!="1"`)
				scoped := strings.Contains(l, `ATTR{[dmi/id]sys_vendor}==`) && strings.Contains(l, `ATTR{[dmi/id]product_name}==`)
				if !guarded && !scoped {
					t.Errorf("%s: input grant could match a typing keyboard on any machine:\n  %s", id, l)
				}
				if strings.Contains(l, "ATTRS{id") {
					t.Errorf("%s: USB identity as ATTRS cannot match beside ATTRS{name}:\n  %s", id, l)
				}
			case strings.Contains(l, `SUBSYSTEM=="hidraw"`):
				// A keyboard's interfaces include the one it types through.
				if !strings.Contains(l, "ENV{ID_USB_INTERFACE_NUM}==") || strings.Contains(l, "bInterfaceNumber") {
					t.Errorf("%s: hidraw grant does not name its interface as ENV:\n  %s", id, l)
				}
			case strings.Contains(l, "RUN+="):
				if !strings.Contains(l, `[ -e \"$$f\" ] || continue;`) {
					t.Errorf("%s: loop does not skip a missing file:\n  %s", id, l)
				}
				if strings.Contains(strings.ReplaceAll(l, "$$f", ""), "$f") {
					t.Errorf("%s: a bare $f expands to nothing in udev:\n  %s", id, l)
				}
				// powercap holds the package power caps beside the counter.
				if strings.Contains(l, "energy_uj") && (!strings.Contains(l, "chmod g+r") || strings.Contains(l, "g+w")) {
					t.Errorf("%s: the energy counter must be granted read only:\n  %s", id, l)
				}
			default:
				t.Errorf("%s: unrecognised rule line:\n  %s", id, l)
			}
		}
	}
}

// TestEveryMethodHasAGrantCase: For refuses a section method it does not
// know, so a new driver cannot ship without its grants.
func TestEveryMethodHasAGrantCase(t *testing.T) {
	c := configByID(t, "asus-rog-flow-z13-2025")
	f := *c.Fans
	f.Method = "some-new-driver"
	c.Fans = &f
	if _, err := For(c); err == nil || !strings.Contains(err.Error(), "some-new-driver") {
		t.Fatalf("For accepted an unknown method: %v", err)
	}
}

// TestASectionDeclaresItsGrant is the negative control for every capability:
// drop the section from the Z13's file, and its grant goes.
func TestASectionDeclaresItsGrant(t *testing.T) {
	base := configByID(t, "asus-rog-flow-z13-2025")
	no := false
	cases := []struct {
		name   string
		drop   func(*device.Config)
		absent []string
	}{
		{"fans", func(c *device.Config) { c.Fans = nil }, []string{"asus_custom_fan_curve", `ATTR{name}=="asus"`}},
		{"power", func(c *device.Config) { c.Power = nil }, []string{"ppt_*"}},
		{"profiles", func(c *device.Config) { c.Profiles = nil }, []string{"platform-profile"}},
		{"toggles", func(c *device.Config) { c.Toggles = nil }, []string{"boot_sound", "panel_overdrive"}},
		{"cpu", func(c *device.Config) { c.CPU = nil }, []string{"cpufreq/boost"}},
		{"lighting", func(c *device.Config) { c.Lighting = nil }, []string{"hidraw"}},
		{"buttons", func(c *device.Config) { c.Buttons = nil }, []string{"Asus WMI hotkeys"}},
		{"undervolt", func(c *device.Config) { c.Undervolt = nil }, []string{"ryzen_smu"}},
		{"telemetry", func(c *device.Config) { t := *c.Telemetry; t.PowerDraw = ""; c.Telemetry = &t }, []string{"energy_uj"}},
		{"charge limit", func(c *device.Config) { b := *c.Battery; b.ChargeLimit = &no; c.Battery = &b }, []string{"charge_control_end_threshold"}},
		{"a hidden toggle", func(c *device.Config) {
			tg := *c.Toggles
			tg.Entries = append(slices.Clone(tg.Entries), device.ToggleEntry{ID: "panel_overdrive", Hidden: true})
			c.Toggles = &tg
		}, []string{"panel_overdrive"}},
	}
	full := mustFor(t, base).Rules(testHeader(base.Device.ID))
	for _, tc := range cases {
		for _, s := range tc.absent {
			if !strings.Contains(full, s) {
				t.Fatalf("%s: the full Z13 rules lack %q, so this control proves nothing", tc.name, s)
			}
		}
		c := base
		tc.drop(&c)
		rules := mustFor(t, c).Rules(testHeader(c.Device.ID))
		for _, s := range tc.absent {
			if strings.Contains(rules, s) {
				t.Errorf("without %s, the rules still mention %q", tc.name, s)
			}
		}
	}
}

// TestSMUIsGrantedForEitherUser: the OXP needs ryzen_smu for its TDP even
// without an undervolt, and loses it only with neither.
func TestSMUIsGrantedForEitherUser(t *testing.T) {
	c := configByID(t, "onexplayer-x2-mini-pro")
	c.Undervolt = nil
	if !strings.Contains(mustFor(t, c).Rules(testHeader("x")), "ryzen_smu") {
		t.Error("amd-smu power without an undervolt lost the ryzen_smu grant")
	}
	c.Power = nil
	if strings.Contains(mustFor(t, c).Rules(testHeader("x")), "ryzen_smu") {
		t.Error("ryzen_smu granted with neither user declared")
	}
}

// TestNoMachineGrantsAnothersHardware is the point of the redesign: each
// device's rules carry only its own hardware.
func TestNoMachineGrantsAnothersHardware(t *testing.T) {
	for _, tc := range []struct {
		id          string
		absent, has []string
	}{
		{"asus-rog-flow-z13-2025",
			[]string{"1a86", "rumble", "Ally"},
			[]string{`idProduct}=="1a30"`, `idProduct}=="18c6"`, "Asus WMI hotkeys", "asus_custom_fan_curve", "boot_sound", "panel_overdrive", "energy_uj", "ryzen_smu", "charge_control_end_threshold"}},
		{"onexplayer-x2-mini-pro",
			[]string{"0b05", "asus-armoury", "hwmon", "asus-nb-wmi", "charge_control_end_threshold"},
			[]string{"rumble_intensity", "ryzen_smu", "HID 1a86:fe00", `idProduct}=="fe00"`}},
		{"asus-rog-xbox-ally-x",
			// The rings' interface 2 alone: never interface 5, the gamepad,
			// nor the Z13's lighting devices.
			[]string{"1a86", "rumble", "ryzen_smu", "Asus WMI hotkeys", `ID_USB_INTERFACE_NUM}=="05"`, `idProduct}=="1a30"`, `idProduct}=="18c6"`},
			[]string{"mcu_powersave", "ASUS ROG Ally X Gamepad", "asus_custom_fan_curve", "charge_control_end_threshold",
				`idProduct}=="1b4c", ENV{ID_USB_INTERFACE_NUM}=="02"`}},
	} {
		rules := mustFor(t, configByID(t, tc.id)).Rules(testHeader(tc.id))
		for _, s := range tc.absent {
			if strings.Contains(rules, s) {
				t.Errorf("%s: rules mention %q, another machine's hardware", tc.id, s)
			}
		}
		for _, s := range tc.has {
			if !strings.Contains(rules, s) {
				t.Errorf("%s: rules lack %q", tc.id, s)
			}
		}
	}
}

// keyboardNodeConfig is a device file that does not ship: one ordinary button
// and one whose chords arrive on the AT keyboard.
func keyboardNodeConfig() device.Config {
	return device.Config{
		Device: device.Meta{ID: "oxp-test", Match: []device.Match{
			{Vendor: "ONE-NETBOOK", ProductPrefix: "ONEXPLAYER X2"},
			{Vendor: "ONE-NETBOOK", Product: "ONEXPLAYER X2Mini PRO"},
		}},
		Buttons: []device.ButtonConfig{
			{ID: "home", Label: "Home button", Method: "evdev-key", Device: "OXP hotkeys", Keycode: 102},
			{ID: "oxp", Label: "OneXPlayer button", Method: "evdev-key", Device: "AT Translated Set 2 keyboard",
				Keycode: 56, KeyboardNode: "carries only the button chords on this model"},
		},
	}
}

// usbInterfaceConfig names an evdev button's USB interface: the OXP's
// controller has two nodes called "HID 1a86:fe00" and only interface 0
// carries the chords.
func usbInterfaceConfig() device.Config {
	zero := 0
	return device.Config{
		Device: device.Meta{ID: "usb-iface-test", Match: []device.Match{{Vendor: "ONE-NETBOOK", Product: "ONEXPLAYER X2Mini PRO"}}},
		Buttons: []device.ButtonConfig{{ID: "oxp", Label: "OneXPlayer button", Method: "evdev-chord",
			Device: "HID 1a86:fe00", USB: "1a86:fe00", Interface: &zero, Keys: []int{29, 56, 125},
			KeyboardNode: "the controller's interface-0 keyboard carries only the button chords"}},
	}
}

func TestKeyboardNodeGrantIsScopedToItsDevice(t *testing.T) {
	rules := mustFor(t, keyboardNodeConfig()).Rules(testHeader("oxp-test"))
	var lines []string
	for _, l := range ruleLines(rules) {
		if strings.Contains(l, `SUBSYSTEM=="input"`) {
			lines = append(lines, l)
		}
	}
	if len(lines) != 3 { // the ordinary button, and one per DMI pattern
		t.Fatalf("got %d input lines, want 3:\n%s", len(lines), rules)
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
	for _, l := range lines {
		if strings.Contains(l, "AT Translated") && strings.Contains(l, "ENV{ID_INPUT_KEYBOARD}") {
			t.Errorf("the opted-in keyboard node is refused by its own grant:\n  %s", l)
		}
	}
}

func TestEvdevGrantNamesItsUSBInterface(t *testing.T) {
	rules := mustFor(t, usbInterfaceConfig()).Rules(testHeader("usb-iface-test"))
	want := `ENV{ID_VENDOR_ID}=="1a86", ENV{ID_MODEL_ID}=="fe00", ENV{ID_USB_INTERFACE_NUM}=="00"`
	if !strings.Contains(rules, want) {
		t.Errorf("rules missing %s:\n%s", want, rules)
	}
}

// The direct grant follows the same rules as the udev line, through the same
// tests the watcher uses.
func TestInputGrantedFollowsTheGuard(t *testing.T) {
	origKbd, origUSB := isTypingKeyboard, nodeUSB
	t.Cleanup(func() { isTypingKeyboard, nodeUSB = origKbd, origUSB })
	kbd := map[string]bool{"event2": true, "event7": false, "event13": true, "event10": true}
	isTypingKeyboard = func(event string) (bool, error) { return kbd[event], nil }
	nodes := map[string]evdevkey.USBNode{
		"event10": {Vendor: 0x1a86, Product: 0xfe00, Interface: 1},
		"event13": {Vendor: 0x1a86, Product: 0xfe00, Interface: 0},
	}
	nodeUSB = func(event string) (evdevkey.USBNode, bool) { n, ok := nodes[event]; return n, ok }

	kn := mustFor(t, keyboardNodeConfig()).Input
	ui := mustFor(t, usbInterfaceConfig()).Input
	for _, c := range []struct {
		desc        string
		grants      []Input
		name, event string
		dmi, want   bool
	}{
		{"ordinary node", kn, "OXP hotkeys", "event7", false, true},
		{"ordinary name on a keyboard node", kn, "OXP hotkeys", "event2", false, false},
		{"opted-in keyboard, DMI match", kn, "AT Translated Set 2 keyboard", "event2", true, true},
		{"opted-in keyboard, named with --machine", kn, "AT Translated Set 2 keyboard", "event2", false, false},
		{"a name no grant gives", kn, "Some keyboard", "event7", true, false},
		{"the named USB interface", ui, "HID 1a86:fe00", "event13", true, true},
		{"the same name on another interface", ui, "HID 1a86:fe00", "event10", true, false},
		{"an unreadable USB identity", ui, "HID 1a86:fe00", "event99", true, false},
	} {
		if got := inputGranted(c.grants, c.name, c.event, c.dmi); got != c.want {
			t.Errorf("%s: granted = %v, want %v", c.desc, got, c.want)
		}
	}
}

func TestUnit(t *testing.T) {
	u := Unit("/usr/bin/voltaire", testHeader("asus-rog-flow-z13-2025"), "")
	for _, want := range []string{
		Marker + "\n", "Type=oneshot", "RemainAfterExit=yes",
		"ConditionPathExists=/usr/bin/voltaire",
		// Before the pass, so a hidraw grant udev applied to a renumbered
		// node is reset (docs/decisions/0006); "-" so it never fails the unit.
		"ExecStartPre=-udevadm settle --timeout=30\nExecStart=/usr/bin/voltaire setup --perms-only --group users\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
	if u := Unit("/usr/local/bin/voltaire", testHeader("x"), "onexplayer-x2-mini-pro"); !strings.Contains(u,
		"ExecStart=/usr/local/bin/voltaire setup --perms-only --group users --machine onexplayer-x2-mini-pro\n") {
		t.Errorf("a --machine run must reach the boot pass:\n%s", u)
	}
}

func TestParseHeader(t *testing.T) {
	rules := mustFor(t, configByID(t, "asus-rog-xbox-ally-x")).Rules(testHeader("asus-rog-xbox-ally-x"))
	if dev, group := ParseHeader(rules); dev != "asus-rog-xbox-ally-x" || group != "users" {
		t.Errorf("ParseHeader = %q, %q", dev, group)
	}
	if dev, group := ParseHeader("# some other file\n"); dev != "" || group != "" {
		t.Errorf("ParseHeader on a foreign file = %q, %q", dev, group)
	}
}

// A hidraw button is granted read-only on its own interface, and not at all
// on an interface lighting already writes, where a read-only rule after the
// read-write one would take the write away.
func TestHidrawButtonGrants(t *testing.T) {
	zero, two := 0, 2
	c := device.Config{
		Device: device.Meta{ID: "hid-test"},
		Lighting: &device.LightingConfig{Method: "aura-hid", Zones: []device.LightingZoneConfig{
			{Name: "rings", Label: "Rings", USB: "1a86:fe00", Interface: &zero},
		}},
		Buttons: []device.ButtonConfig{
			{ID: "a", Label: "A", Method: "hidraw-report", USB: "1a86:fe00", Interface: &zero},
			{ID: "b", Label: "B", Method: "hidraw-report", USB: "1a86:fe00", Interface: &two},
		},
	}
	got := mustFor(t, c).HIDRaw
	want := []HIDRaw{
		{USB: "1a86:fe00", Interface: 0, Write: true, Why: "lighting: Rings"},
		{USB: "1a86:fe00", Interface: 2, Why: "button: B"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("HIDRaw = %+v, want %+v", got, want)
	}
}
