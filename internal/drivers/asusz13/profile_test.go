package asusz13

// profile_test.go — the firmware profile list comes from the kernel (the owning
// handler's choices), filtered by device data; reads are normalized to that
// list; the owning handler is chosen by name. All against the fake sysfs.

import (
	"slices"
	"testing"
)

// profileDevice adds a named platform-profile class device.
func (f *fakeSysfs) profileDevice(t *testing.T, dir, name, choices, current string) string {
	t.Helper()
	base := f.withProfileDevice(t, dir, choices, current)
	f.writeFile(t, base+"/name", name)
	return base
}

// The Z13's two handlers: amd-pmf registers first and has no "quiet". By name,
// the owning handler is asus-wmi whatever the registration order; asked for
// amd-pmf, it is amd-pmf even though asus-wmi is the one offering "quiet".
func TestFindProfilePathForChoosesTheHandlerByName(t *testing.T) {
	f := newFakeSysfs(t)
	amd := f.profileDevice(t, "platform-profile-1", "amd-pmf", "low-power balanced performance", "balanced")
	asus := f.profileDevice(t, "platform-profile-2", "asus-wmi", "quiet balanced performance", "balanced")

	if got := FindProfilePathFor("asus-wmi"); got != asus+"/profile" {
		t.Errorf("handler asus-wmi = %q, want %q", got, asus+"/profile")
	}
	if got := FindProfilePathFor("amd-pmf"); got != amd+"/profile" {
		t.Errorf("handler amd-pmf = %q, want %q", got, amd+"/profile")
	}
	// An unknown handler falls back to the heuristic rather than failing.
	if got := FindProfilePathFor("nope"); got != asus+"/profile" {
		t.Errorf("unknown handler = %q, want the heuristic's %q", got, asus+"/profile")
	}
}

