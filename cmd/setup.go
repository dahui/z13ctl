package cmd

// setup.go — "setup" subcommand: install udev rules for non-root device access.

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/drivers/asus"
	"github.com/dahui/voltaire/v2/internal/drivers/evdevkey"
	"github.com/dahui/voltaire/v2/internal/drivers/hidrawbtn"
)

const rulesPath = "/etc/udev/rules.d/99-voltaire.rules"
const servicePath = "/etc/systemd/system/voltaire-perms.service"

// The pre-rename artifact locations. setup removes them — the rules file only
// after verifying it is ours (see cleanupOldArtifacts), the unit with an
// explicit disable first: z13ctl-perms.service is Type=oneshot with
// RemainAfterExit=yes, so without the disable it stays active holding stale
// grants until the next reboot.
const oldRulesPath = "/etc/udev/rules.d/99-z13ctl.rules"
const oldServicePath = "/etc/systemd/system/z13ctl-perms.service"
const oldServiceName = "z13ctl-perms.service"

var (
	setupGroup    string
	permsOnlyFlag bool
)

// setupTargets is what the device files name for setup to grant: the lighting
// zones' USB ids and the hardware buttons' input-device names.
//
// It is every embedded device file's, not only this machine's. The packaged
// rules and perms unit are one file shipped to every machine, and
// cmd/setup_test.go holds them to what setup generates, so the generated
// content cannot depend on which machine runs it. A grant for hardware the
// machine lacks matches nothing.
type setupTargets struct {
	hid        []hidGrant    // lighting interfaces, read-write; sorted, deduplicated
	hidButtons []hidGrant    // hidraw-report button interfaces, read-only, none already in hid
	buttons    []buttonGrant // evdev button nodes; sorted by name, deduplicated
}

// allHID is every hidraw interface setup grants, lighting and buttons alike:
// the interfaces revokeStaleHidrawGrants must leave alone.
func (t setupTargets) allHID() []hidGrant { return append(slices.Clone(t.hid), t.hidButtons...) }

// hidGrant is one USB interface a lighting zone writes. The interface is part
// of the grant because a keyboard's other interfaces include the one it types
// through: granting by vendor:product alone gave every process in the group
// the cover's raw keystrokes over hidraw, from z13ctl's first commit until
// 2026-11-15.
type hidGrant struct {
	usb   string // "vvvv:pppp", lowercase hex
	iface int    // bInterfaceNumber
}

// The interface is matched as ENV{ID_USB_INTERFACE_NUM}, which udev's usb_id
// sets on the hidraw node itself, and never as ATTRS{bInterfaceNumber}: every
// ATTRS key in one rule must match on the same parent, and bInterfaceNumber
// lives on the USB interface while idVendor/idProduct live on the USB device
// above it. The ATTRS form matched nothing, so the first boot after it shipped
// (2026-11-15) left the lighting interfaces root-only and the keyboard dark.

// buttonGrant is one input node a button reads. An ordinary grant (no
// deviceID) is written so it can never match a typing keyboard; a
// keyboard_node grant is written scoped to its device's DMI match instead, so
// it can never apply on another machine. Read access to a typing keyboard is
// read access to every keystroke, which is the line this keeps.
type buttonGrant struct {
	name     string         // the input device's sysfs name
	deviceID string         // keyboard_node only: the device file that opted it in
	dmi      []device.Match // keyboard_node only: that file's DMI patterns
}

// deviceSetupTargets collects setupTargets from the embedded device files. Its
// error is a build defect (the files are validated in CI); setup refuses on it
// rather than writing rules that silently grant nothing.
func deviceSetupTargets() (setupTargets, error) {
	configs, err := device.Configs()
	if err != nil {
		return setupTargets{}, err
	}
	return setupTargetsFrom(configs), nil
}

