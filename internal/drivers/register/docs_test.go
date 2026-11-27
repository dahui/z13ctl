package register

// The developer guide's device-file page is checked here because this is the
// one package that can both parse a device file and assemble it with the real
// drivers. Two guards: the page's complete example must be a device file
// voltaire accepts, and every key the schema knows must be documented on it.
// A key added to internal/device's Config without a line on the page fails
// the second.

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/perms"
)

const deviceFilesDoc = "../../../website/docs/dev/device-files.md"

func readDeviceFilesDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(deviceFilesDoc)
	if err != nil {
		t.Fatalf("reading the device-file guide: %v", err)
	}
	return string(b)
}

// TestDeviceFilesExampleAssembles parses the page's example with the strict
// parser and assembles it with every registered driver.
func TestDeviceFilesExampleAssembles(t *testing.T) {
	doc := readDeviceFilesDoc(t)
	const start, end = "<!-- example-device:start -->", "<!-- example-device:end -->"
	i, j := strings.Index(doc, start), strings.Index(doc, end)
	if i < 0 || j < i {
		t.Fatalf("the guide has no %s … %s block", start, end)
	}
	block := doc[i+len(start) : j]
	fenceOpen, fenceClose := strings.Index(block, "```toml\n"), strings.LastIndex(block, "```")
	if fenceOpen < 0 || fenceClose <= fenceOpen {
		t.Fatal("the example block holds no ```toml fence")
	}
	raw := block[fenceOpen+len("```toml\n") : fenceClose]

	c, err := device.ParseConfig([]byte(raw))
	if err != nil {
		t.Fatalf("the guide's example is not a valid device file: %v", err)
	}
	d, err := device.Assemble(c)
	if err != nil {
		t.Fatalf("the guide's example does not assemble: %v", err)
	}
	// The example claims to use every block; hold it to that.
	if d.Fans == nil || d.Power == nil || d.Profiles == nil || d.Lighting == nil || d.Toggles == nil ||
		d.Battery == nil || d.CPUBoost == nil || d.Undervolt == nil || d.Telemetry == nil || d.Buttons == nil {
		t.Errorf("the example left a capability unassembled: %+v", d)
	}
	// And voltaire setup knows what every section it uses needs granted.
	if _, err := perms.For(c); err != nil {
		t.Errorf("the guide's example has a section setup cannot grant: %v", err)
	}
}

// TestEveryDeviceFileKeyIsDocumented walks the schema's toml tags and wants
// each one written as `key` (or a block as `[key]`) on the page.
func TestEveryDeviceFileKeyIsDocumented(t *testing.T) {
	doc := readDeviceFilesDoc(t)
	seen := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if tag := strings.Split(f.Tag.Get("toml"), ",")[0]; tag != "" && tag != "-" {
				seen[tag] = true
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeOf(device.Config{}))
	if len(seen) < 40 {
		t.Fatalf("found only %d keys; the walk is not reaching the schema", len(seen))
	}
	for key := range seen {
		// A block is documented under its `[name]` heading (`[[name]]` for an
		// array of blocks), a key as `name`.
		if !strings.Contains(doc, "`"+key+"`") && !strings.Contains(doc, "`["+key+"]`") &&
			!strings.Contains(doc, "`[["+key+"]]`") {
			t.Errorf("device-file key %q is not documented in %s", key, deviceFilesDoc)
		}
	}
}

// TestEveryDeviceHasACompatibilityPage holds the user guide's Devices section
// to the device files: each embedded device file needs its own page at
// devices/<id>.md, and the supported-devices page must link to it (its
// compatibility matrix has a column per device). A new device file without
// them fails here, so no machine ships with nothing saying what works on it.
func TestEveryDeviceHasACompatibilityPage(t *testing.T) {
	const devicesDir = "../../../website/docs/devices/"
	index, err := os.ReadFile(devicesDir + "index.md")
	if err != nil {
		t.Fatalf("reading the supported-devices page: %v", err)
	}
	configs, err := device.Configs()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range configs {
		id := c.Device.ID
		if _, err := os.Stat(devicesDir + id + ".md"); err != nil {
			t.Errorf("device %q has no page at website/docs/devices/%s.md", id, id)
		}
		if !strings.Contains(string(index), "](/devices/"+id+")") {
			t.Errorf("website/docs/devices/index.md does not link to device %q", id)
		}
	}
}
