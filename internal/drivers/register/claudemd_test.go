package register

// claudemd_test.go — the backticked Go names in the CLAUDE.md files must
// still exist. The root file once carried a table translating old names to
// new ones because a rename did not update it, and stale names read as
// guidance. Decision records (docs/decisions) are history and are not checked;
// the CLAUDE.md index lines are their titles, so they are skipped too.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// claudePackages maps the package names CLAUDE.md qualifies identifiers with
// to their directories, from the repository root.
var claudePackages = map[string]string{
	"api": "api", "safety": "internal/safety", "device": "internal/device", "driver": "internal/driver",
	"asus": "internal/drivers/asus", "amd": "internal/drivers/amd", "linux": "internal/drivers/linux",
	"perms": "internal/perms", "limits": "internal/limits", "aura": "internal/aura", "hid": "internal/hid",
	"cli": "internal/cli", "profileui": "internal/profileui", "controls": "internal/controls",
	"display": "internal/display", "theme": "internal/theme", "telemetryplot": "internal/telemetryplot",
	"buttonpref": "internal/buttonpref", "settingsui": "internal/settingsui", "mainwin": "internal/mainwin",
	"lighting": "internal/lighting", "wakeup": "internal/wakeup", "dismiss": "internal/dismiss",
	"coalesce": "internal/coalesce", "session": "internal/session", "evdevkey": "internal/drivers/evdevkey",
	"hidrawbtn": "internal/drivers/hidrawbtn", "hidoxp": "internal/drivers/hidoxp",
	"startup": "internal/startup", "uiscale": "internal/uiscale", "togglegate": "internal/togglegate",
	"focusgrid": "internal/focusgrid", "telemetryring": "internal/telemetryring",
	"apiresult": "internal/apiresult", "popupgeom": "internal/popupgeom", "panelgeom": "internal/panelgeom",
	"colorconv": "internal/colorconv", "keyrepeat": "internal/keyrepeat",
}

var qualified = regexp.MustCompile("`([a-z]+)\\.([A-Za-z_][A-Za-z0-9_]*(?:\\.[A-Za-z_][A-Za-z0-9_]*)*)(\\*?)")

func TestCLAUDEmdNamesExist(t *testing.T) {
	src := map[string]string{} // package dir -> its Go source, concatenated
	for _, file := range []string{"../../../CLAUDE.md", "../../gui/CLAUDE.md"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "docs/decisions/") {
				continue // an index line: a record's title, which is history
			}
			for _, m := range qualified.FindAllStringSubmatch(line, -1) {
				dir, ok := claudePackages[m[1]]
				if !ok {
					continue
				}
				if _, ok := src[dir]; !ok {
					src[dir] = goSource(t, "../../../"+dir)
				}
				parts := strings.Split(m[2], ".")
				name := parts[len(parts)-1]
				if m[3] == "*" || name == "go" { // a wildcard (api.Send*), or a file name
					continue
				}
				decl := regexp.MustCompile(`(func (\([^)]*\) )?` + name + `\b|type ` + name + `\b|\b` + name + `\s+(=|[A-Za-z*\[\]])|\b` + name + `\s*,)`)
				if !decl.MatchString(src[dir]) {
					t.Errorf("%s:%d names `%s.%s`, which %s no longer declares", filepath.Base(filepath.Dir(file))+"/CLAUDE.md", n+1, m[1], m[2], dir)
				}
			}
		}
	}
}

func goSource(t *testing.T, dir string) string {
	t.Helper()
	files, err := filepath.Glob(dir + "/*.go")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
	}
	return b.String()
}
