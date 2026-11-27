// Package perms decides which device nodes and sysfs attributes voltaire's
// unprivileged daemon needs on one machine, and renders that decision as the
// udev rules file and as the boot-time pass `voltaire setup --perms-only`
// applies directly.
//
// Grants come from the matched device file only: a section the file does not
// declare grants nothing, and a method perms has no case for is an error, so a
// new driver cannot ship without saying what it needs. Until 2026-11-26 setup
// granted every embedded device file's targets on every machine, so one
// static rules file could be packaged; packages now run setup at install
// instead, and a machine grants only what its own hardware needs.
//
// One Grants value feeds both renderers (Rules and Apply), so the udev rules
// and the boot pass cannot disagree about what is granted.
package perms

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/drivers/asus"
	"github.com/dahui/voltaire/v2/internal/drivers/hidoxp"
)

// Grants is everything one machine's device file needs granted.
type Grants struct {
	Device string   // the device file's id
	HIDRaw []HIDRaw // hidraw interfaces, sorted and deduplicated
	Input  []Input  // evdev nodes, sorted and deduplicated
	Sysfs  []Sysfs  // sysfs attributes, in a fixed order
}

// HIDRaw is one USB interface's hidraw node. The interface is part of the
// grant because a keyboard's other interfaces include the one it types
// through: granting by vendor:product alone gave every process in the group
// the Z13 cover's raw keystrokes, from z13ctl's first commit until 2026-11-15.
type HIDRaw struct {
	USB       string // "vvvv:pppp", lowercase hex
	Interface int    // bInterfaceNumber
	Write     bool   // read-write (lighting) or read-only (buttons)
	Why       string
}

// Input is one evdev node a hardware button is read from: always read-only,
// since a writable input node accepts injected events. An ordinary grant is
// written so it can never match a typing keyboard; a keyboard_node grant
// (DMI set) is written scoped to its device's DMI match instead, so it can
// never apply on another machine. Read access to a typing keyboard is read
// access to every keystroke, which is the line both forms keep.
type Input struct {
	Name      string         // the input device's sysfs name
	USB       string         // "vvvv:pppp" when the device data narrows the node to a USB interface
	Interface int            // that interface's number; meaningful only with USB
	DMI       []device.Match // keyboard_node only
	Why       string
}

// Sysfs is a set of attribute files under the directories Dir matches.
type Sysfs struct {
	Capability string // what needs it, for the rules file's comments
	Why        string
	Dir        string   // a directory glob
	Where      *Attr    // an attribute of the directory that must match; nil: any
	Files      []string // file globs relative to Dir
	Write      bool     // g+w, or g+r (powercap's energy counter only)

	// Udev is the rule's match keys, "" when no udev event reaches the files
	// (cpufreq's boost switch and ryzen_smu are plain kobjects, not devices):
	// the boot pass is then the only grant. EventDir says the files are under
	// the event's own device (/sys%p) rather than at Dir's absolute path.
	Udev     string
	EventDir bool
}

// Attr is one directory attribute and the value it must read.
type Attr struct {
	File, Value string
}

// The sysfs locations granted. Absolute, and joined under root by the boot
// pass (root is "" outside tests).
const (
	armouryAttrs     = "/sys/class/firmware-attributes/asus-armoury/attributes"
	asusNBWMI        = "/sys/devices/platform/asus-nb-wmi"
	ryzenSMUDir      = "/sys/kernel/ryzen_smu_drv"
	cpufreqDir       = "/sys/devices/system/cpu/cpufreq"
	hwmonDirs        = "/sys/class/hwmon/*"
	powerSupplyDirs  = "/sys/class/power_supply/*"
	profileDirs      = "/sys/class/platform-profile/*"
	powercapDirs     = "/sys/class/powercap/*"
	armouryUdevMatch = `ACTION=="add", SUBSYSTEM=="firmware-attributes", KERNEL=="asus-armoury"`
)

