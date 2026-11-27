package inputplumber

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// rings is the Xbox Ally X's lighting interface.
var rings = []HIDRawSource{{Vendor: 0x0b05, Product: 0x1b4c, Interface: 2}}

var allyDMI = DMI{"board_name": "RC73XA", "sys_vendor": "ASUSTeK COMPUTER INC."}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// passthroughOf is each source's passthrough value, by interface number.
func passthroughOf(t *testing.T, config []byte) map[int]bool {
	t.Helper()
	var c struct {
		Sources []struct {
			HIDRaw *struct {
				Interface int `yaml:"interface_num"`
			} `yaml:"hidraw"`
			Passthrough bool `yaml:"passthrough"`
		} `yaml:"source_devices"`
	}
	if err := yaml.Unmarshal(config, &c); err != nil {
		t.Fatal(err)
	}
	out := map[int]bool{}
	for _, s := range c.Sources {
		if s.HIDRaw != nil {
			out[s.HIDRaw.Interface] = s.Passthrough
		}
	}
	return out
}

// TestPassthroughOnTheRealAllyConfig: InputPlumber 0.85.0's Ally config
// gains passthrough on interface 2 alone, as one inserted line.
func TestPassthroughOnTheRealAllyConfig(t *testing.T) {
	orig := readTestdata(t, "50-rog_xbox_ally.yaml")
	patched, matched, err := Passthrough(orig, rings)
	if err != nil || !matched {
		t.Fatalf("matched %v, err %v", matched, err)
	}
	if got := passthroughOf(t, patched); !got[2] || got[5] {
		t.Errorf("passthrough by interface = %v, want interface 2 only", got)
	}
	// One line inserted: the source's first key moves down a line at the
	// same column, and nothing else in the file changes.
	first := "  - group: gamepad # Used for setting attributes on load\n"
	want := strings.Replace(string(orig), first,
		"  - passthrough: true # voltaire: leave the lighting interface visible\n"+
			"    group: gamepad # Used for setting attributes on load\n", 1)
	if string(patched) != want {
		t.Errorf("patched config is not the original plus one line:\n%s", patched)
	}

	again, _, err := Passthrough(patched, rings)
	if err != nil || !bytes.Equal(again, patched) {
		t.Errorf("a second pass changed the file (err %v)", err)
	}
}

func TestPassthroughLeavesOtherConfigsAlone(t *testing.T) {
	orig := readTestdata(t, "50-rog_xbox_ally.yaml")
	other := []HIDRawSource{{Vendor: 0x0b05, Product: 0x1b4c, Interface: 3}}
	if out, matched, err := Passthrough(orig, other); err != nil || matched || !bytes.Equal(out, orig) {
		t.Errorf("a config without the source: matched %v, err %v, changed %v", matched, err, !bytes.Equal(out, orig))
	}
}

func TestPassthroughFalseIsTurnedOn(t *testing.T) {
	in := []byte("version: 1\nsource_devices:\n  - group: gamepad\n    passthrough: false\n    hidraw:\n      vendor_id: 0x0b05\n      product_id: 0x1b4c\n      interface_num: 2\n")
	out, _, err := Passthrough(in, rings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "    passthrough: true\n") || strings.Contains(string(out), "false") {
		t.Errorf("got:\n%s", out)
	}
}

// TestPassthroughRefusesWhatItCannotEditCleanly: a flow-style source is not
// edited, and the caller gets an error rather than a file.
func TestPassthroughRefusesWhatItCannotEditCleanly(t *testing.T) {
	in := []byte("source_devices:\n  - {group: gamepad, hidraw: {vendor_id: 0x0b05, product_id: 0x1b4c, interface_num: 2}}\n")
	if out, matched, err := Passthrough(in, rings); err == nil || !matched || out != nil {
		t.Errorf("flow style: out %q, matched %v, err %v", out, matched, err)
	}
}

// TestCheckOnlyPassthroughCatchesOtherChanges is the negative control for the
// check every edit goes through.
func TestCheckOnlyPassthroughCatchesOtherChanges(t *testing.T) {
	orig := readTestdata(t, "50-rog_xbox_ally.yaml")
	patched, _, err := Passthrough(orig, rings)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(patched, []byte("interface_num: 5"), []byte("interface_num: 6"), 1)
	if checkOnlyPassthrough(orig, tampered, []int{0}) == nil {
		t.Error("a change besides passthrough passed the check")
	}
	if checkOnlyPassthrough(orig, patched, []int{0}) != nil {
		t.Error("the real edit failed the check")
	}
}

func TestConfigMatches(t *testing.T) {
	ally, x20 := readTestdata(t, "50-rog_xbox_ally.yaml"), readTestdata(t, "50-rog_xbox_ally_x20.yaml")
	if !ConfigMatches(ally, allyDMI) {
		t.Error("the Ally's config does not match the Ally")
	}
	if ConfigMatches(x20, allyDMI) {
		t.Error("the X20's config matches the Ally")
	}
	if !ConfigMatches([]byte("name: any\n"), allyDMI) {
		t.Error("a config with no matches list applies everywhere")
	}
	glob := []byte("matches:\n  - dmi_data:\n      board_name: RC73*\n")
	if !ConfigMatches(glob, allyDMI) {
		t.Error("a glob pattern did not match")
	}
	udev := []byte("matches:\n  - udev:\n      sys_path: /sys/x\n")
	if !ConfigMatches(udev, allyDMI) {
		t.Error("an entry that cannot be judged from DMI must count as a match")
	}
}

