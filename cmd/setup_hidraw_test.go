package cmd

// setup_hidraw_test.go — the lighting grant reaches interface 0 only, and
// setup takes back the pre-1.4.4 grant on the other interfaces. Until 1.4.4
// the rule matched vendor:product alone, so the cover keyboard's typing
// interface was readable by the whole group.

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestHidrawGrantsNameAnInterface: every hidraw rule, generated and packaged,
// names interface 0 in the ENV form. ATTRS{bInterfaceNumber} is refused: it
// sits on a different parent from idVendor/idProduct, so a rule using it
// matches nothing.
func TestHidrawGrantsNameAnInterface(t *testing.T) {
	t.Parallel()
	for name, rules := range map[string]string{
		"buildRulesContent": buildRulesContent("users"),
		packagedRulesPath:   readPackaged(t, packagedRulesPath),
	} {
		n := 0
		for _, line := range strings.Split(rules, "\n") {
			if !strings.Contains(line, `SUBSYSTEM=="hidraw"`) || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			n++
			if !strings.Contains(line, `ENV{ID_USB_INTERFACE_NUM}=="00"`) {
				t.Errorf("%s: hidraw rule grants every interface of its device:\n%s", name, line)
			}
			if strings.Contains(line, "bInterfaceNumber") {
				t.Errorf("%s: ATTRS{bInterfaceNumber} never matches beside idVendor/idProduct:\n%s", name, line)
			}
		}
		if n != 2 {
			t.Errorf("%s: %d hidraw rules, want 2 (keyboard and lightbar)", name, n)
		}
	}
}

// hidrawRig builds a fake /sys/class/hidraw and /dev and records resets.
type hidrawRig struct {
	sys, dev string
	reset    []string
}

func newHidrawRig(t *testing.T) *hidrawRig {
	t.Helper()
	root := t.TempDir()
	r := &hidrawRig{sys: root + "/sys/class/hidraw", dev: root + "/dev"}
	savedSys, savedDev, savedReset := sysHidrawDir, devDir, resetNode
	t.Cleanup(func() { sysHidrawDir, devDir, resetNode = savedSys, savedDev, savedReset })
	sysHidrawDir, devDir = r.sys, r.dev
	resetNode = func(p string) error { r.reset = append(r.reset, filepath.Base(p)); return nil }
	if err := os.MkdirAll(r.dev, 0o755); err != nil {
		t.Fatal(err)
	}
	return r
}

// node adds hidrawN for HID_ID product on USB interface iface. Its /dev node
// is owned by the test's own group, standing for the setup group.
func (r *hidrawRig) node(t *testing.T, name, product, iface string) {
	t.Helper()
	usbIface := r.sys + "/../usb/" + name // the USB interface; device/.. resolves here
	hid := usbIface + "/hid"
	if err := os.MkdirAll(hid, 0o755); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(usbIface+"/bInterfaceNumber", []byte(iface+"\n"), 0o644))
	must(os.WriteFile(hid+"/uevent", []byte("DRIVER=asus\nHID_ID=0003:00000B05:0000"+product+"\n"), 0o644))
	must(os.MkdirAll(r.sys+"/"+name, 0o755))
	must(os.Symlink(hid, r.sys+"/"+name+"/device"))
	must(os.WriteFile(r.dev+"/"+name, nil, 0o660))
}

func testGroup(t *testing.T) string {
	t.Helper()
	g, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		t.Skipf("cannot name this process's group: %v", err)
	}
	return g.Name
}

// TestRevokeStaleHidrawGrants: the cover's typing interface and touchpad and
// the lightbar's other interface go back to root; interface 0 of both, and
// other devices, are left alone; a node another group owns is not setup's.
func TestRevokeStaleHidrawGrants(t *testing.T) {
	r := newHidrawRig(t)
	group := testGroup(t)
	r.node(t, "hidraw0", "1A30", "00") // cover lighting: keeps its grant
	r.node(t, "hidraw1", "1A30", "02") // cover typing interface
	r.node(t, "hidraw2", "1A30", "03") // cover touchpad
	r.node(t, "hidraw3", "18C6", "00") // lightbar lighting
	r.node(t, "hidraw4", "18C6", "01") // lightbar's other interface
	r.node(t, "hidraw5", "1B4C", "02") // another ASUS device: not ours

	revokeStaleHidrawGrants(group, true)
	if len(r.reset) != 0 {
		t.Fatalf("a dry run reset %v", r.reset)
	}
	revokeStaleHidrawGrants(group, false)
	if got := strings.Join(r.reset, " "); got != "hidraw1 hidraw2 hidraw4" {
		t.Errorf("reset %q, want the non-lighting interfaces of the cover and lightbar", got)
	}

	r.reset = nil
	revokeStaleHidrawGrants("root", false)
	if len(r.reset) != 0 {
		t.Errorf("nodes not carrying the setup group were reset: %v", r.reset)
	}
}

func TestHidUSB(t *testing.T) {
	t.Parallel()
	if got := hidUSB("HID_ID=0003:00000B05:00001A30\n"); got != "0b05:1a30" {
		t.Errorf("hidUSB = %q", got)
	}
	if got := hidUSB("DRIVER=x\n"); got != "" {
		t.Errorf("hidUSB without HID_ID = %q", got)
	}
}

// TestPostinstallTakesBackTheOldGrant: package upgrades run setup
// --perms-only, which carries the revocation; without it a package user keeps
// the old grant until a reboot.
func TestPostinstallTakesBackTheOldGrant(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readPackaged(t, "../contrib/nfpm/postinstall.sh"), "/usr/bin/z13ctl setup --perms-only") {
		t.Error("postinstall does not run setup --perms-only")
	}
}
