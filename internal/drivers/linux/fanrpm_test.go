// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package linux

import (
	"os"
	"testing"
)

func TestReadHwmonFanRPM(t *testing.T) {
	root := t.TempDir()
	old := sysHwmonDir
	sysHwmonDir = root
	t.Cleanup(func() { sysHwmonDir = old })

	mk := func(dir string, files map[string]string) {
		if err := os.MkdirAll(root+"/"+dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, v := range files {
			if err := os.WriteFile(root+"/"+dir+"/"+name, []byte(v+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("hwmon3", map[string]string{"name": "k10temp", "temp1_input": "40000"})
	mk("hwmon7", map[string]string{"name": "oxp_ec", "fan1_input": "2597", "pwm1": "0", "pwm1_enable": "2"})

	got, err := ReadHwmonFanRPM("oxp_ec")
	if err != nil || len(got) != 1 || got[0] != 2597 {
		t.Fatalf("ReadHwmonFanRPM = %v, %v; want [2597]", got, err)
	}
	if _, err := ReadHwmonFanRPM("asus"); err == nil {
		t.Error("a missing hwmon must be an error")
	}
	if _, err := ReadHwmonFanRPM("k10temp"); err == nil {
		t.Error("an hwmon with no fan inputs must be an error, not zero fans")
	}
	mk("hwmon7", map[string]string{"fan10_input": "1200", "fan2_input": "3000"})
	if got, _ := ReadHwmonFanRPM("oxp_ec"); len(got) != 3 || got[0] != 2597 || got[1] != 3000 || got[2] != 1200 {
		t.Errorf("channels out of order: %v", got)
	}
}
