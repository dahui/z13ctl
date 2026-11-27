// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package device

import "testing"

func configByID(t *testing.T, id string) Config {
	t.Helper()
	configs, err := Configs()
	if err != nil {
		t.Fatalf("Configs: %v", err)
	}
	for _, c := range configs {
		if c.Device.ID == id {
			return c
		}
	}
	t.Fatalf("no embedded device file %q", id)
	return Config{}
}

// TestYieldToSteamTakesOnlyWhatSteamWrites: on the Xbox Ally X,
// steamos-manager writes the power limits and the charge limit in Gaming
// Mode, so those go and everything else stays.
func TestYieldToSteamTakesOnlyWhatSteamWrites(t *testing.T) {
	c := configByID(t, "asus-rog-xbox-ally-x")
	got, gone := YieldToSteam(c)
	if got.Power != nil {
		t.Error("power survived the yield")
	}
	caps := map[string]bool{}
	for _, u := range gone {
		if u.Reason == "" {
			t.Errorf("%s yielded without a reason", u.Capability)
		}
		caps[u.Capability] = true
	}
	if len(gone) != 2 || !caps["power"] || !caps["charge-limit"] {
		t.Errorf("gone = %+v, want power and charge-limit", gone)
	}
	if got.Fans == nil || got.Profiles == nil || got.Toggles == nil || got.Battery == nil {
		t.Error("the yield took a capability Steam does not write")
	}
	if c.Power == nil {
		t.Error("the yield modified its input")
	}
}

// TestYieldToSteamChargeLimit: the Ally's charge limit is switched off for
// the session, while battery reading (level, health) stays, and the caller's
// config is untouched.
func TestYieldToSteamChargeLimit(t *testing.T) {
	c := configByID(t, "asus-rog-xbox-ally-x")
	if !c.Battery.Caps().ChargeLimit {
		t.Fatal("the Ally's device file no longer offers a charge limit")
	}
	got, _ := YieldToSteam(c)
	if got.Battery == nil || got.Battery.Caps().ChargeLimit {
		t.Error("charge limit survived the yield, or the battery went with it")
	}
	if !got.Battery.Caps().Health {
		t.Error("the yield took battery health, which Steam does not write")
	}
	if !c.Battery.Caps().ChargeLimit {
		t.Error("the yield modified the caller's battery block")
	}
}

// TestYieldToSteamChargeLimitOnly: a device yielding only its battery keeps
// its power control (the two keys are independent).
func TestYieldToSteamChargeLimitOnly(t *testing.T) {
	c := configByID(t, "asus-rog-xbox-ally-x")
	p := *c.Power
	p.YieldInGamingMode = false
	c.Power = &p
	got, gone := YieldToSteam(c)
	if got.Power == nil {
		t.Error("power yielded without the key")
	}
	if len(gone) != 1 || gone[0].Capability != "charge-limit" {
		t.Errorf("gone = %+v, want only charge-limit", gone)
	}
}

// TestDevicesWithoutTheKeyYieldNothing: the Z13 and the OXP declare no yield,
// so a gamescope session changes nothing about them.
func TestDevicesWithoutTheKeyYieldNothing(t *testing.T) {
	for _, id := range []string{"asus-rog-flow-z13-2025", "onexplayer-x2-mini-pro"} {
		c := configByID(t, id)
		got, gone := YieldToSteam(c)
		if len(gone) != 0 || (c.Power != nil) != (got.Power != nil) {
			t.Errorf("%s: yielded %+v", id, gone)
		}
	}
}