// setupTargetsFrom is deviceSetupTargets over the configs given, so tests can
// drive a device file that does not ship (a keyboard_node button, say).
func setupTargetsFrom(configs []device.Config) setupTargets {
	var t setupTargets
	for _, c := range configs {
		if c.Lighting != nil {
			for _, z := range c.Lighting.Zones {
				v, p, err := z.USBIDs()
				if err != nil || z.Interface == nil {
					continue // refused by Validate; never grant a whole device
				}
				t.hid = append(t.hid, hidGrant{usb: fmt.Sprintf("%04x:%04x", v, p), iface: *z.Interface})
			}
		}
		for _, b := range c.Buttons {
			if b.Method == "hidraw-report" {
				v, p, err := b.USBIDs()
				if err != nil || b.Interface == nil {
					continue // refused by Validate
				}
				t.hidButtons = append(t.hidButtons, hidGrant{usb: fmt.Sprintf("%04x:%04x", v, p), iface: *b.Interface})
				continue
			}
			if b.Device == "" {
				continue
			}
			g := buttonGrant{name: b.Device}
			if b.KeyboardNode != "" {
				g.deviceID, g.dmi = c.Device.ID, c.Device.Match
			}
			t.buttons = append(t.buttons, g)
		}
	}
	byInterface := func(a, b hidGrant) int {
		if c := strings.Compare(a.usb, b.usb); c != 0 {
			return c
		}
		return a.iface - b.iface
	}
	slices.SortFunc(t.hid, byInterface)
	t.hid = slices.Compact(t.hid)
	// A button on an interface lighting already writes needs no grant of its
	// own: the read-write one covers it, and a second, read-only rule after it
	// would take the write away.
	t.hidButtons = slices.DeleteFunc(t.hidButtons, func(g hidGrant) bool { return slices.Contains(t.hid, g) })
	slices.SortFunc(t.hidButtons, byInterface)
	t.hidButtons = slices.Compact(t.hidButtons)
	slices.SortFunc(t.buttons, func(a, b buttonGrant) int {
		return strings.Compare(a.name+"\x00"+a.deviceID, b.name+"\x00"+b.deviceID)
	})
	t.buttons = slices.CompactFunc(t.buttons, func(a, b buttonGrant) bool {
		return a.name == b.name && a.deviceID == b.deviceID
	})
	return t
}

// buttonRules is the udev grant for each button node: read-only, because a
// writable input node accepts injected events.
func buttonRules(grants []buttonGrant, group string) string {
	var b strings.Builder
	for _, g := range grants {
		if g.deviceID == "" {
			fmt.Fprintf(&b, `ACTION=="add", SUBSYSTEM=="input", KERNEL=="event*", ATTRS{name}=="%s", ENV{ID_INPUT_KEYBOARD}!="1", MODE="0640", GROUP="%s"`+"\n", g.name, group)
			continue
		}
		for _, m := range g.dmi {
			product := m.Product
			if product == "" {
				product = m.ProductPrefix + "*"
			}
			fmt.Fprintf(&b, `ACTION=="add", SUBSYSTEM=="input", KERNEL=="event*", ATTRS{name}=="%s", ATTR{[dmi/id]sys_vendor}=="%s", ATTR{[dmi/id]product_name}=="%s", MODE="0640", GROUP="%s"`+"\n",
				g.name, m.Vendor, product, group)
		}
	}
	return b.String()
}

