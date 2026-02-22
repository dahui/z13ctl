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
