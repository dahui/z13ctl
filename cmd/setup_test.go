package cmd

// setup_test.go — drift guards between the permission artifacts that
// "z13ctl setup" generates and the ones shipped by package installs under
// contrib/. Issue #14 was caused by exactly this drift: the ppt_* grants
// existed only in the generated artifacts, so .rpm/.deb users lost TDP access
// on every reboot.

import (
	"os"
	"strings"
	"testing"
)

const (
	packagedRulesPath   = "../contrib/udev/99-z13ctl.rules"
	packagedServicePath = "../contrib/systemd/system/z13ctl-perms.service"
)

// sysfsGrants are the targets z13ctl must be able to write as a non-root user.
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
	{name: "PPT power limits", match: "/sys/devices/platform/asus-nb-wmi/ppt_*", rules: true, service: true},
	{name: "ryzen_smu", match: "/sys/kernel/ryzen_smu_drv/", service: true},
}

func readPackaged(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
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
	if !strings.Contains(postinstall, "systemctl restart z13ctl-perms.service") {
		t.Error("contrib/nfpm/postinstall.sh must restart z13ctl-perms.service; " +
			"'enable --now' does not re-run ExecStart for an already-active oneshot, " +
			"so upgrades would not apply new permission grants until a reboot")
	}
}
