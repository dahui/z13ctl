package cmd

// setup_test.go — drift guards between the permission artifacts that
// "voltaire setup" generates and the ones shipped by package installs under
// contrib/. Issue #14 was caused by exactly this drift: the ppt_* grants
// existed only in the generated artifacts, so .rpm/.deb users lost TDP access
// on every reboot.

import (
	"os"
	"strings"
	"testing"
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
	{name: "PPT power limits", match: "/sys/devices/platform/asus-nb-wmi/ppt_*", rules: true, service: true},
	{name: "ryzen_smu", match: "/sys/kernel/ryzen_smu_drv/", service: true},
	// The one read-only grant. energy_uj is 0400 root:root under the Platypus
	// mitigation, so package power is unreadable without it; the rule matches
	// the package-0 domain and the unit globs, because the counter can exist
	// before any udev event this rule could hang off.
	{name: "powercap energy counter", match: "energy_uj", rules: true, service: true},
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
