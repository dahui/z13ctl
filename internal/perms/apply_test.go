package perms

// apply_test.go — the boot pass against a temporary tree, with real chown and
// chmod. An unprivileged test can chown only to its own groups, so a revoke
// returns files to the test's group (rootGID) and is checked by mode; "setup's
// group" is the same gid, or gid+1 where a test needs a group nothing carries.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/drivers/evdevkey"
)

type tree struct {
	t   *testing.T
	gid int
}

func newTree(t *testing.T) tree {
	t.Helper()
	dir := t.TempDir()
	origRoot, origGID := root, rootGID
	root, rootGID = dir, os.Getgid()
	t.Cleanup(func() { root, rootGID = origRoot, origGID })
	return tree{t: t, gid: os.Getgid()}
}

func (tr tree) file(path, content string, mode os.FileMode) string {
	tr.t.Helper()
	p := root + path
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // WriteFile honours the umask
		tr.t.Fatal(err)
	}
	return p
}

func (tr tree) mode(path string) os.FileMode {
	tr.t.Helper()
	fi, err := os.Stat(root + path)
	if err != nil {
		tr.t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// hidraw makes /sys/class/hidraw/<name> and its /dev node. An empty iface is
// a node with no USB interface (an I2C device).
func (tr tree) hidraw(name, hidID, iface string, devMode os.FileMode) {
	tr.file("/sys/class/hidraw/"+name+"/device/uevent", "HID_ID="+hidID+"\n", 0o644)
	if iface != "" {
		tr.file("/sys/class/hidraw/"+name+"/bInterfaceNumber", iface+"\n", 0o644)
	}
	tr.file("/dev/"+name, "", devMode)
}

func TestApplyGrantsWhatTheDeviceDeclares(t *testing.T) {
	tr := newTree(t)
	g := mustFor(t, configByID(t, "asus-rog-flow-z13-2025"))

	tr.file("/sys/class/platform-profile/platform-profile-0/profile", "balanced", 0o644)
	tr.file("/sys/class/hwmon/hwmon3/name", "asus_custom_fan_curve\n", 0o444)
	tr.file("/sys/class/hwmon/hwmon3/pwm1_auto_point1_pwm", "0", 0o644)
	tr.file("/sys/class/hwmon/hwmon3/temp1_input", "50000", 0o444) // not pwm*: untouched
	tr.file("/sys/class/hwmon/hwmon4/name", "k10temp\n", 0o444)
	tr.file("/sys/class/hwmon/hwmon4/pwm1", "0", 0o644) // another hwmon: untouched
	tr.file("/sys/class/power_supply/BAT0/type", "Battery\n", 0o444)
	tr.file("/sys/class/power_supply/BAT0/charge_control_end_threshold", "80", 0o644)
	tr.file("/sys/class/power_supply/AC0/type", "Mains\n", 0o444)
	tr.file("/sys/class/powercap/intel-rapl:0/name", "package-0\n", 0o444)
	tr.file("/sys/class/powercap/intel-rapl:0/energy_uj", "1", 0o400)
	tr.file("/sys/class/powercap/intel-rapl:0/constraint_0_power_limit_uw", "1", 0o644)
	tr.file("/sys/class/firmware-attributes/asus-armoury/attributes/boot_sound/current_value", "0", 0o644)
	tr.file("/sys/devices/system/cpu/cpufreq/boost", "1", 0o644)
	// panel_overdrive, ppt_*, ryzen_smu absent: nothing to do, no error.

	res := Apply(g, nil, Options{Group: "test", GID: tr.gid})
	if res.Failed != 0 {
		t.Fatalf("failures: %+v", res)
	}
	for path, want := range map[string]os.FileMode{
		"/sys/class/platform-profile/platform-profile-0/profile":                          0o664,
		"/sys/class/hwmon/hwmon3/pwm1_auto_point1_pwm":                                    0o664,
		"/sys/class/hwmon/hwmon3/temp1_input":                                             0o444,
		"/sys/class/hwmon/hwmon4/pwm1":                                                    0o644,
		"/sys/class/power_supply/BAT0/charge_control_end_threshold":                       0o664,
		"/sys/class/powercap/intel-rapl:0/energy_uj":                                      0o440,
		"/sys/class/powercap/intel-rapl:0/constraint_0_power_limit_uw":                    0o644,
		"/sys/class/firmware-attributes/asus-armoury/attributes/boot_sound/current_value": 0o664,
		"/sys/devices/system/cpu/cpufreq/boost":                                           0o664,
	} {
		if got := tr.mode(path); got != want {
			t.Errorf("%s: mode %04o, want %04o", path, got, want)
		}
	}
	first := Apply(g, nil, Options{Group: "test", GID: tr.gid}) // the pass above already ran; this is the second
	if first.Granted != 0 {
		t.Errorf("a second pass changed %d files; it should find them granted", first.Granted)
	}
	// A pass that changes nothing still says what it checked: an all-zero
	// summary read as "found nothing" on the first hardware run.
	if first.Already == 0 {
		t.Errorf("a second pass counted nothing as already granted: %v", first)
	}
}

func TestApplyOnAnEmptyTreeDoesNothing(t *testing.T) {
	newTree(t)
	for _, c := range configs(t) {
		if res := Apply(mustFor(t, c), nil, Options{Group: "test", GID: os.Getgid()}); res != (Result{}) {
			t.Errorf("%s: %+v on an empty tree", c.Device.ID, res)
		}
	}
}

func TestApplyDryRunChangesNothing(t *testing.T) {
	tr := newTree(t)
	tr.file("/sys/devices/system/cpu/cpufreq/boost", "1", 0o644)
	res := Apply(mustFor(t, configByID(t, "asus-rog-flow-z13-2025")), nil, Options{Group: "test", GID: tr.gid, DryRun: true})
	if res.Granted != 1 || tr.mode("/sys/devices/system/cpu/cpufreq/boost") != 0o644 {
		t.Errorf("dry run: %+v, mode %04o", res, tr.mode("/sys/devices/system/cpu/cpufreq/boost"))
	}
}

func TestApplyHIDRaw(t *testing.T) {
	tr := newTree(t)
	g := mustFor(t, configByID(t, "asus-rog-flow-z13-2025"))    // 0b05:1a30 and 0b05:18c6, interface 0
	tr.hidraw("hidraw0", "0003:00000B05:00001A30", "00", 0o600) // the cover's lighting interface
	tr.hidraw("hidraw2", "0003:00000B05:00001A30", "02", 0o660) // its keyboard, left with our group by an old setup
	tr.hidraw("hidraw5", "0003:00001A86:0000FE00", "02", 0o000) // hidden by InputPlumber; another device's anyway
	tr.hidraw("hidraw6", "0003:0000046D:0000C52B", "00", 0o660) // a device no file names

	var log strings.Builder
	res := Apply(g, Known(configs(t)), Options{Group: "test", GID: tr.gid, Out: &log})
	if res.Failed != 0 {
		t.Fatalf("failures: %+v", res)
	}
	// The boot pass's journal must name what it revoked, by interface: the
	// node number alone moves between boots.
	if want := "revoked stale hidraw (interface 02 of 0b05:1a30): /dev/hidraw2"; !strings.Contains(log.String(), want) {
		t.Errorf("revoke not logged; want %q in:\n%s", want, log.String())
	}
	for path, want := range map[string]os.FileMode{
		"/dev/hidraw0": 0o660, // granted read-write
		"/dev/hidraw2": 0o600, // stale: another interface of a granted device
		"/dev/hidraw5": 0o000, // another device file's interface, hidden: untouched
		"/dev/hidraw6": 0o660, // unknown device: none of setup's business
	} {
		if got := tr.mode(path); got != want {
			t.Errorf("%s: mode %04o, want %04o", path, got, want)
		}
	}
}

func TestApplyLeavesAHiddenGrantedNodeAlone(t *testing.T) {
	tr := newTree(t)
	g := mustFor(t, configByID(t, "onexplayer-x2-mini-pro")) // hidraw-report button on 1a86:fe00 interface 2
	tr.hidraw("hidraw4", "0003:00001A86:0000FE00", "02", 0o000)
	res := Apply(g, nil, Options{Group: "test", GID: tr.gid})
	if tr.mode("/dev/hidraw4") != 0 || res.Skipped != 1 {
		t.Errorf("a node InputPlumber hid was loosened: mode %04o, %+v", tr.mode("/dev/hidraw4"), res)
	}
}

func TestApplyRefusesAKeyboardInterface(t *testing.T) {
	tr := newTree(t)
	g := mustFor(t, configByID(t, "asus-rog-flow-z13-2025"))
	tr.hidraw("hidraw0", "0003:00000B05:00001A30", "00", 0o660)
	// Usage Page (Generic Desktop), Usage (Keyboard), Collection (Application).
	tr.file("/sys/class/hidraw/hidraw0/device/report_descriptor", "\x05\x01\x09\x06\xa1\x01\xc0", 0o444)
	Apply(g, nil, Options{Group: "test", GID: tr.gid})
	if got := tr.mode("/dev/hidraw0"); got != 0o600 {
		t.Errorf("a granted interface whose descriptor is a keyboard: mode %04o, want 0600", got)
	}
}

func TestApplyInputNodes(t *testing.T) {
	tr := newTree(t)
	origKbd, origUSB := isTypingKeyboard, nodeUSB
	t.Cleanup(func() { isTypingKeyboard, nodeUSB = origKbd, origUSB })
	isTypingKeyboard = func(event string) (bool, error) { return event == "event2", nil }
	nodeUSB = func(string) (evdevkey.USBNode, bool) { return evdevkey.USBNode{}, false }

	tr.file("/sys/class/input/event5/device/name", "Asus WMI hotkeys\n", 0o444)
	tr.file("/dev/input/event5", "", 0o660) // an older setup's write bit
	tr.file("/sys/class/input/event2/device/name", "AT Translated Set 2 keyboard\n", 0o444)
	tr.file("/dev/input/event2", "", 0o660)
	tr.file("/sys/class/input/event9/device/name", "Asus WMI hotkeys\n", 0o444)
	tr.file("/dev/input/event9", "", 0o000) // hidden

	Apply(mustFor(t, configByID(t, "asus-rog-flow-z13-2025")), nil, Options{Group: "test", GID: tr.gid, DMIMatch: true})
	for path, want := range map[string]os.FileMode{
		"/dev/input/event5": 0o640, // exactly g=r
		"/dev/input/event2": 0o660, // not named by the Z13: untouched
		"/dev/input/event9": 0o000,
	} {
		if got := tr.mode(path); got != want {
			t.Errorf("%s: mode %04o, want %04o", path, got, want)
		}
	}
}

func TestRevert(t *testing.T) {
	tr := newTree(t)
	tr.file("/sys/devices/system/cpu/cpufreq/boost", "1", 0o664)
	tr.file("/sys/class/powercap/intel-rapl:0/name", "package-0\n", 0o444)
	tr.file("/sys/class/powercap/intel-rapl:0/energy_uj", "1", 0o440)
	tr.hidraw("hidraw0", "0003:00000B05:00001A30", "00", 0o660)

	var all []Grants
	for _, c := range configs(t) {
		all = append(all, mustFor(t, c))
	}
	if other := Revert(all, Options{Group: "test", GID: tr.gid + 1}); other.Revoked != 0 {
		t.Errorf("revert touched %d files not carrying setup's group", other.Revoked)
	}
	res := Revert(all, Options{Group: "test", GID: tr.gid})
	if res.Revoked != 3 || res.Failed != 0 {
		t.Errorf("revert: %+v, want 3 revoked (boost, energy_uj, hidraw0), each once", res)
	}
	for path, want := range map[string]os.FileMode{
		"/sys/devices/system/cpu/cpufreq/boost":      0o644,
		"/sys/class/powercap/intel-rapl:0/energy_uj": 0o400,
		"/dev/hidraw0": 0o600,
	} {
		if got := tr.mode(path); got != want {
			t.Errorf("%s: mode %04o after revert, want %04o", path, got, want)
		}
	}
}

// TestStrayHIDRawGrants is the Z13's 2026-11-26 boot: udev's coldplug event
// for the keyboard's interface 0, queued as hidraw0, landed after a driver
// rebind had given that name to the ITE sensor hub (I2C, in no device file).
// With no other rules file handing out the group, the pass resets it; with
// one, the group could be that file's, and it is left.
func TestStrayHIDRawGrants(t *testing.T) {
	g := mustFor(t, configByID(t, "asus-rog-flow-z13-2025"))
	setup := func(t *testing.T) tree {
		tr := newTree(t)
		tr.hidraw("hidraw0", "0018:0000048D:00008353", "", 0o660)   // the sensor hub, granted by the stray event
		tr.hidraw("hidraw7", "0003:00000B05:00001A30", "00", 0o660) // the keyboard's lighting interface
		tr.hidraw("hidraw9", "0003:0000046D:0000C52B", "00", 0o600) // an unknown device without the group
		tr.file("/etc/udev/rules.d/99-voltaire.rules", `SUBSYSTEM=="hidraw", GROUP="test"`+"\n", 0o644)
		tr.file("/usr/lib/udev/rules.d/99-voltaire.rules", `GROUP="test"`+"\n", 0o644) // same name: shadowed
		tr.file("/usr/lib/udev/rules.d/50-other.rules", "# GROUP=\"test\" in a comment\n"+`GROUP="testers"`+"\n", 0o644)
		return tr
	}

	t.Run("only ours", func(t *testing.T) {
		tr := setup(t)
		only, other := OnlyOursGrant("test", "/etc/udev/rules.d/99-voltaire.rules")
		if !only {
			t.Fatalf("OnlyOursGrant = false (%s); comments, other groups and the shadowed copy must not count", other)
		}
		var log strings.Builder
		res := Apply(g, Known(configs(t)), Options{Group: "test", GID: tr.gid, Out: &log, OnlyOurs: only})
		if tr.mode("/dev/hidraw0") != 0o600 || tr.mode("/dev/hidraw7") != 0o660 || tr.mode("/dev/hidraw9") != 0o600 {
			t.Errorf("modes: sensor hub %04o (want 0600), keyboard %04o (want 0660), unknown %04o (want 0600); %v",
				tr.mode("/dev/hidraw0"), tr.mode("/dev/hidraw7"), tr.mode("/dev/hidraw9"), res)
		}
		if want := "revoked stray hidraw (048d:8353, not a USB interface): /dev/hidraw0"; !strings.Contains(log.String(), want) {
			t.Errorf("want %q in:\n%s", want, log.String())
		}
		// setup --remove takes it back the same way.
		tr.file("/dev/hidraw0", "", 0o660)
		if r := Revert([]Grants{g}, Options{Group: "test", GID: tr.gid, OnlyOurs: true}); tr.mode("/dev/hidraw0") != 0o600 {
			t.Errorf("Revert left the sensor hub at %04o: %v", tr.mode("/dev/hidraw0"), r)
		}
	})

	t.Run("another rules file grants the group", func(t *testing.T) {
		tr := setup(t)
		tr.file("/etc/udev/rules.d/70-mine.rules", `SUBSYSTEM=="hidraw", ATTRS{idVendor}=="048d", GROUP="test"`+"\n", 0o644)
		only, other := OnlyOursGrant("test", "/etc/udev/rules.d/99-voltaire.rules")
		if only || other != "/etc/udev/rules.d/70-mine.rules" {
			t.Fatalf("OnlyOursGrant = %v, %q; want false naming 70-mine.rules", only, other)
		}
		Apply(g, Known(configs(t)), Options{Group: "test", GID: tr.gid, OnlyOurs: only})
		if tr.mode("/dev/hidraw0") != 0o660 {
			t.Errorf("a node another rule may have granted was reset to %04o", tr.mode("/dev/hidraw0"))
		}
	})
}
