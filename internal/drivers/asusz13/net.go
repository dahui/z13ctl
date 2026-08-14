// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package asusz13

// net.go — network byte counters from /proc/net/dev, for the dashboard's
// throughput chart.
//
// What crosses the driver boundary is the cumulative counters, not a rate —
// the energy-counter pattern, for the energy counter's reason: a rate is a
// delta over an interval, which needs the previous reading, and drivers hold
// no state (see driver.Sample). What is here is the read and the decision of
// which interfaces count.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadNetBytes returns cumulative received and transmitted bytes summed over
// the machine's physical network interfaces.
//
// "Physical" is decided by /sys/class/net/<name>/device existing — an
// interface backed by hardware has a device link, while lo, bridges, tun/tap
// and container veth pairs do not. That filter is the point of the function:
// summing every interface double-counts anything routed through a VPN or a
// bridge (the bytes cross the tunnel device and the hardware beneath it), so a
// download over a VPN would chart at twice its size.
//
// An error means no physical interface exists to read (a container, an odd
// VM), never "no traffic": an idle machine's zero is a real reading, so it
// cannot double as absence.
func ReadNetBytes() (rx, tx uint64, err error) {
	data, err := os.ReadFile(procNetDevPath)
	if err != nil {
		return 0, 0, err
	}
	return parseNetDev(string(data), func(name string) bool {
		_, err := os.Lstat(sysNetDir + "/" + name + "/device")
		return err == nil
	})
}

// parseNetDev sums the byte counters of every interface the physical filter
// accepts. Pure, so the table test needs no /proc.
//
// The format is two header lines and then one line per interface:
//
//	wlan0: 123456 789 0 0 0 0 0 0  654321 987 0 0 0 0 0 0
//
// with received bytes first after the colon and transmitted bytes ninth.
func parseNetDev(content string, physical func(string) bool) (rx, tx uint64, err error) {
	found := false
	for _, line := range strings.Split(content, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue // the two header lines
		}
		name = strings.TrimSpace(name)
		if name == "" || !physical(name) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		r, rErr := strconv.ParseUint(fields[0], 10, 64)
		x, xErr := strconv.ParseUint(fields[8], 10, 64)
		if rErr != nil || xErr != nil {
			continue
		}
		rx += r
		tx += x
		found = true
	}
	if !found {
		return 0, 0, fmt.Errorf("no physical network interface to read")
	}
	return rx, tx, nil
}