func buildRulesContent(group string) string {
	targets, _ := deviceSetupTargets() // refused earlier by RunE on error
	var hid strings.Builder
	for _, g := range targets.hid {
		v, p, _ := strings.Cut(g.usb, ":")
		fmt.Fprintf(&hid, `SUBSYSTEM=="hidraw", ATTRS{idVendor}=="%s", ATTRS{idProduct}=="%s", ENV{ID_USB_INTERFACE_NUM}=="%02x", MODE="0660", GROUP="%s"`+"\n", v, p, g.iface, group)
	}
	for _, g := range targets.hidButtons {
		v, p, _ := strings.Cut(g.usb, ":")
		fmt.Fprintf(&hid, `SUBSYSTEM=="hidraw", ATTRS{idVendor}=="%s", ATTRS{idProduct}=="%s", ENV{ID_USB_INTERFACE_NUM}=="%02x", MODE="0640", GROUP="%s"`+"\n", v, p, g.iface, group)
	}
	curves, readings := asus.FanHwmonNames()
	return fmt.Sprintf(`# 99-voltaire.rules — voltaire device access
#
# Grants members of the %s group read/write access to:
#   - the HID interface of every lighting zone the device files declare (RGB
#     control) — that interface only, never the device's others, one of which
#     is how a keyboard types
#   - platform-profile attributes for performance profile control
#   - the battery charge_control_end_threshold for battery limit control
#   - the hardware buttons' input devices for the daemon button watcher (read
#     only, and never a typing keyboard: ENV{ID_INPUT_KEYBOARD}!="1")
#   - asus-armoury firmware-attributes (%s)
#   - cpufreq's global boost switch (perms unit only — udev cannot see it)
#   - hwmon fan curve attributes (%s + %s pwm_enable)
#   - asus-armoury PPT power limit attributes for TDP control (asus-nb-wmi ppt_* as fallback)
#   - powercap RAPL energy_uj for package-power telemetry (read only)
#
# Generated by: voltaire setup
# To remove: sudo rm /etc/udev/rules.d/99-voltaire.rules && sudo udevadm control --reload-rules
%sACTION=="add", SUBSYSTEM=="platform-profile", RUN+="/usr/bin/chgrp %s /sys%%p/profile", RUN+="/usr/bin/chmod g+w /sys%%p/profile"
ACTION=="add", SUBSYSTEM=="platform-profile", KERNELS=="asus-nb-wmi", RUN+="/usr/bin/chgrp %s /sys/class/power_supply/BAT0/charge_control_end_threshold", RUN+="/usr/bin/chmod g+w /sys/class/power_supply/BAT0/charge_control_end_threshold"
%s%sACTION=="add", SUBSYSTEM=="firmware-attributes", KERNEL=="asus-armoury", RUN+="/bin/sh -c 'for f in /sys/class/firmware-attributes/asus-armoury/attributes/ppt_*/current_value; do [ -e \"$$f\" ] || continue; chgrp %s \"$$f\" && chmod g+w \"$$f\"; done'"
ACTION=="add", SUBSYSTEM=="hwmon", ATTR{name}=="%s", RUN+="/bin/sh -c 'chgrp %s /sys%%p/pwm*; chmod g+w /sys%%p/pwm*'"
ACTION=="add", SUBSYSTEM=="hwmon", ATTR{name}=="%s", RUN+="/bin/sh -c 'for f in /sys%%p/pwm*_enable; do [ -e \"$$f\" ] && chgrp %s \"$$f\" && chmod g+w \"$$f\"; done'"
# ppt_* are usually created after this add event (later in asus_nb_wmi probe()), so
# voltaire-perms.service is what applies this grant at boot; this rule covers the files
# that already exist. A missing file is skipped rather than ending the script non-zero,
# which logged "failed with exit code 1" on every boot (issue #25).
ACTION=="add", SUBSYSTEM=="platform", KERNEL=="asus-nb-wmi", RUN+="/bin/sh -c 'for f in /sys/devices/platform/asus-nb-wmi/ppt_*; do [ -e \"$$f\" ] || continue; chgrp %s \"$$f\" && chmod g+w \"$$f\"; done'"
ACTION=="add", SUBSYSTEM=="powercap", ATTR{name}=="package-0", RUN+="/bin/sh -c 'for f in /sys%%p/energy_uj; do [ -e \"$$f\" ] && chgrp %s \"$$f\" && chmod g+r \"$$f\"; done'"
`, group, strings.Join(asus.SafeToggleIDs(), ", "), curves, readings,
		hid.String(), group, group, buttonRules(targets.buttons, group),
		toggleUdevRules(group), group, curves, group, readings, group, group, group)
}

// toggleUdevRules grants each firmware toggle the daemon may write — the
// driver's allowlist, so a newly allowlisted attribute is granted with no
// second list to update, and nothing outside it ever is.
func toggleUdevRules(group string) string {
	var b strings.Builder
	for _, id := range asus.SafeToggleIDs() {
		p := toggleAttrPath(id)
		fmt.Fprintf(&b, `ACTION=="add", SUBSYSTEM=="firmware-attributes", KERNEL=="asus-armoury", RUN+="/usr/bin/chgrp %s %s", RUN+="/usr/bin/chmod g+w %s"`+"\n", group, p, p)
	}
	return b.String()
}