// For derives one machine's grants from its device file. Its error is a build
// defect: a section method perms does not know, which must be given a case
// here before the device file can ship.
func For(c device.Config) (Grants, error) {
	g := Grants{Device: c.Device.ID}
	var errs []string
	unknown := func(section, method string) {
		errs = append(errs, fmt.Sprintf("%s method %q", section, method))
	}

	if c.Lighting != nil {
		if c.Lighting.Method != "aura-hid" {
			unknown("lighting", c.Lighting.Method)
		}
		for _, z := range c.Lighting.Zones {
			v, p, err := z.USBIDs()
			if err != nil || z.Interface == nil {
				continue // refused by Validate; never grant a whole device
			}
			g.HIDRaw = append(g.HIDRaw, HIDRaw{
				USB: fmt.Sprintf("%04x:%04x", v, p), Interface: *z.Interface, Write: true,
				Why: "lighting: " + z.Label,
			})
		}
	}

	for _, b := range c.Buttons {
		switch b.Method {
		case "hidraw-report":
			v, p, err := b.USBIDs()
			if err != nil || b.Interface == nil {
				continue // refused by Validate
			}
			g.HIDRaw = append(g.HIDRaw, HIDRaw{
				USB: fmt.Sprintf("%04x:%04x", v, p), Interface: *b.Interface,
				Why: "button: " + b.Label,
			})
		case "evdev-key", "evdev-chord":
			if b.Device == "" {
				continue
			}
			in := Input{Name: b.Device, Why: "button: " + b.Label}
			if b.USB != "" && b.Interface != nil {
				if v, p, err := b.USBIDs(); err == nil {
					in.USB, in.Interface = fmt.Sprintf("%04x:%04x", v, p), *b.Interface
				}
			}
			if b.KeyboardNode != "" {
				in.DMI = c.Device.Match
			}
			g.Input = append(g.Input, in)
		default:
			unknown("button", b.Method)
		}
	}

	if c.Profiles != nil {
		if c.Profiles.Method != "platform-profile" {
			unknown("profiles", c.Profiles.Method)
		}
		g.Sysfs = append(g.Sysfs, Sysfs{
			Capability: "profiles", Why: "firmware performance profile",
			Dir: profileDirs, Files: []string{"profile"}, Write: true,
			Udev: `ACTION=="add", SUBSYSTEM=="platform-profile"`, EventDir: true,
		})
	}

	if c.Fans != nil {
		if c.Fans.Method != "asus-nb-wmi" {
			unknown("fans", c.Fans.Method)
		}
		curves, readings := asus.FanHwmonNames()
		g.Sysfs = append(g.Sysfs,
			Sysfs{
				Capability: "fans", Why: "custom fan curve points and mode",
				Dir: hwmonDirs, Where: &Attr{"name", curves}, Files: []string{"pwm*"}, Write: true,
				Udev: fmt.Sprintf(`ACTION=="add", SUBSYSTEM=="hwmon", ATTR{name}==%q`, curves), EventDir: true,
			},
			Sysfs{
				Capability: "fans", Why: "fan mode (full speed, auto)",
				Dir: hwmonDirs, Where: &Attr{"name", readings}, Files: []string{"pwm*_enable"}, Write: true,
				Udev: fmt.Sprintf(`ACTION=="add", SUBSYSTEM=="hwmon", ATTR{name}==%q`, readings), EventDir: true,
			})
	}

	smu := false
	if c.Power != nil {
		switch c.Power.Method {
		case "asus-nb-wmi-ppt":
			// The driver writes asus-armoury's attributes when they are
			// writable and asus-nb-wmi's deprecated ones otherwise, choosing
			// per call, so both are granted. Both are usually created late in
			// asus_nb_wmi's probe(), after their udev events: the boot pass is
			// the reliable grant and the rules cover what already exists.
			g.Sysfs = append(g.Sysfs,
				Sysfs{
					Capability: "power", Why: "TDP (asus-armoury PPT limits)",
					Dir: armouryAttrs, Files: []string{"ppt_*/current_value"}, Write: true,
					Udev: armouryUdevMatch,
				},
				Sysfs{
					Capability: "power", Why: "TDP (asus-nb-wmi PPT limits, the fallback)",
					Dir: asusNBWMI, Files: []string{"ppt_*"}, Write: true,
					Udev: `ACTION=="add", SUBSYSTEM=="platform", KERNEL=="asus-nb-wmi"`,
				})
		case "amd-smu":
			smu = true
		default:
			unknown("power", c.Power.Method)
		}
	}

	if c.Toggles != nil {
		if c.Toggles.Method != "asus-armoury" {
			unknown("toggles", c.Toggles.Method)
		}
		// The driver offers every allowlisted attribute the kernel exposes,
		// declared or not (asus.enumerateToggles), so the grant is the
		// allowlist minus what the device file hides — never anything outside
		// the allowlist, which is the safety boundary.
		hidden := map[string]bool{}
		for _, e := range c.Toggles.Entries {
			if e.Hidden {
				hidden[e.ID] = true
			}
		}
		var files []string
		for _, id := range asus.SafeToggleIDs() {
			if !hidden[id] {
				files = append(files, id+"/current_value")
			}
		}
		if len(files) > 0 {
			g.Sysfs = append(g.Sysfs, Sysfs{
				Capability: "toggles", Why: "firmware switches (" + strings.Join(trimSuffixes(files), ", ") + ")",
				Dir: armouryAttrs, Files: files, Write: true, Udev: armouryUdevMatch,
			})
		}
	}

	if c.Battery != nil {
		if c.Battery.Method != "power-supply" {
			unknown("battery", c.Battery.Method)
		}
		// From the declared config, not a Gaming-Mode-yielded one: the grant
		// outlives sessions, and the desktop session uses it.
		if c.Battery.Caps().ChargeLimit {
			// The attribute is added by asus-wmi's battery hook, which emits no
			// uevent of its own, so it may appear after the battery's add
			// event: change events catch it later, and the boot pass is the
			// reliable grant.
			g.Sysfs = append(g.Sysfs, Sysfs{
				Capability: "battery", Why: "charge limit",
				Dir: powerSupplyDirs, Where: &Attr{"type", "Battery"},
				Files: []string{"charge_control_end_threshold"}, Write: true,
				Udev:     `ACTION=="add|change", SUBSYSTEM=="power_supply", ATTR{type}=="Battery"`,
				EventDir: true,
			})
		}
	}

	if c.CPU != nil {
		switch c.CPU.Boost {
		case "":
		case "cpufreq":
			g.Sysfs = append(g.Sysfs, Sysfs{
				Capability: "cpu", Why: "CPU boost switch",
				Dir: cpufreqDir, Files: []string{"boost"}, Write: true,
			})
		default:
			unknown("cpu boost", c.CPU.Boost)
		}
	}

	if c.Undervolt != nil {
		if c.Undervolt.Method != "ryzen-smu-co" {
			unknown("undervolt", c.Undervolt.Method)
		}
		smu = true
	}
	if smu {
		g.Sysfs = append(g.Sysfs, Sysfs{
			Capability: "smu", Why: "ryzen_smu mailbox (TDP or undervolt)",
			Dir: ryzenSMUDir, Files: []string{"smu_args", "mp1_smu_cmd", "rsmu_cmd"}, Write: true,
		})
	}

	if c.Telemetry != nil {
		switch c.Telemetry.PowerDraw {
		case "":
		case "rapl":
			// Read only: powercap also holds the package power caps, and a
			// graph is no reason to hand those to the group.
			g.Sysfs = append(g.Sysfs, Sysfs{
				Capability: "telemetry", Why: "package power (RAPL energy counter, read only)",
				Dir: powercapDirs, Where: &Attr{"name", "package-0"}, Files: []string{"energy_uj"},
				Udev: `ACTION=="add", SUBSYSTEM=="powercap", ATTR{name}=="package-0"`, EventDir: true,
			})
		default:
			unknown("telemetry power_draw", c.Telemetry.PowerDraw)
		}
	}

	if c.Rumble != nil {
		if c.Rumble.Method != "hid-oxp" {
			unknown("rumble", c.Rumble.Method)
		}
		// hid-oxp creates the attribute when it binds, so the bind event finds
		// it. It binds several of the controller's HID devices and only one
		// carries it, which the rule's missing-file skip absorbs.
		g.Sysfs = append(g.Sysfs, Sysfs{
			Capability: "rumble", Why: "controller vibration strength",
			Dir: hidoxp.DriverDir + "/*", Files: []string{hidoxp.RumbleAttr}, Write: true,
			Udev: `ACTION=="bind", SUBSYSTEM=="hid", DRIVER=="hid-oxp"`, EventDir: true,
		})
	}

	if len(errs) > 0 {
		return Grants{}, fmt.Errorf("perms: %s: no grant is defined for %s", c.Device.ID, strings.Join(errs, ", "))
	}
	g.normalize()
	return g, nil
}

