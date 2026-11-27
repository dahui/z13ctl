package perms

// apply.go — the boot pass: the same grants Rules writes, applied directly to
// what exists now. It is what `voltaire setup` runs after writing the rules
// (systemd 259+ does not execute RUN for a synthetic `udevadm trigger`), and
// what voltaire-perms.service runs at every boot for the attributes created
// after their udev event.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dahui/voltaire/v2/internal/drivers/evdevkey"
	"github.com/dahui/voltaire/v2/internal/drivers/hidrawbtn"
)

// root prefixes every absolute path the boot pass touches: "" in production,
// a temporary tree in tests. rootGID is the group a revoke returns a file to:
// root's in production, one an unprivileged test may chown to in tests.
var (
	root    = ""
	rootGID = 0
)

// The input-node tests the button watcher applies, vars so tests can stand in
// for sysfs.
var (
	isTypingKeyboard = evdevkey.IsTypingKeyboard
	nodeUSB          = evdevkey.NodeUSB
)

// Options says how to apply.
type Options struct {
	Group string
	GID   int
	// DMIMatch says the device file was matched to this machine by DMI (not
	// named with --machine). A keyboard_node grant applies only then: like
	// the DMI terms in its udev rule, it must never reach another machine.
	DMIMatch bool
	DryRun   bool
	Out      io.Writer
}

// Result counts what a pass did.
type Result struct {
	Granted, Revoked, Skipped, Failed int
}

// Apply grants g to what exists now and resets stale hidraw grants: another
// interface of a device in known (every device file's hidraw grants) that
// still carries the group, which an older setup or another machine's rules
// may have left. An absent file is nothing to do; a failed change is reported
// and counted, never fatal, so the boot unit does not fail for one attribute.
func Apply(g Grants, known []HIDRaw, opt Options) Result {
	a := applier{opt: opt}
	for _, s := range g.Sysfs {
		bit := os.FileMode(0o040)
		if s.Write {
			bit = 0o020
		}
		for _, f := range s.files() {
			a.set(f, func(m os.FileMode) os.FileMode { return m | bit }, s.Capability)
		}
	}
	a.inputs(g.Input)
	a.hidraw(g.HIDRaw, known)
	return a.res
}

// Revert takes the group back off everything any of grants names that still
// carries it: sysfs attributes lose the group bit and return to root's group,
// hidraw nodes return to the kernel's root:root 0600. Input nodes are left to
// the caller, which re-triggers udev so its default rules restore them.
func Revert(grants []Grants, opt Options) Result {
	a := applier{opt: opt}
	var hid []HIDRaw
	seen := map[string]bool{} // several device files name the same attribute
	for _, g := range grants {
		for _, s := range g.Sysfs {
			bits := os.FileMode(0o040)
			if s.Write {
				bits = 0o020
			}
			for _, f := range s.files() {
				if seen[f] {
					continue
				}
				seen[f] = true
				a.revert(f, func(m os.FileMode) os.FileMode { return m &^ bits }, s.Capability)
			}
		}
		hid = append(hid, g.HIDRaw...)
	}
	for _, n := range hidrawNodes() {
		if _, ok := classifyHIDRaw(n, hid); ok {
			a.revert(n.dev, func(os.FileMode) os.FileMode { return 0o600 }, "hidraw")
		}
	}
	return a.res
}

type applier struct {
	opt Options
	res Result
}

func (a *applier) printf(format string, args ...any) {
	if a.opt.Out != nil {
		_, _ = fmt.Fprintf(a.opt.Out, format, args...)
	}
}