func TestProfileNamesAreTheKernelsFilteredByDeviceData(t *testing.T) {
	f := newFakeSysfs(t)
	f.profileDevice(t, "platform-profile-1", "asus-wmi", "quiet balanced performance custom", "balanced")

	// max-power is declared but not offered: dropped. The kernel's "custom"
	// is offered but never a profile. Declared order wins.
	p := NewProfileController([]string{"performance", "balanced", "quiet", "max-power"}, "asus-wmi", "balanced", nil)
	if got, want := p.Names(), []string{"performance", "balanced", "quiet"}; !slices.Equal(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

// The case with a safety edge: device data naming "quiet" against a handler
// that spells it "low-power". Offering "low-power" would key a stock row that
// does not exist (the row is "quiet"), so the stock write before a fan release
// would silently be skipped. It is dropped, with a warning, instead.
func TestProfileNamesDoNotTranslateDeclaredNames(t *testing.T) {
	f := newFakeSysfs(t)
	f.profileDevice(t, "platform-profile-1", "amd-pmf", "low-power balanced performance", "balanced")
	p := NewProfileController([]string{"quiet", "balanced", "performance"}, "amd-pmf", "", nil)
	if got, want := p.Names(), []string{"balanced", "performance"}; !slices.Equal(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

func TestProfileNamesFallBacks(t *testing.T) {
	declared := []string{"quiet", "balanced", "performance"}

	// No sysfs at all: the device data stands alone.
	newFakeSysfs(t)
	swap(t, &sysProfileDir, t.TempDir()+"/absent")
	swap(t, &sysProfileACPI, t.TempDir()+"/absent")
	if got := NewProfileController(declared, "asus-wmi", "", nil).Names(); !slices.Equal(got, declared) {
		t.Errorf("no sysfs: Names = %v, want the declared %v", got, declared)
	}

	// No overlap at all: the data is wrong for this kernel, so offer what the
	// kernel offers rather than names the handler would refuse.
	f := newFakeSysfs(t)
	f.profileDevice(t, "platform-profile-1", "other", "low-power cool performance", "cool")
	if got, want := NewProfileController([]string{"quiet", "balanced"}, "other", "", nil).Names(),
		[]string{"low-power", "cool", "performance"}; !slices.Equal(got, want) {
		t.Errorf("no overlap: Names = %v, want the kernel's %v", got, want)
	}
}

func TestProfileGetIsNormalized(t *testing.T) {
	names := []string{"quiet", "balanced", "performance"}
	for _, tt := range []struct {
		raw, want string
		ok        bool
	}{
		{"balanced", "balanced", true},
		{"quiet", "quiet", true},
		{"low-power", "quiet", true}, // a secondary handler's spelling
		{"cool", "quiet", true},
		{"custom", "", false},    // the kernel's "handlers disagree"
		{"max-power", "", false}, // not a profile this device offers
	} {
		got, err := normalizeProfile(tt.raw, names)
		if got != tt.want || (err == nil) != tt.ok {
			t.Errorf("normalizeProfile(%q) = %q, %v; want %q, ok=%v", tt.raw, got, err, tt.want, tt.ok)
		}
	}

	f := newFakeSysfs(t)
	f.profileDevice(t, "platform-profile-1", "asus-wmi", "quiet balanced performance", "custom")
	if _, err := NewProfileController(names, "asus-wmi", "", nil).Get(); err == nil {
		t.Error("Get() on a \"custom\" read = nil error, want unknown")
	}
}

func TestProfileDefault(t *testing.T) {
	f := newFakeSysfs(t)
	f.profileDevice(t, "platform-profile-1", "asus-wmi", "quiet balanced performance", "balanced")
	names := []string{"quiet", "balanced", "performance"}

	if got := NewProfileController(names, "asus-wmi", "quiet", nil).Default(); got != "quiet" {
		t.Errorf("declared default = %q, want quiet", got)
	}
	if got := NewProfileController(names, "asus-wmi", "", nil).Default(); got != "balanced" {
		t.Errorf("undeclared default = %q, want balanced when offered", got)
	}
	// Declared but not offered: no safe landing profile, so "" (a reset refuses).
	if got := NewProfileController(names, "asus-wmi", "max-power", nil).Default(); got != "" {
		t.Errorf("unoffered default = %q, want \"\"", got)
	}
}

func TestProfileLabel(t *testing.T) {
	p := NewProfileController(nil, "", "", map[string]string{"quiet": "Silent"})
	if got := p.Label("quiet"); got != "Silent" {
		t.Errorf("declared label = %q", got)
	}
	if got := p.Label("balanced-performance"); got != "Balanced Performance" {
		t.Errorf("fallback label = %q", got)
	}
}

// A secondary handler with only "cool" still gets the low-power group's intent.
func TestProfileNameForDeviceUsesTheEquivalenceGroup(t *testing.T) {
	f := newFakeSysfs(t)
	cool := f.profileDevice(t, "platform-profile-1", "x", "cool balanced performance", "balanced")
	if got := profileNameForDevice(cool, "quiet"); got != "cool" {
		t.Errorf("quiet on a cool-only handler = %q, want cool", got)
	}
	// Outside the group nothing is translated: performance never becomes
	// max-power, which would raise a limit nobody chose.
	maxp := f.profileDevice(t, "platform-profile-2", "y", "low-power balanced max-power", "balanced")
	if got := profileNameForDevice(maxp, "performance"); got != "performance" {
		t.Errorf("performance = %q, want it untranslated", got)
	}
}

func TestPPDProfile(t *testing.T) {
	for name, want := range map[string]string{
		"low-power": "power-saver", "cool": "power-saver", "quiet": "power-saver",
		"balanced":             "balanced",
		"balanced-performance": "performance", "performance": "performance", "max-power": "performance",
		"custom": "", "gaming": "",
	} {
		if got := PPDProfile(name); got != want {
			t.Errorf("PPDProfile(%q) = %q, want %q", name, got, want)
		}
	}
}