// toggleAttrPath is an allowlisted toggle's value file.
func toggleAttrPath(id string) string {
	return "/sys/class/firmware-attributes/asus-armoury/attributes/" + id + "/current_value"
}

// toggleAttrPaths is every allowlisted toggle's value file, space-separated.
func toggleAttrPaths() string {
	ids := asus.SafeToggleIDs()
	paths := make([]string, len(ids))
	for i, id := range ids {
		paths[i] = toggleAttrPath(id)
	}
	return strings.Join(paths, " ")
}

// buildServiceContent generates the voltaire-perms.service unit file content.
// Handles sysfs files that cannot be set by udev rules:
//   - charge_control_end_threshold: created late in asus_nb_wmi probe()
//   - firmware-attributes current_value: may be created after parent ADD event
//   - ppt_* PPT attributes: created late in the same asus_nb_wmi probe()
//   - ryzen_smu_drv files: kobject under /sys/kernel/, not a udev device
//
// Keep this in sync with contrib/systemd/system/voltaire-perms.service, which is
// what package installs ship; cmd/setup_test.go asserts they do not drift.
// Note the "$$f" doubling is systemd's escape for a literal dollar sign, not Go
// or fmt.Sprintf escaping — a bare "$f" expands to the empty string.
func buildServiceContent(group string) string {
	return fmt.Sprintf(`[Unit]
Description=voltaire sysfs permissions (battery + firmware-attributes + PPT + cpufreq boost + ryzen_smu + powercap)
# charge_control_end_threshold on BAT0 is created by asus_nb_wmi late in probe(),
# after all observable udev child-device events. udev RUN+= cannot catch it.
# firmware-attributes current_value files may be created after the parent ADD event.
# ppt_* on the asus-nb-wmi platform device are created late in the same probe(),
# as are asus-armoury's ppt_*/current_value under firmware-attributes,
# so the udev rule for them is best-effort only and this service is the reliable path.
# cpufreq's global boost switch is not a udev device at all — the path is a
# plain kobject, so udevadm info answers "Unknown device" and no rule can
# grant it. This unit is the only path.
# ryzen_smu files are under /sys/kernel/, not a udev subsystem.
# powercap energy_uj is 0400 root:root under the Platypus mitigation and is
# granted READ only — write would hand out the package power caps themselves.
After=sysinit.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'for f in /sys/class/power_supply/BAT*/charge_control_end_threshold; do [ -e "$$f" ] && chgrp %s "$$f" && chmod g+w "$$f"; done'
ExecStart=/bin/sh -c 'for f in %s; do [ -e "$$f" ] && chgrp %s "$$f" && chmod g+w "$$f"; done'
ExecStart=/bin/sh -c 'for f in /sys/class/firmware-attributes/asus-armoury/attributes/ppt_*/current_value; do [ -e "$$f" ] || continue; chgrp %s "$$f" && chmod g+w "$$f"; done'
ExecStart=/bin/sh -c 'for f in /sys/devices/platform/asus-nb-wmi/ppt_*; do [ -e "$$f" ] && chgrp %s "$$f" && chmod g+w "$$f"; done'
ExecStart=/bin/sh -c 'for f in /sys/devices/system/cpu/cpufreq/boost; do [ -e "$$f" ] && chgrp %s "$$f" && chmod g+w "$$f"; done'
ExecStart=/bin/sh -c 'for f in /sys/kernel/ryzen_smu_drv/smu_args /sys/kernel/ryzen_smu_drv/mp1_smu_cmd /sys/kernel/ryzen_smu_drv/rsmu_cmd; do [ -e "$$f" ] && chgrp %s "$$f" && chmod g+w "$$f" || true; done'
ExecStart=/bin/sh -c 'for f in /sys/class/powercap/*/energy_uj; do [ -e "$$f" ] && chgrp %s "$$f" && chmod g+r "$$f"; done'

[Install]
WantedBy=multi-user.target
`, group, toggleAttrPaths(), group, group, group, group, group, group)
}

