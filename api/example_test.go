package api_test

import (
	"encoding/json"
	"fmt"

	"github.com/dahui/z13ctl/api"
)

func ExampleSendApply() {
	// Apply a static red color at full brightness to all devices.
	handled, err := api.SendApply("", "FF0000", "000000", "static", "normal", 3)
	if !handled {
		fmt.Println("daemon not running, falling back to direct HID access")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("applied")
}

func ExampleSendOff() {
	// Turn off lighting on all devices.
	handled, err := api.SendOff("")
	if !handled {
		fmt.Println("daemon not running, falling back to direct HID access")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("lights off")
}

func ExampleSendBrightness() {
	// Set brightness to medium on the keyboard only.
	handled, err := api.SendBrightness("keyboard", 2)
	if !handled {
		fmt.Println("daemon not running, falling back to direct HID access")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("brightness set")
}

func ExampleSendProfileSet() {
	// Switch to the performance power profile.
	handled, err := api.SendProfileSet("performance")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("profile set")
}

func ExampleSendProfileGet() {
	// Read the current performance profile from sysfs via the daemon.
	handled, profile, err := api.SendProfileGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("profile:", profile)
}

func ExampleSendBatteryLimitSet() {
	// Limit battery charge to 80%.
	handled, err := api.SendBatteryLimitSet(80)
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("battery limit set")
}

func ExampleSendBatteryLimitGet() {
	// Read the current battery charge limit.
	handled, limit, err := api.SendBatteryLimitGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("battery limit:", limit)
}

func ExampleSendBootSoundSet() {
	// Disable the POST boot sound.
	handled, err := api.SendBootSoundSet(0)
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("boot sound set")
}

func ExampleSendBootSoundGet() {
	// Read the current boot sound setting.
	handled, value, err := api.SendBootSoundGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("boot sound:", value)
}

func ExampleSendPanelOverdriveSet() {
	// Enable panel refresh overdrive.
	handled, err := api.SendPanelOverdriveSet(1)
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("panel overdrive set")
}

func ExampleSendPanelOverdriveGet() {
	// Read the current panel overdrive setting.
	handled, value, err := api.SendPanelOverdriveGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("panel overdrive:", value)
}

func ExampleSendFanCurveGet() {
	// Read the current fan curve for both fans via the daemon.
	handled, value, err := api.SendFanCurveGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("fan curve:", value)
}

func ExampleSendFanCurveSet() {
	// Set a custom 8-point fan curve (applied to both fans).
	handled, err := api.SendFanCurveSet("48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("fan curve set")
}

func ExampleSendFanCurveReset() {
	// Reset both fans to firmware auto mode.
	handled, err := api.SendFanCurveReset()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("fan curves reset")
}

func ExampleSendTdpGet() {
	// Read current TDP/PPT values via the daemon.
	handled, value, err := api.SendTdpGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("tdp:", value)
}

func ExampleSendTdpSet() {
	// Set TDP to 50W (all PPT values equal).
	handled, err := api.SendTdpSet("50", "", "", "", false)
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("tdp set")
}

func ExampleSendTdpReset() {
	// Reset to balanced profile, restoring its stock PPT and auto fan curves.
	handled, err := api.SendTdpReset()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("tdp reset")
}

func ExampleSendUndervoltGet() {
	// Read current Curve Optimizer offsets from daemon state.
	handled, value, err := api.SendUndervoltGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("undervolt:", value)
}

func ExampleSendUndervoltSet() {
	// Set CPU Curve Optimizer to -20.
	handled, err := api.SendUndervoltSet("-20")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("undervolt set")
}

func ExampleSendUndervoltReset() {
	// Reset Curve Optimizer to stock (0).
	handled, err := api.SendUndervoltReset()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("undervolt reset")
}

func ExampleSendGetState() {
	// Fetch the daemon's full cached state for GUI initialization.
	handled, state, err := api.SendGetState()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("mode:", state.Lighting.Mode)
}

func ExampleSubscribe() {
	// Subscribe to Armoury Crate button press events.
	ch, cancel, err := api.Subscribe([]string{"gui-toggle"})
	if ch == nil {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer cancel()
	for event := range ch {
		fmt.Println("received event:", event)
	}
}

func ExampleSendProfileCreate() {
	// Create an empty custom profile. It is not activated; add settings with
	// the *For variants, then select it with SendProfileSet.
	handled, err := api.SendProfileCreate("battery-uv")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("profile created")
}

func ExampleSendProfileSave() {
	// Copy the profile currently in force under a new name, leaving the
	// original active.
	handled, err := api.SendProfileSave("gaming")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("profile copied")
}

func ExampleSendProfileDelete() {
	// The daemon refuses to delete the active profile or one referenced by
	// autoswitch.
	handled, err := api.SendProfileDelete("gaming")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("profile deleted")
}

func ExampleSendProfileList() {
	handled, value, err := api.SendProfileList()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	var profiles []struct {
		api.CustomProfile
		Active bool `json:"active"`
	}
	if err := json.Unmarshal([]byte(value), &profiles); err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, p := range profiles {
		fmt.Println(p.Name, p.Active)
	}
}

func ExampleSendTdpSetFor() {
	// Store a 35W limit in a profile that is not running. Nothing is applied to
	// hardware, which is what makes it possible to build the profile autoswitch
	// selects on battery while still plugged in.
	handled, err := api.SendTdpSetFor("battery-uv", "35", "", "", "", false)
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("stored in battery-uv")
}

func ExampleSendFanCurveSetFor() {
	// An empty profile name edits the active profile and writes hardware, which
	// is exactly what SendFanCurveSet does.
	handled, err := api.SendFanCurveSetFor("battery-uv", "30:40%,40:45%,50:50%,60:60%,70:70%,80:85%,90:100%,100:100%")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("stored in battery-uv")
}

func ExampleSendFanCurveResetFor() {
	// Clear the fan curve from a stored profile, so it no longer controls fans.
	handled, err := api.SendFanCurveResetFor("battery-uv")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("fan curve cleared")
}

func ExampleSendTdpResetFor() {
	handled, err := api.SendTdpResetFor("battery-uv")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("power limits cleared")
}

func ExampleSendUndervoltSetFor() {
	handled, err := api.SendUndervoltSetFor("battery-uv", "-25")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("stored in battery-uv")
}

func ExampleSendUndervoltResetFor() {
	handled, err := api.SendUndervoltResetFor("battery-uv")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("undervolt cleared")
}

func ExampleSendAutoswitchSet() {
	// Balanced on AC, a custom profile on battery. An empty target leaves that
	// side to the desktop's own power management.
	handled, err := api.SendAutoswitchSet(true, "balanced", "battery-uv")
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("autoswitch configured")
}

func ExampleSendAutoswitchGet() {
	handled, value, err := api.SendAutoswitchGet()
	if !handled {
		fmt.Println("daemon not running")
		return
	}
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	var st struct {
		api.AutoswitchState
		OnAC bool `json:"on_ac"`
	}
	if err := json.Unmarshal([]byte(value), &st); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(st.Enabled, st.AC, st.Battery, st.OnAC)
}
