// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package sysfsio holds the few sysfs read and write helpers every driver
// package needs. Each takes its path as an argument, so a driver keeps its
// own sysfs roots as package variables — which its tests redirect into a fake
// tree — and this package never needs to know where anything lives.
//
// It is internal to internal/drivers: a helper here is a convenience for
// drivers, not an interface for anything above them.
package sysfsio

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadInt reads a file holding one decimal integer, surrounding whitespace
// allowed.
func ReadInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// WriteInt writes one decimal integer and a newline, as sysfs attributes
// expect.
func WriteInt(path string, value int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(value)+"\n"), 0o644)
}

// ReadUint reads a file holding one unsigned decimal integer — a counter such
// as RAPL's energy_uj, which can exceed what an int holds on a 32-bit build.
func ReadUint(path string) (uint64, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return v, nil
}

// ReadTrimmed returns a file's contents without surrounding whitespace, or ""
// when it cannot be read. For attributes where absent and empty mean the same
// thing to the caller.
func ReadTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// FindHwmon returns the directory under root (normally /sys/class/hwmon)
// whose name attribute is name, or "" when none is. hwmon numbers change
// across boots, so a device is always found by its name.
func FindHwmon(root, name string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		dir := root + "/" + e.Name()
		if ReadTrimmed(dir+"/name") == name {
			return dir
		}
	}
	return ""
}