// applySysfsPerms scans live sysfs for files managed by voltaire and either
// applies chgrp/chmod immediately (dryRun=false) or prints what would be done
// (dryRun=true). Errors are silently skipped for missing/inaccessible paths.
// cpuBoostPath is cpufreq's global boost switch, granted group-write so the
// daemon (which runs as the user) can toggle it. The grant is narrow on
// purpose: it is one file, and the only thing it confers is the ability to turn
// boost clocks off — not the power *caps* beside it, which is the same
// distinction that keeps the powercap grant read-only.
const cpuBoostPath = "/sys/devices/system/cpu/cpufreq/boost"

func applySysfsPerms(group string, dryRun bool) {
	chgrpChmod := func(p, mode string) {
		if dryRun {
			fmt.Printf("  chgrp %s %s && chmod %s %s\n", group, p, mode, p)
			return
		}
		_ = exec.Command("chgrp", group, p).Run()
		_ = exec.Command("chmod", mode, p).Run()
	}

	for _, class := range []struct {
		dir  string
		attr string
	}{
		{"/sys/class/platform-profile", "profile"},
		{"/sys/class/power_supply", "charge_control_end_threshold"},
	} {
		entries, err := os.ReadDir(class.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			p := class.dir + "/" + e.Name() + "/" + class.attr
			if _, err := os.Stat(p); err != nil {
				continue
			}
			chgrpChmod(p, "g+w")
		}
	}

	// Firmware-attributes (asus-armoury): the toggles the daemon may write.
	for _, id := range asus.SafeToggleIDs() {
		p := toggleAttrPath(id)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		chgrpChmod(p, "g+w")
	}

	// hwmon fan curve attributes: scan by name for the correct hwmon device.
	curves, readings := asus.FanHwmonNames()
	hwmonEntries, hwErr := os.ReadDir("/sys/class/hwmon")
	if hwErr == nil {
		for _, e := range hwmonEntries {
			namePath := "/sys/class/hwmon/" + e.Name() + "/name"
			nameBytes, rErr := os.ReadFile(namePath)
			if rErr != nil {
				continue
			}
			name := strings.TrimSpace(string(nameBytes))
			hwBase := "/sys/class/hwmon/" + e.Name()
			switch name {
			case curves:
				// All pwm* files (curve points + pwm_enable).
				pwmFiles, _ := os.ReadDir(hwBase)
				for _, f := range pwmFiles {
					if strings.HasPrefix(f.Name(), "pwm") {
						chgrpChmod(hwBase+"/"+f.Name(), "g+w")
					}
				}
			case readings:
				// Only pwm*_enable files (fan mode control).
				pwmFiles, _ := os.ReadDir(hwBase)
				for _, f := range pwmFiles {
					if strings.HasPrefix(f.Name(), "pwm") && strings.HasSuffix(f.Name(), "_enable") {
						chgrpChmod(hwBase+"/"+f.Name(), "g+w")
					}
				}
			}
		}
	}

	// PPT power limit attributes: asus-armoury's, which voltaire writes when the
	// kernel exposes them, and asus-nb-wmi's as the fallback.
	if armouryPPT, globErr := filepath.Glob("/sys/class/firmware-attributes/asus-armoury/attributes/ppt_*/current_value"); globErr == nil {
		for _, p := range armouryPPT {
			chgrpChmod(p, "g+w")
		}
	}
	pptFiles, pptErr := os.ReadDir("/sys/devices/platform/asus-nb-wmi")
	if pptErr == nil {
		for _, f := range pptFiles {
			if strings.HasPrefix(f.Name(), "ppt_") {
				chgrpChmod("/sys/devices/platform/asus-nb-wmi/"+f.Name(), "g+w")
			}
		}
	}

	// cpufreq's global boost switch. Not reachable by udev at all — the path is
	// a plain kobject under /sys/devices/system/cpu, not a device, so
	// `udevadm info` answers "Unknown device" — which makes the perms unit the
	// only path for it and is why there is no matching rule below.
	if _, bErr := os.Stat(cpuBoostPath); bErr == nil {
		chgrpChmod(cpuBoostPath, "g+w")
	}

	// ryzen_smu sysfs files for Curve Optimizer (undervolt).
	if _, smuErr := os.Stat("/sys/kernel/ryzen_smu_drv"); smuErr == nil {
		for _, attr := range []string{"smu_args", "mp1_smu_cmd", "rsmu_cmd"} {
			p := "/sys/kernel/ryzen_smu_drv/" + attr
			if _, err := os.Stat(p); err != nil {
				continue
			}
			chgrpChmod(p, "g+w")
		}
	}

	// powercap RAPL energy counter, for package-power telemetry. Read only:
	// the same directory holds the power-cap controls, and voltaire has no
	// business handing those out to set a graph.
	if capDirs, capErr := os.ReadDir("/sys/class/powercap"); capErr == nil {
		for _, d := range capDirs {
			p := "/sys/class/powercap/" + d.Name() + "/energy_uj"
			if _, err := os.Stat(p); err != nil {
				continue
			}
			chgrpChmod(p, "g+r")
		}
	}

	// The hardware buttons' input devices, by the names the device files give,
	// under the same two rules as the udev grant: read-only, and never a typing
	// keyboard unless this machine's own device file opted that node in.
	targets, _ := deviceSetupTargets()
	matchedID := ""
	if c, err := device.MatchConfig(); err == nil {
		matchedID = c.Device.ID
	}
	entries, err := os.ReadDir("/sys/class/input")
	if err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "event") {
				continue
			}
			nameBytes, err := os.ReadFile("/sys/class/input/" + e.Name() + "/device/name")
			if err != nil {
				continue
			}
			if buttonNodeGranted(targets.buttons, strings.TrimSpace(string(nameBytes)), e.Name(), matchedID) {
				chgrpChmod("/dev/input/"+e.Name(), "g=r") // exact: drops a write bit an older setup granted
			}
		}
	}

	revokeStaleHidrawGrants(targets.allHID(), group, dryRun)
}