// set gives path the group and the mode change, unless it already has both.
func (a *applier) set(path string, mode func(os.FileMode) os.FileMode, what string) {
	fi, err := os.Stat(path)
	if err != nil {
		return // absent: nothing to grant
	}
	want := mode(fi.Mode().Perm())
	if gidOf(fi) == a.opt.GID && fi.Mode().Perm() == want {
		return
	}
	if a.opt.DryRun {
		a.printf("  grant %s: %s → group %s, mode %04o\n", what, display(path), a.opt.Group, want)
		a.res.Granted++
		return
	}
	if err := os.Chown(path, -1, a.opt.GID); err != nil {
		a.fail(path, err)
		return
	}
	if err := os.Chmod(path, want); err != nil {
		a.fail(path, err)
		return
	}
	a.res.Granted++
}

// revert undoes a grant on path if it still carries the group. A hidden node
// (mode 0000) is another tool's and is never touched, revoke or not.
func (a *applier) revert(path string, mode func(os.FileMode) os.FileMode, what string) {
	fi, err := os.Stat(path)
	if err != nil || gidOf(fi) != a.opt.GID || fi.Mode().Perm() == 0 {
		return
	}
	want := mode(fi.Mode().Perm())
	if a.opt.DryRun {
		a.printf("  revoke %s: %s → group root, mode %04o\n", what, display(path), want)
		a.res.Revoked++
		return
	}
	if err := os.Chown(path, -1, rootGID); err != nil {
		a.fail(path, err)
		return
	}
	if err := os.Chmod(path, want); err != nil {
		a.fail(path, err)
		return
	}
	a.res.Revoked++
}

func (a *applier) fail(path string, err error) {
	a.printf("  warning: %s: %v\n", display(path), err)
	a.res.Failed++
}

// inputs grants the button nodes: exactly g=r (dropping a write bit an older
// setup granted), never a typing keyboard unless the device file opted the
// node in and matched this machine, and never a node another tool hid.
func (a *applier) inputs(grants []Input) {
	if len(grants) == 0 {
		return
	}
	events, _ := filepath.Glob(root + "/sys/class/input/event*")
	for _, e := range events {
		event := filepath.Base(e)
		name, err := os.ReadFile(e + "/device/name")
		if err != nil || !inputGranted(grants, strings.TrimSpace(string(name)), event, a.opt.DMIMatch) {
			continue
		}
		dev := root + "/dev/input/" + event
		if hidden(dev) {
			a.printf("  leaving %s alone: another tool has hidden it (mode 0000; InputPlumber does this to the devices it manages)\n", display(dev))
			a.res.Skipped++
			continue
		}
		a.set(dev, func(m os.FileMode) os.FileMode { return m&^0o070 | 0o040 }, "button")
	}
}

