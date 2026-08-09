// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gamepad

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dahui/z13gui/internal/gui/gamepad/hidblocker"
)

// pidFilePath returns the path to the frozen-PID state file.
// Used for crash recovery: if z13gui is killed, the systemd ExecStopPost
// or signal handler reads this file to thaw the frozen Steam process.
func pidFilePath() string {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(runtime, "z13gui-frozen-pid")
}

// FindSteamPID locates the main Steam process by scanning /proc.
// Returns 0 if not found (graceful degradation — no freeze attempted).
func FindSteamPID() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == "steam" {
			slog.Debug("steam: found process", "pid", pid)
			return pid
		}
	}
	return 0
}

// FreezeProc sends SIGSTOP to suspend a process and writes the PID to a
// state file for crash recovery.
func FreezeProc(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return err
	}
	_ = os.WriteFile(pidFilePath(), []byte(strconv.Itoa(pid)), 0o600)
	return nil
}

// ThawProc sends SIGCONT to resume a process and removes the state file.
func ThawProc(pid int) error {
	_ = os.Remove(pidFilePath())
	return syscall.Kill(pid, syscall.SIGCONT)
}

// ThawFrozen reads the PID state file and sends SIGCONT if a frozen PID is
// recorded. Used by signal handlers and cleanup paths for crash recovery.
func ThawFrozen() {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGCONT)
	_ = os.Remove(pidFilePath())
	slog.Info("steam: thawed frozen process on cleanup", "pid", pid)
}

// SteamInputBlocker suppresses PS/Nintendo controller input from reaching
// Steam while the overlay is visible. Two implementations:
//   - bpfBlocker: BPF LSM blocks hidraw reads (preferred, no side effects)
//   - sigstopBlocker: SIGSTOP freezes Steam (fallback, freezes games too)
type SteamInputBlocker interface {
	BlockSteam() int
	UnblockSteam(pid int)
	Close()
}

// NewSteamInputBlocker creates a SteamInputBlocker. Tries BPF LSM first
// (zero side effects), falls back to SIGSTOP (freezes Steam + games).
func NewSteamInputBlocker() SteamInputBlocker {
	hb, err := hidblocker.New()
	if err != nil {
		slog.Info("steam: BPF LSM unavailable, using SIGSTOP fallback", "err", err)
		return &sigstopBlocker{}
	}
	slog.Info("steam: using BPF LSM hidraw blocker")
	return &bpfBlocker{hb: hb}
}

// --- BPF implementation ---

type bpfBlocker struct {
	hb *hidblocker.Blocker
}

func (b *bpfBlocker) BlockSteam() int {
	pid := FindSteamPID()
	if pid == 0 {
		return 0
	}
	if err := b.hb.Block(pid); err != nil {
		slog.Warn("steam: BPF block failed", "pid", pid, "err", err)
		return 0
	}
	slog.Info("steam: BPF blocked", "pid", pid)
	for _, child := range findChildren(pid) {
		if err := b.hb.Block(child); err != nil {
			slog.Warn("steam: BPF block child failed", "pid", child, "err", err)
		} else {
			slog.Info("steam: BPF blocked child", "pid", child)
		}
	}
	return pid
}

func (b *bpfBlocker) UnblockSteam(pid int) {
	if pid == 0 {
		return
	}
	b.hb.UnblockAll()
	slog.Info("steam: BPF unblocked", "pid", pid)
}

func (b *bpfBlocker) Close() {
	b.hb.Close()
}

// --- SIGSTOP fallback ---

type sigstopBlocker struct{}

func (s *sigstopBlocker) BlockSteam() int {
	pid := FindSteamPID()
	if pid == 0 {
		return 0
	}
	if err := FreezeProc(pid); err != nil {
		slog.Warn("steam: freeze failed", "pid", pid, "err", err)
		return 0
	}
	slog.Info("steam: frozen", "pid", pid)
	return pid
}

func (s *sigstopBlocker) UnblockSteam(pid int) {
	if pid == 0 {
		return
	}
	if err := ThawProc(pid); err != nil {
		slog.Warn("steam: thaw failed", "pid", pid, "err", err)
	} else {
		slog.Info("steam: thawed", "pid", pid)
	}
}

func (s *sigstopBlocker) Close() {}

// findChildren returns PIDs of all direct child processes of ppid.
func findChildren(ppid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	ppidStr := strconv.Itoa(ppid)
	var children []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == ppid {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PPid:\t") {
				if strings.TrimPrefix(line, "PPid:\t") == ppidStr {
					children = append(children, pid)
				}
				break
			}
		}
	}
	return children
}