// revokeStaleHidrawGrants takes setup's group back off the hidraw interfaces
// of a lighting device that are not its lighting interface. udev leaves a
// node's permissions alone once no rule sets them, so the per-device grant
// every release before 2026-11-15 wrote — which reached the Z13 cover's
// typing interface — would otherwise persist on an installed machine until
// the node is next created. A node is reset to the kernel's default
// (root:root 0600) only if it still carries setup's group, so access some
// other tool granted is left alone.
func revokeStaleHidrawGrants(grants []hidGrant, group string, dryRun bool) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return
	}
	nodes, _ := filepath.Glob("/sys/class/hidraw/hidraw*")
	for _, n := range nodes {
		uevent, err := os.ReadFile(n + "/device/uevent")
		if err != nil {
			continue
		}
		iface, err := os.ReadFile(n + "/device/../bInterfaceNumber")
		if err != nil {
			continue
		}
		usb, stale := staleHidrawInterface(string(uevent), strings.TrimSpace(string(iface)), grants)
		// A udev rule cannot read a report descriptor, so a device file that
		// names a keyboard's interface by mistake would grant its keystrokes.
		// Setup can read it: such a node is revoked too, and said out loud.
		keyboard := false
		if !stale && usb != "" {
			if desc, err := os.ReadFile(n + "/device/report_descriptor"); err == nil && hidrawbtn.IsKeyboardDescriptor(desc) {
				keyboard = true
				fmt.Printf("Warning: a device file grants %s, interface %s of %s, which is a keyboard; "+
					"refusing it (please report this as a device-file bug).\n",
					"/dev/"+filepath.Base(n), strings.TrimSpace(string(iface)), usb)
			}
		}
		if !stale && !keyboard {
			continue
		}
		dev := "/dev/" + filepath.Base(n)
		var st syscall.Stat_t
		if syscall.Stat(dev, &st) != nil || int(st.Gid) != gid {
			continue
		}
		if dryRun {
			fmt.Printf("  revoke %s from %s (interface %s of %s is not its lighting interface)\n", group, dev, strings.TrimSpace(string(iface)), usb)
			continue
		}
		if os.Chown(dev, 0, 0) == nil && os.Chmod(dev, 0o600) == nil {
			fmt.Printf("Removed group %s from %s (interface %s of %s is not its lighting interface).\n",
				group, dev, strings.TrimSpace(string(iface)), usb)
		}
	}
}

