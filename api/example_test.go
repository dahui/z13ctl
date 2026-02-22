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
