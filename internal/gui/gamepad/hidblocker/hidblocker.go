// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package hidblocker

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

// Blocker manages a BPF LSM program that blocks hidraw reads for specific PIDs.
// The BPF program attaches to the kernel's file_permission hook and returns
// -EAGAIN for read() calls from blocked PIDs on hidraw character devices.
//
// Thread-safe: Block/Unblock/UnblockAll can be called from any goroutine.
type Blocker struct {
	objs blockerObjects
	link link.Link
}

// ErrLSMNotAvailable is returned when BPF LSM is not enabled in the kernel.
var ErrLSMNotAvailable = errors.New("BPF LSM not available: bpf not in /sys/kernel/security/lsm")

// New loads the BPF program and attaches it to the file_permission LSM hook.
func New() (*Blocker, error) {
	if !lsmEnabled() {
		return nil, ErrLSMNotAvailable
	}

	major, err := hidrawMajor()
	if err != nil {
		return nil, fmt.Errorf("hidblocker: read hidraw major: %w", err)
	}

	var objs blockerObjects
	err = loadBlockerObjects(&objs, nil)
	if err != nil {
		return nil, fmt.Errorf("hidblocker: load BPF objects: %w", err)
	}

	key := uint32(0)
	err = objs.HidrawConfig.Put(key, major)
	if err != nil {
		_ = objs.Close()
		return nil, fmt.Errorf("hidblocker: set config: %w", err)
	}

	lnk, err := link.AttachLSM(link.LSMOptions{
		Program: objs.HidrawBlock,
	})
	if err != nil {
		_ = objs.Close()
		return nil, fmt.Errorf("hidblocker: attach LSM: %w", err)
	}

	slog.Info("hidblocker: BPF LSM attached", "hidraw_major", major)
	return &Blocker{objs: objs, link: lnk}, nil
}

// Block adds a PID to the blocked set.
func (b *Blocker) Block(pid int) error {
	key := uint32(pid)
	val := uint8(1)
	if err := b.objs.BlockedPids.Put(key, val); err != nil {
		return fmt.Errorf("hidblocker: block PID %d: %w", pid, err)
	}
	slog.Debug("hidblocker: blocked PID", "pid", pid)
	return nil
}

// Unblock removes a PID from the blocked set. Idempotent.
func (b *Blocker) Unblock(pid int) error {
	key := uint32(pid)
	err := b.objs.BlockedPids.Delete(key)
	if err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		return fmt.Errorf("hidblocker: unblock PID %d: %w", pid, err)
	}
	slog.Debug("hidblocker: unblocked PID", "pid", pid)
	return nil
}

// UnblockAll removes all PIDs from the blocked set.
func (b *Blocker) UnblockAll() {
	// Collect all keys first, then delete — safe against concurrent modification.
	var keys []uint32
	var key uint32
	var val uint8
	iter := b.objs.BlockedPids.Iterate()
	for iter.Next(&key, &val) {
		keys = append(keys, key)
	}
	for _, k := range keys {
		_ = b.objs.BlockedPids.Delete(k)
	}
	slog.Info("hidblocker: unblocked all PIDs", "count", len(keys))
}

// Close detaches the BPF program and releases all resources.
func (b *Blocker) Close() {
	if b.link != nil {
		_ = b.link.Close()
	}
	_ = b.objs.Close()
	slog.Info("hidblocker: BPF LSM detached")
}

// lsmEnabled checks if BPF LSM is active in the running kernel.
func lsmEnabled() bool {
	data, err := os.ReadFile("/sys/kernel/security/lsm")
	if err != nil {
		return false
	}
	for _, mod := range strings.Split(strings.TrimSpace(string(data)), ",") {
		if mod == "bpf" {
			return true
		}
	}
	return false
}

// hidrawMajor reads the hidraw character device major number from /proc/devices.
func hidrawMajor() (uint32, error) {
	f, err := os.Open("/proc/devices")
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == "hidraw" {
			n, err := strconv.ParseUint(fields[0], 10, 32)
			if err != nil {
				return 0, fmt.Errorf("parse hidraw major %q: %w", fields[0], err)
			}
			return uint32(n), nil
		}
	}
	return 0, errors.New("hidraw not found in /proc/devices")
}