// staleHidrawInterface reports whether a hidraw node — its HID uevent and its
// USB bInterfaceNumber — belongs to a lighting device but is not an interface
// that device's grant names, and which device ("vvvv:pppp") it belongs to.
func staleHidrawInterface(uevent, iface string, grants []hidGrant) (string, bool) {
	var usb string
	for _, line := range strings.Split(uevent, "\n") {
		id, ok := strings.CutPrefix(line, "HID_ID=")
		if !ok {
			continue
		}
		parts := strings.Split(id, ":") // bus:vendor:product, eight hex digits each
		if len(parts) != 3 || len(parts[1]) < 4 || len(parts[2]) < 4 {
			return "", false
		}
		usb = strings.ToLower(parts[1][len(parts[1])-4:] + ":" + parts[2][len(parts[2])-4:])
	}
	n, err := strconv.ParseUint(iface, 16, 8)
	if usb == "" || err != nil {
		return "", false
	}
	known := false
	for _, g := range grants {
		if g.usb != usb {
			continue
		}
		known = true
		if g.iface == int(n) {
			return usb, false
		}
	}
	return usb, known
}

// isTypingKeyboard is the watcher's keyboard test, a var so tests can stand
// in for sysfs.
var isTypingKeyboard = evdevkey.IsTypingKeyboard

