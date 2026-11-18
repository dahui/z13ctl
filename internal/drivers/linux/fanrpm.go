// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package linux

// fanrpm.go — fan speed read from an hwmon device by name, for a device whose
// fans voltaire reads but does not control (telemetry.fans; the OXP's oxp_ec).
// Where voltaire controls the fans, the fan driver reports RPM instead.

import (
	"errors"
	"os"
	"regexp"
	"sort"
	"strconv"

	"github.com/dahui/voltaire/v2/internal/drivers/internal/sysfsio"
)

var fanInputRe = regexp.MustCompile(`^fan(\d+)_input$`)

// findFanHwmon returns the hwmon directory named name, or "".
func findFanHwmon(name string) string { return sysfsio.FindHwmon(sysHwmonDir, name) }

// ReadHwmonFanRPM reads every fanN_input of the hwmon device named name, in
// channel order. Listing the directory reads nothing; each input is one read.
// No fan inputs at all is an error, never an empty reading: a chart of no fans
// and a chart of stopped fans are different claims.
func ReadHwmonFanRPM(name string) ([]int, error) {
	dir := findFanHwmon(name)
	if dir == "" {
		return nil, errors.New("fan hwmon " + name + " not found")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var chans []int
	for _, e := range entries {
		if m := fanInputRe.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.Atoi(m[1])
			chans = append(chans, n)
		}
	}
	if len(chans) == 0 {
		return nil, errors.New("fan hwmon " + name + " has no fan inputs")
	}
	sort.Ints(chans)
	out := make([]int, 0, len(chans))
	for _, n := range chans {
		v, err := sysfsio.ReadInt(dir + "/fan" + strconv.Itoa(n) + "_input")
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
