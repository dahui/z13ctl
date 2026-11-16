// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package sysfsio

import (
	"os"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadIntTrimsAndParses(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/v", " 42\n")
	if got, err := ReadInt(dir + "/v"); err != nil || got != 42 {
		t.Fatalf("ReadInt = %d, %v; want 42", got, err)
	}
	write(t, dir+"/bad", "x\n")
	if _, err := ReadInt(dir + "/bad"); err == nil {
		t.Error("ReadInt accepted a non-number")
	}
	if _, err := ReadInt(dir + "/missing"); err == nil {
		t.Error("ReadInt read a missing file")
	}
}

func TestWriteIntRoundTrips(t *testing.T) {
	p := t.TempDir() + "/v"
	if err := WriteInt(p, -7); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != "-7\n" {
		t.Errorf("WriteInt wrote %q, want %q", data, "-7\n")
	}
}

func TestReadUintHoldsLargeCounters(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/e", "262143999999\n")
	if got, err := ReadUint(dir + "/e"); err != nil || got != 262143999999 {
		t.Fatalf("ReadUint = %d, %v", got, err)
	}
	write(t, dir+"/neg", "-1\n")
	if _, err := ReadUint(dir + "/neg"); err == nil {
		t.Error("ReadUint accepted a negative value")
	}
}

func TestReadTrimmedAndExists(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/s", "  balanced \n")
	if got := ReadTrimmed(dir + "/s"); got != "balanced" {
		t.Errorf("ReadTrimmed = %q", got)
	}
	if got := ReadTrimmed(dir + "/missing"); got != "" {
		t.Errorf("ReadTrimmed of a missing file = %q, want empty", got)
	}
	if !Exists(dir+"/s") || Exists(dir+"/missing") {
		t.Error("Exists answered wrongly")
	}
}

func TestFindHwmonByName(t *testing.T) {
	root := t.TempDir()
	for name, label := range map[string]string{"hwmon0": "k10temp", "hwmon3": "asus_custom_fan_curve"} {
		if err := os.Mkdir(root+"/"+name, 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, root+"/"+name+"/name", label+"\n")
	}
	if got := FindHwmon(root, "asus_custom_fan_curve"); got != root+"/hwmon3" {
		t.Errorf("FindHwmon = %q, want %q", got, root+"/hwmon3")
	}
	if got := FindHwmon(root, "nope"); got != "" {
		t.Errorf("FindHwmon found a device that is not there: %q", got)
	}
	if got := FindHwmon(root+"/missing", "k10temp"); got != "" {
		t.Errorf("FindHwmon under a missing root = %q", got)
	}
}