// buttonNodeGranted reports whether the input node event, named name, gets
// the direct grant. A keyboard_node grant applies only when the device file
// that opted it in is the one matching this machine; any other grant only to
// a node that is not a typing keyboard (an unreadable bitmap counts as one).
func buttonNodeGranted(grants []buttonGrant, name, event, matchedID string) bool {
	for _, g := range grants {
		if g.name != name {
			continue
		}
		if g.deviceID != "" {
			if g.deviceID == matchedID {
				return true
			}
			continue
		}
		if kbd, err := isTypingKeyboard(event); err == nil && !kbd {
			return true
		}
	}
	return false
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

	oldUnitEnabled := exec.Command("systemctl", "is-enabled", "--quiet", oldServiceName).Run() == nil
	_, oldUnitStat := os.Stat(oldServicePath)
	if !oldUnitEnabled && oldUnitStat != nil {
		return
	}
	if dryRun {
		fmt.Printf("Would run: systemctl disable --now %s\n", oldServiceName)
		if oldUnitStat == nil {
			fmt.Printf("Would remove old unit file %s\n", oldServicePath)
		}
		return
	}
	if out, err := exec.Command("systemctl", "disable", "--now", oldServiceName).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "note: systemctl disable --now %s failed: %v\n%s", oldServiceName, err, out)
	} else {
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
	Short: "Install udev rules and boot service for non-root device access",
	Long: `Writes ` + rulesPath + ` granting members of a group read/write
access to the lighting HID devices and hardware-button input devices the
device files name, the platform-profile attribute for performance profile
switching, the battery charge threshold for battery limit control, the
firmware toggles voltaire may write, hwmon fan curve attributes, and the PPT
power limit attributes for TDP control. The grants cover every supported
device, so the file is the same on every machine. Then reloads and triggers
udev so the rules take effect immediately.

Also installs ` + servicePath + `, a small systemd oneshot that
runs 'chgrp' and 'chmod g+w' at boot on the battery charge threshold, the
asus-armoury firmware-attributes, the PPT power limit attributes, the cpufreq
boost switch and the ryzen_smu files, and grants read-only access to the
powercap RAPL energy counter used for package-power telemetry. This is required because those attributes are created late in
the asus_nb_wmi kernel driver's probe() sequence (after all udev events have
already fired), or live outside any udev subsystem.

Use --perms-only to skip writing the rules file and only reapply sysfs
permissions (useful after a group change or kernel module reload).

Use --dry-run to preview exactly what would be written and which sysfs files
would be chmod'd, without making any changes (no root required).

This command must be run with sudo (unless --dry-run is used).`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if _, err := deviceSetupTargets(); err != nil {
			return fmt.Errorf("reading the device files: %w", err)
		}
		if dryRunFlag {
			if permsOnlyFlag {
				fmt.Println("=== DRY RUN: would reapply sysfs permissions ===")
			} else {
				fmt.Println("=== DRY RUN (no changes will be made) ===")
				fmt.Printf("\nWould write %s:\n\n", rulesPath)
				fmt.Print(buildRulesContent(setupGroup))
				fmt.Println("Would run: udevadm control --reload-rules")
				fmt.Println("Would run: udevadm trigger --subsystem-match=hidraw")
				fmt.Printf("\nWould write %s:\n\n", servicePath)
				fmt.Print(buildServiceContent(setupGroup))
				fmt.Println("Would run: systemctl daemon-reload")
				fmt.Println("Would run: systemctl enable --now voltaire-perms.service")
				cleanupOldArtifacts(true)
			}
			fmt.Println("\nWould apply sysfs permissions (files present on this system):")
			applySysfsPerms(setupGroup, true)
			return nil
		}

		if os.Getuid() != 0 {
			return fmt.Errorf("this command requires root — run with sudo:\n  sudo voltaire setup\n\nTo preview changes without root: voltaire --dry-run setup")
		}

		// --perms-only: just reapply sysfs permissions, skip rules and service.
		if permsOnlyFlag {
			applySysfsPerms(setupGroup, false)
			return nil
		}

		content := buildRulesContent(setupGroup)
		if err := os.WriteFile(rulesPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rulesPath, err)
		}
		fmt.Printf("Wrote %s (group: %s)\n", rulesPath, setupGroup)

		// Remove the pre-rename artifacts after the new rules exist and before
		// udev reloads, so there is no window with neither rules file in place
		// and the reload below picks up the removal too.
		cleanupOldArtifacts(false)

		if out, err := exec.Command("udevadm", "control", "--reload-rules").CombinedOutput(); err != nil {
			return fmt.Errorf("udevadm control --reload-rules: %w\n%s", err, out)
		}
		fmt.Println("Reloaded udev rules.")

		if out, err := exec.Command("udevadm", "trigger", "--subsystem-match=hidraw").CombinedOutput(); err != nil {
			return fmt.Errorf("udevadm trigger: %w\n%s", err, out)
		}
		fmt.Println("Triggered udev rules.")

		// systemd 259+ does not execute RUN{program} for synthetic udevadm trigger
		// events, so apply sysfs permissions directly. The udev rules cover
		// subsequent boots via real kernel ADD events.
		applySysfsPerms(setupGroup, false)
		fmt.Println("Applied sysfs permissions.")

		// Install the battery permissions service. charge_control_end_threshold is
		// created late in asus_nb_wmi probe(), after all udev events have fired, so
		// udev RUN+= cannot set its permissions reliably at boot.
		svcContent := buildServiceContent(setupGroup)
		if err := os.WriteFile(servicePath, []byte(svcContent), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", servicePath, err)
		}
		fmt.Printf("Wrote %s\n", servicePath)
		if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "note: systemctl daemon-reload failed: %v\n%s", err, out)
			fmt.Fprintf(os.Stderr, "Enable the service manually: sudo systemctl enable --now voltaire-perms.service\n")
		} else if out, err := exec.Command("systemctl", "enable", "--now", "voltaire-perms.service").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "note: systemctl enable failed: %v\n%s", err, out)
			fmt.Fprintf(os.Stderr, "Enable the service manually: sudo systemctl enable --now voltaire-perms.service\n")
		} else {
			fmt.Println("Enabled voltaire-perms.service.")
		}

		fmt.Printf("\nDone. Members of the '%s' group can now run voltaire without sudo.\n", setupGroup)
		fmt.Println("This covers RGB control, performance profiles, battery charge limiting,")
		fmt.Println("the hardware button, firmware toggles, fan curves, TDP, CPU boost,")
		fmt.Println("and undervolt (if ryzen_smu is installed).")
		fmt.Printf("If your user is not in '%s', add it with:\n", setupGroup)
		fmt.Printf("  sudo usermod -aG %s $USER\n", setupGroup)
		fmt.Println("Then log out and back in for the group change to take effect.")
		return nil
	},
}

func init() {
	setupCmd.Flags().StringVar(&setupGroup, "group", "users", "Group to grant device access to")
	setupCmd.Flags().BoolVar(&permsOnlyFlag, "perms-only", false, "Reapply sysfs permissions without writing udev rules or service")
	rootCmd.AddCommand(setupCmd)
}
