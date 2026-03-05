package api_test

import (
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
	// Reset to balanced profile (firmware manages PPT and fan curves).
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
