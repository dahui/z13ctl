package hid_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/dahui/z13ctl/internal/hid"
)

// pipeDevice returns a write-end-backed Device and the read end of the pipe.
func pipeDevice(t *testing.T) (*hid.Device, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDevice(w)
	t.Cleanup(func() {
		dev.Close()
		_ = r.Close()
	})
	return dev, r
}

func TestWrite_PadsTo64Bytes(t *testing.T) {
	t.Parallel()
	dev, r := pipeDevice(t)

	data := []byte{0x5d, 0xB9} // 2-byte init packet
	if err := dev.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	dev.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != hid.ReportSize {
		t.Errorf("Write wrote %d bytes, want %d", len(got), hid.ReportSize)
	}
	if got[0] != 0x5d || got[1] != 0xB9 {
		t.Errorf("Write first two bytes = %02x %02x, want 5d b9", got[0], got[1])
	}
	// Remaining bytes must be zero-padded.
	if !bytes.Equal(got[2:], make([]byte, hid.ReportSize-2)) {
		t.Error("Write: trailing bytes not zero-padded")
	}
}

func TestWrite_FullBuffer(t *testing.T) {
	t.Parallel()
	dev, r := pipeDevice(t)

	data := make([]byte, hid.ReportSize)
	for i := range data {
		data[i] = byte(i)
	}
	if err := dev.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	dev.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Error("Write: full-buffer data mismatch")
	}
}

func TestPaths(t *testing.T) {
	t.Parallel()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDevice(w)
	defer dev.Close()

	paths := dev.Paths()
	if len(paths) != 1 || paths[0] != "test" {
		t.Errorf("Paths() = %v, want [\"test\"]", paths)
	}
}

func TestDescriptions_Named(t *testing.T) {
	t.Parallel()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDevice(w)
	defer dev.Close()

	descs := dev.Descriptions()
	if len(descs) != 1 || descs[0] != "test (test)" {
		t.Errorf("Descriptions() = %v, want [\"test (test)\"]", descs)
	}
}

func TestDescriptions_Anon(t *testing.T) {
	t.Parallel()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDeviceAnon(w)
	defer dev.Close()

	descs := dev.Descriptions()
	if len(descs) != 1 || descs[0] != "/dev/hidraw0" {
		t.Errorf("Descriptions() = %v, want [\"/dev/hidraw0\"]", descs)
	}
}

func TestClose_Safe(t *testing.T) {
	t.Parallel()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDevice(w)
	// Double-close should not panic.
	dev.Close()
	dev.Close()
}

func TestFilteredView_EmptyReturnsAll(t *testing.T) {
	t.Parallel()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDevice(w)
	defer dev.Close()

	view, err := dev.FilteredView("")
	if err != nil {
		t.Fatalf("FilteredView(\"\") error: %v", err)
	}
	if view != dev {
		t.Error("FilteredView(\"\") should return the device itself")
	}
}

func TestFilteredView_MatchByName(t *testing.T) {
	t.Parallel()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDevice(w) // name = "test", path = "test"
	defer dev.Close()

	view, err := dev.FilteredView("test")
	if err != nil {
		t.Fatalf("FilteredView(\"test\") error: %v", err)
	}
	paths := view.Paths()
	if len(paths) != 1 || paths[0] != "test" {
		t.Errorf("FilteredView(\"test\").Paths() = %v, want [\"test\"]", paths)
	}
}

func TestFilteredView_NoMatch(t *testing.T) {
	t.Parallel()
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dev := hid.NewTestDevice(w)
	defer dev.Close()

	_, err = dev.FilteredView("lightbar")
	if err == nil {
		t.Error("FilteredView(\"lightbar\") should return error when no match")
	}
}
