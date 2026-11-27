package api

// intercept.go — taking the controller from games while an overlay is open.

import (
	"encoding/json"
	"sync"
)

// StreamGamepad is the event name on a gamepad-intercept stream. It is never
// sent to subscribers, so it is not in AllEvents.
const StreamGamepad = "gamepad"

// InterceptGamepad takes the machine's controller from games and returns its
// navigation as a channel of presses and releases, until release is called or
// the daemon goes away (the channel then closes). It is for an overlay like
// voltaire-gui: open it when the overlay appears, release it when it goes.
//
// The daemon does this through InputPlumber's intercept mode, so it works where
// InputPlumber manages the controller, whatever virtual controller it presents
// to games, and nowhere else. The daemon gives the controller back when the
// last client releases or disconnects, crashes included, restoring the mode it
// found. Several clients may hold it at once.
//
// It returns (nil, nil, nil) when the daemon is not running, and an error when
// the daemon refused: no controller managed by InputPlumber, InputPlumber not
// running, or InputPlumber's polkit rule refusing this user (it allows wheel
// and inputplumber members). An older daemon refuses with ErrUnknownCommand.
func InterceptGamepad() (events <-chan GamepadEvent, release func(), err error) {
	conn, scanner, err := openStream(Request{Cmd: CmdGamepadIntercept})
	if conn == nil {
		return nil, nil, err
	}
	ch := make(chan GamepadEvent, 16)
	done := make(chan struct{})
	go func() {
		defer close(ch)
		for scanner.Scan() {
			var ev Response
			if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil || ev.Event != StreamGamepad || ev.Gamepad == nil {
				continue
			}
			select { // see Subscribe: done frees a reader whose caller stopped reading
			case ch <- *ev.Gamepad:
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	release = func() {
		once.Do(func() {
			close(done)
			_ = conn.Close()
		})
	}
	return ch, release, nil
}