// Known is every embedded device file's hidraw grants, the universe the boot
// pass checks for stale grants: another interface of a device some file
// grants, left carrying setup's group by an older setup or another machine's
// rules, is reset.
func Known(configs []device.Config) []HIDRaw {
	var all []HIDRaw
	for _, c := range configs {
		if g, err := For(c); err == nil {
			all = append(all, g.HIDRaw...)
		}
	}
	return all
}

// normalize sorts and deduplicates the hidraw and input grants. A read-only
// button grant on an interface lighting already writes is dropped: the
// read-write rule covers it, and a second, read-only rule after it would take
// the write away.
func (g *Grants) normalize() {
	slices.SortStableFunc(g.HIDRaw, func(a, b HIDRaw) int {
		if c := strings.Compare(a.USB, b.USB); c != 0 {
			return c
		}
		if a.Interface != b.Interface {
			return a.Interface - b.Interface
		}
		if a.Write != b.Write {
			if a.Write {
				return -1
			}
			return 1
		}
		return 0
	})
	g.HIDRaw = slices.CompactFunc(g.HIDRaw, func(a, b HIDRaw) bool {
		return a.USB == b.USB && a.Interface == b.Interface
	})
	slices.SortStableFunc(g.Input, func(a, b Input) int { return strings.Compare(a.key(), b.key()) })
	g.Input = slices.CompactFunc(g.Input, func(a, b Input) bool { return a.key() == b.key() })
}

func (in Input) key() string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%t", in.Name, in.USB, in.Interface, in.DMI != nil)
}

// trimSuffixes turns "boot_sound/current_value" into "boot_sound".
func trimSuffixes(files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i], _, _ = strings.Cut(f, "/")
	}
	return out
}
