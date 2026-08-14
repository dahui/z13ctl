// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package asusz13

import (
	"os"
	"testing"
)

// netDevSample is a realistic /proc/net/dev: the two header lines, the
// loopback, a physical wlan, a physical usb ethernet, and a wireguard tunnel
// whose traffic also crosses the wlan beneath it.
const netDevSample = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 9000000    100    0    0    0     0          0         0  9000000    100    0    0    0     0       0          0
 wlan0: 1000000    800    0    0    0     0          0         0   200000    400    0    0    0     0       0          0
  usb0:  500000    300    0    0    0     0          0         0   100000    200    0    0    0     0       0          0
   wg0:  400000    250    0    0    0     0          0         0    80000    150    0    0    0     0       0          0
`

// TestParseNetDev pins the physical filter, which is the function's whole
// point: summing every interface counts VPN traffic twice (once on the tunnel
// device, once on the hardware beneath it) and counts loopback chatter as
// network use.
func TestParseNetDev(t *testing.T) {
	t.Parallel()
	physical := func(name string) bool { return name == "wlan0" || name == "usb0" }

	rx, tx, err := parseNetDev(netDevSample, physical)
	if err != nil {
		t.Fatalf("parseNetDev() = %v", err)
	}
	if rx != 1_500_000 || tx != 300_000 {
		t.Errorf("= %d rx, %d tx; want 1500000, 300000 (wlan0+usb0, never lo or wg0)", rx, tx)
	}

	// No physical interface is an error, never a zero: an idle machine's zero
	// is a real reading and cannot double as absence.
	if _, _, noneErr := parseNetDev(netDevSample, func(string) bool { return false }); noneErr == nil {
		t.Error("parseNetDev() = nil error with no physical interface")
	}

	// A malformed line is skipped, not fatal — the others still sum.
	mangled := netDevSample + " eth9: not numbers here\n"
	if rx, _, err = parseNetDev(mangled, physical); err != nil || rx != 1_500_000 {
		t.Errorf("a malformed line broke the sum: rx=%d err=%v", rx, err)
	}
}

// TestReadNetBytes drives the sysfs-backed physical filter: an interface is
// physical iff /sys/class/net/<name>/device exists.
func TestReadNetBytes(t *testing.T) {
	root := t.TempDir()
	swap(t, &procNetDevPath, root+"/netdev")
	swap(t, &sysNetDir, root+"/net")

	if err := os.WriteFile(root+"/netdev", []byte(netDevSample), 0o644); err != nil {
		t.Fatal(err)
	}
	// wlan0 and usb0 are hardware-backed; lo and wg0 have no device link.
	for _, name := range []string{"wlan0/device", "usb0/device", "lo", "wg0"} {
		if err := os.MkdirAll(root+"/net/"+name, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	rx, tx, err := ReadNetBytes()
	if err != nil {
		t.Fatalf("ReadNetBytes() = %v", err)
	}
	if rx != 1_500_000 || tx != 300_000 {
		t.Errorf("= %d rx, %d tx; want 1500000, 300000", rx, tx)
	}
}