// inputGranted reports whether the input node event, named name, is granted.
// A grant naming a USB interface applies only to the node on it; a
// keyboard_node grant only when its device file matched this machine by DMI;
// any other only to a node that is not a typing keyboard (an unreadable
// capability bitmap counts as one).
func inputGranted(grants []Input, name, event string, dmiMatch bool) bool {
	for _, g := range grants {
		if g.Name != name {
			continue
		}
		if g.USB != "" {
			n, ok := nodeUSB(event)
			if !ok || fmt.Sprintf("%04x:%04x", n.Vendor, n.Product) != g.USB || n.Interface != g.Interface {
				continue
			}
		}
		if g.DMI != nil {
			if dmiMatch {
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

// hidrawNode is one /sys/class/hidraw entry, identified.
type hidrawNode struct {
	sys, dev  string
	usb       string // "vvvv:pppp"
	iface     int
	ifaceText string
}

func hidrawNodes() []hidrawNode {
	paths, _ := filepath.Glob(root + "/sys/class/hidraw/hidraw*")
	var out []hidrawNode
	for _, n := range paths {
		uevent, err := os.ReadFile(n + "/device/uevent")
		if err != nil {
			continue
		}
		ifaceRaw, err := os.ReadFile(n + "/device/../bInterfaceNumber")
		if err != nil {
			continue
		}
		usb := hidUSB(string(uevent))
		text := strings.TrimSpace(string(ifaceRaw))
		iface, err := strconv.ParseUint(text, 16, 8)
		if usb == "" || err != nil {
			continue
		}
		out = append(out, hidrawNode{sys: n, dev: root + "/dev/" + filepath.Base(n), usb: usb, iface: int(iface), ifaceText: text})
	}
	return out
}

// hidUSB reads "vvvv:pppp" from a HID uevent's HID_ID (bus:vendor:product,
// eight hex digits each), "" when it has none.
func hidUSB(uevent string) string {
	for _, line := range strings.Split(uevent, "\n") {
		id, ok := strings.CutPrefix(line, "HID_ID=")
		if !ok {
			continue
		}
		parts := strings.Split(id, ":")
		if len(parts) != 3 || len(parts[1]) < 4 || len(parts[2]) < 4 {
			return ""
		}
		return strings.ToLower(parts[1][len(parts[1])-4:] + ":" + parts[2][len(parts[2])-4:])
	}
	return ""
}

// classifyHIDRaw finds the grant naming exactly this node's interface, and
// reports whether the node's device appears in grants at all.
func classifyHIDRaw(n hidrawNode, grants []HIDRaw) (match *HIDRaw, known bool) {
	for i := range grants {
		if grants[i].USB != n.usb {
			continue
		}
		known = true
		if grants[i].Interface == n.iface {
			return &grants[i], true
		}
	}
	return nil, known
}

// hidraw grants this machine's interfaces directly, so the boot pass works
// without the rules file, and resets stale ones.
func (a *applier) hidraw(mine, known []HIDRaw) {
	for _, n := range hidrawNodes() {
		g, _ := classifyHIDRaw(n, mine)
		if g != nil {
			// A udev rule cannot read a report descriptor, so a device file
			// naming a keyboard's interface by mistake would grant its
			// keystrokes. The boot pass can read it, and refuses.
			if desc, err := os.ReadFile(n.sys + "/device/report_descriptor"); err == nil && hidrawbtn.IsKeyboardDescriptor(desc) {
				a.printf("  warning: the device file grants %s (interface %s of %s), which is a keyboard; refusing it (please report this as a device-file bug)\n",
					display(n.dev), n.ifaceText, n.usb)
				a.revert(n.dev, func(os.FileMode) os.FileMode { return 0o600 }, "keyboard")
				continue
			}
			if hidden(n.dev) {
				a.printf("  leaving %s alone: another tool has hidden it (mode 0000)\n", display(n.dev))
				a.res.Skipped++
				continue
			}
			bits := os.FileMode(0o040)
			if g.Write {
				bits = 0o060
			}
			a.set(n.dev, func(m os.FileMode) os.FileMode { return m&^0o070 | bits }, "hidraw")
			continue
		}
		if _, isKnown := classifyHIDRaw(n, known); isKnown {
			a.revert(n.dev, func(os.FileMode) os.FileMode { return 0o600 }, "stale hidraw")
		}
	}
}

// files expands a Sysfs grant against the live tree.
func (s Sysfs) files() []string {
	dirs, _ := filepath.Glob(root + s.Dir)
	var out []string
	for _, d := range dirs {
		if s.Where != nil {
			v, err := os.ReadFile(d + "/" + s.Where.File)
			if err != nil || strings.TrimSpace(string(v)) != s.Where.Value {
				continue
			}
		}
		for _, f := range s.Files {
			m, _ := filepath.Glob(d + "/" + f)
			out = append(out, m...)
		}
	}
	return out
}

// hidden reports whether a device node has no permission bits at all. No
// kernel creates a node that way; a tool that took the device over hid it —
// InputPlumber sets the nodes it manages to 0000 so only its own exclusive
// reader sees them. Loosening one would undo that for the whole group until
// the next udev event, and the rule cannot override it either (InputPlumber's
// assignment is final).
func hidden(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().Perm() == 0
}

func gidOf(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Gid)
	}
	return -1
}

// display strips the test root from a path.
func display(path string) string { return strings.TrimPrefix(path, root) }