// overrideRig points the directories at a temporary tree holding both real
// Ally configs as InputPlumber's.
func overrideRig(t *testing.T) (upstream string) {
	t.Helper()
	root := t.TempDir()
	saved := []string{OverrideDir}
	t.Cleanup(func() { OverrideDir = saved[0] })
	upstream = root + "/usr/share/inputplumber/devices"
	OverrideDir = root + "/etc/inputplumber/devices.d"
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"50-rog_xbox_ally.yaml", "50-rog_xbox_ally_x20.yaml"} {
		if err := os.WriteFile(upstream+"/"+n, readTestdata(t, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return upstream
}

func mustPlan(t *testing.T, upstream string, want bool) OverridePlan {
	t.Helper()
	p, err := PlanOverrides(upstream, rings, allyDMI, want)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOverrideLifecycle: written for this machine's config only, unchanged on
// a rerun, remade when upstream changes, gone on opt-out.
func TestOverrideLifecycle(t *testing.T) {
	upstream := overrideRig(t)
	p := mustPlan(t, upstream, true)
	if len(p.Write) != 1 || p.Write[0].Name != "50-rog_xbox_ally.yaml" || !p.Wanted() {
		t.Fatalf("plan %+v, want the Ally's config alone (not the X20's)", p)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(OverrideDir + "/50-rog_xbox_ally.yaml")
	if err != nil || !IsOverride(written) || !passthroughOf(t, written)[2] {
		t.Fatalf("override not written as expected (err %v):\n%s", err, written)
	}
	if !strings.Contains(string(written), upstream+"/50-rog_xbox_ally.yaml") {
		t.Error("the override does not name the file it was made from")
	}

	if rerun := mustPlan(t, upstream, true); rerun.Changed() || len(rerun.Unchanged) != 1 {
		t.Errorf("rerun: %+v, want nothing to do", rerun)
	}

	// Upstream changes: the override follows.
	f := upstream + "/50-rog_xbox_ally.yaml"
	if err := os.WriteFile(f, bytes.Replace(readTestdata(t, "50-rog_xbox_ally.yaml"),
		[]byte("name: ASUS ROG Xbox Ally"), []byte("name: ASUS ROG Xbox Ally (new)"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	p = mustPlan(t, upstream, true)
	if len(p.Write) != 1 || !bytes.Contains(p.Write[0].Content, []byte("(new)")) {
		t.Fatalf("upstream change not followed: %+v", p)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}

	// Upstream takes passthrough itself: the override goes.
	patched, _, _ := Passthrough(readTestdata(t, "50-rog_xbox_ally.yaml"), rings)
	if err := os.WriteFile(f, patched, 0o644); err != nil {
		t.Fatal(err)
	}
	if up := mustPlan(t, upstream, true); len(up.Remove) != 1 || up.Wanted() {
		t.Errorf("upstream passthrough: %+v, want the override removed", up)
	}

	// Opt-out removes it.
	if err := os.WriteFile(f, readTestdata(t, "50-rog_xbox_ally.yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mustPlan(t, upstream, true).Apply(); err != nil {
		t.Fatal(err)
	}
	p = mustPlan(t, upstream, false)
	if len(p.Remove) != 1 || len(p.Write) != 0 {
		t.Fatalf("opt-out plan %+v", p)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(Overrides()) != 0 {
		t.Error("opt-out left an override")
	}
}

// TestForeignOverrideIsLeftAlone: a file of the same name without setup's
// marker is the administrator's; setup neither rewrites nor removes it.
func TestForeignOverrideIsLeftAlone(t *testing.T) {
	upstream := overrideRig(t)
	if err := os.MkdirAll(OverrideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := OverrideDir + "/50-rog_xbox_ally.yaml"
	if err := os.WriteFile(mine, []byte("# my own\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustPlan(t, upstream, true)
	if len(p.Foreign) != 1 || p.Changed() {
		t.Errorf("plan %+v, want the foreign file reported and nothing written", p)
	}
	if p := mustPlan(t, upstream, false); p.Changed() {
		t.Errorf("opt-out would touch the foreign file: %+v", p)
	}
	if data, _ := os.ReadFile(mine); string(data) != "# my own\n" {
		t.Error("the foreign file changed")
	}
}

func TestNoInputPlumberNoOverride(t *testing.T) {
	overrideRig(t)
	if p := mustPlan(t, "", true); p.Changed() || p.Wanted() {
		t.Errorf("no InputPlumber: %+v", p)
	}
	upstream := overrideRig(t)
	if p, _ := PlanOverrides(upstream, nil, allyDMI, true); p.Changed() || p.Wanted() {
		t.Errorf("no lighting interface: %+v", p)
	}
}
