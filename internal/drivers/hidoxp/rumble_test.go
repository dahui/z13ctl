package hidoxp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDriver builds a hid-oxp driver directory holding the X2's three HID
// devices, only the vendor interface (.0005) carrying the attribute, and
// returns that device's directory.
func fakeDriver(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orig := sysDriverDir
	sysDriverDir = root
	t.Cleanup(func() { sysDriverDir = orig })
	for _, n := range []string{"0003:1A86:FE00.0002", "0003:1A86:FE00.0004", "0003:1A86:FE00.0005", "0003:1234:5678.0001"} {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dev := filepath.Join(root, "0003:1A86:FE00.0005")
	write(t, filepath.Join(dev, RumbleAttr), "5\n")
	write(t, filepath.Join(dev, rumbleRangeAttr), "0-5\n")
	// A different controller with the attribute must never be the one used.
	write(t, filepath.Join(root, "0003:1234:5678.0001", RumbleAttr), "2\n")
	return dev
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRumbleReadsAndWritesTheMatchingController(t *testing.T) {
	dev := fakeDriver(t)
	r := NewRumble(0x1a86, 0xfe00)
	lo, hi, err := r.Range()
	if err != nil || lo != 0 || hi != 5 {
		t.Fatalf("Range = %d, %d, %v; want 0, 5", lo, hi, err)
	}
	if v, err := r.Get(); err != nil || v != 5 {
		t.Fatalf("Get = %d, %v; want 5", v, err)
	}
	if err := r.Set(1); err != nil {
		t.Fatalf("Set(1): %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dev, RumbleAttr))
	if strings.TrimSpace(string(b)) != "1" {
		t.Errorf("attribute = %q after Set(1)", b)
	}
}

func TestRumbleRefusesOutOfRangeBeforeWriting(t *testing.T) {
	dev := fakeDriver(t)
	r := NewRumble(0x1a86, 0xfe00)
	for _, v := range []int{-1, 6} {
		if err := r.Set(v); err == nil {
			t.Errorf("Set(%d) accepted", v)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dev, RumbleAttr))
	if strings.TrimSpace(string(b)) != "5" {
		t.Errorf("a refused write reached the attribute: %q", b)
	}
}

func TestRumbleWithoutTheController(t *testing.T) {
	fakeDriver(t)
	r := NewRumble(0x1a86, 0x0001)
	if _, _, err := r.Range(); err == nil {
		t.Error("Range found a controller with another product id")
	}
	sysDriverDir = "/nonexistent/hid-oxp"
	if _, err := NewRumble(0x1a86, 0xfe00).Get(); err == nil || !strings.Contains(err.Error(), "hid-oxp") {
		t.Errorf("Get with no driver: %v, want the hid-oxp hint", err)
	}
}

func TestParseRange(t *testing.T) {
	for _, tc := range []struct {
		in     string
		lo, hi int
		ok     bool
	}{
		{"0-5", 0, 5, true}, {" 1 - 3 ", 1, 3, true}, {"5-0", 0, 0, false}, {"5", 0, 0, false}, {"a-b", 0, 0, false},
	} {
		lo, hi, err := parseRange(tc.in)
		if (err == nil) != tc.ok || lo != tc.lo || hi != tc.hi {
			t.Errorf("parseRange(%q) = %d, %d, %v", tc.in, lo, hi, err)
		}
	}
}
